package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/media"
	"github.com/csx5/it-kb/internal/model"
)

const maxImportZipBytes = 512 << 20 // 512 MiB Entpack-Limit für Markdown-Import-ZIPs

var (
	importMediaRelPattern = regexp.MustCompile(`(\.\./)?media/([^)\s"'>]+)`)
	h1Pattern             = regexp.MustCompile(`^#\s+(.+)$`)
)

// ImportResult fasst das Ergebnis eines Markdown- oder ZIP-Imports zusammen.
type ImportResult struct {
	ArticlesImported  int      `json:"articles_imported"`
	MediaImported     int      `json:"media_imported"`
	CategoriesCreated int      `json:"categories_created"`
	SynonymsCreated   int      `json:"synonyms_created"`
	Warnings          []string `json:"warnings,omitempty"`
}

// ImportMarkdownFile importiert eine einzelne Markdown-Datei (mit optionalem YAML-Frontmatter) als neuen Artikel.
func ImportMarkdownFile(
	ctx context.Context,
	store *db.Store,
	filename string,
	raw []byte,
	defaultAuthor string,
	defaultUserID *int64,
) (model.Article, error) {
	cats, err := store.ListCategories(ctx)
	if err != nil {
		return model.Article{}, err
	}
	catByName := make(map[string]int64, len(cats))
	for _, c := range cats {
		catByName[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
	}

	parsed := parseImportedMarkdown(filename, string(raw))
	if parsed.Title == "" {
		return model.Article{}, errors.New("artikel-titel konnte aus markdown nicht ermittelt werden")
	}

	var catID *int64
	if parsed.Category != "" {
		key := strings.ToLower(parsed.Category)
		if id, ok := catByName[key]; ok {
			catID = &id
		} else {
			created, cErr := store.CreateCategory(ctx, parsed.Category, "", 50)
			if cErr == nil {
				catID = &created.ID
			}
		}
	}

	author := strings.TrimSpace(defaultAuthor)
	if author == "" {
		author = strings.TrimSpace(parsed.Author)
	}
	if author == "" {
		author = "Import"
	}

	return store.CreateArticle(ctx, db.CreateArticleInput{
		TemplateType: parsed.TemplateType,
		Title:        parsed.Title,
		CategoryID:   catID,
		Status:       parsed.Status,
		ErrorCodes:   parsed.ErrorCodes,
		Symptoms:     parsed.Symptoms,
		SolutionBody: parsed.SolutionBody,
		Tags:         parsed.Tags,
		AuthorName:   author,
		AuthorUserID: defaultUserID,
	})
}

// ImportMarkdownZIP importiert ein ZIP aus WritePortableMarkdownExport (oder ein beliebiges ZIP mit .md-Dateien und media/*).
func ImportMarkdownZIP(
	ctx context.Context,
	r io.ReaderAt,
	size int64,
	store *db.Store,
	mediaStore *media.Storage,
	defaultAuthor string,
	defaultUserID *int64,
) (ImportResult, error) {
	var res ImportResult

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return res, fmt.Errorf("ungültiges zip-archiv: %w", err)
	}

	// Pfadprüfung & Aufteilung
	var mdEntries []*zip.File
	mediaEntries := make(map[string]*zip.File)
	var catsFile, synsFile *zip.File

	var totalDeclared uint64
	for _, f := range zr.File {
		if strings.Contains(f.Name, `\`) {
			return res, errors.New("unsicherer pfad im import-archiv (zip-slip abgelehnt)")
		}
		name := filepath.ToSlash(strings.TrimPrefix(f.Name, "/"))
		cleaned := filepath.Clean(filepath.FromSlash(name))
		if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" || strings.Contains(name, ":") {
			return res, errors.New("unsicherer pfad im import-archiv (zip-slip abgelehnt)")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		totalDeclared += f.UncompressedSize64
		if totalDeclared > maxImportZipBytes {
			return res, errors.New("import-archiv überschreitet maximale entpack-größe")
		}

		lower := strings.ToLower(name)
		switch {
		case lower == "categories.json":
			catsFile = f
		case lower == "synonyms.json":
			synsFile = f
		case strings.HasPrefix(name, "media/"):
			rel := strings.TrimPrefix(name, "media/")
			if rel != "" {
				mediaEntries[rel] = f
			}
		case strings.HasSuffix(lower, ".md") && !strings.EqualFold(filepath.Base(name), "readme.md"):
			mdEntries = append(mdEntries, f)
		}
	}

	// 1. Kategorien vorab anlegen
	cats, err := store.ListCategories(ctx)
	if err != nil {
		return res, err
	}
	catByName := make(map[string]int64, len(cats))
	for _, c := range cats {
		catByName[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
	}
	if catsFile != nil {
		if raw, rErr := readZipEntryLimited(catsFile, 4<<20); rErr == nil {
			var importedCats []model.Category
			if json.Unmarshal(raw, &importedCats) == nil {
				for _, c := range importedCats {
					cName := strings.TrimSpace(c.Name)
					if cName == "" {
						continue
					}
					key := strings.ToLower(cName)
					if _, exists := catByName[key]; exists {
						continue
					}
					created, cErr := store.CreateCategory(ctx, cName, c.Description, c.SortOrder)
					if cErr == nil {
						catByName[key] = created.ID
						res.CategoriesCreated++
					}
				}
			}
		}
	}

	// 2. Synonyme vorab anlegen (ohne Duplikate)
	if synsFile != nil {
		if raw, rErr := readZipEntryLimited(synsFile, 4<<20); rErr == nil {
			var importedSyns []model.SynonymGroup
			if json.Unmarshal(raw, &importedSyns) == nil {
				existingSyns, _ := store.ListSynonymGroups(ctx)
				existingKeys := make(map[string]struct{}, len(existingSyns))
				for _, g := range existingSyns {
					existingKeys[canonicalTermsKey(g.Terms)] = struct{}{}
				}
				for _, g := range importedSyns {
					if len(g.Terms) < 2 {
						continue
					}
					k := canonicalTermsKey(g.Terms)
					if _, exists := existingKeys[k]; exists {
						continue
					}
					if _, sErr := store.CreateSynonymGroup(ctx, g.Terms); sErr == nil {
						existingKeys[k] = struct{}{}
						res.SynonymsCreated++
					}
				}
			}
		}
	}

	if len(mdEntries) == 0 {
		return res, errors.New("keine markdown-artikel (.md) im archiv gefunden")
	}

	sort.Slice(mdEntries, func(i, j int) bool {
		return mdEntries[i].Name < mdEntries[j].Name
	})

	importedMedia := make(map[string]string) // relName -> /media/<id>
	author := strings.TrimSpace(defaultAuthor)
	if author == "" {
		author = "Import"
	}

	resolveMedia := func(text string) string {
		if mediaStore == nil || text == "" {
			return text
		}
		return importMediaRelPattern.ReplaceAllStringFunc(text, func(match string) string {
			sub := importMediaRelPattern.FindStringSubmatch(match)
			if len(sub) < 3 {
				return match
			}
			rel := strings.TrimSpace(sub[2])
			if newURL, ok := importedMedia[rel]; ok {
				return newURL
			}
			zf, exists := mediaEntries[rel]
			if !exists {
				// Auch auf Dateinamen ohne Unterordner prüfen
				zf = mediaEntries[filepath.Base(rel)]
			}
			if zf == nil {
				return match
			}
			rc, oErr := zf.Open()
			if oErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("medium %s nicht lesbar: %v", rel, oErr))
				return match
			}
			mf, sErr := mediaStore.Save(io.LimitReader(rc, 256<<20), filepath.Base(rel), author)
			_ = rc.Close()
			if sErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("medium %s übersprungen: %v", rel, sErr))
				return match
			}
			if iErr := store.InsertMediaFile(ctx, mf); iErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("medium %s datenbankfehler: %v", rel, iErr))
				return match
			}
			newURL := "/media/" + mf.ID
			importedMedia[rel] = newURL
			res.MediaImported++
			return newURL
		})
	}

	for _, f := range mdEntries {
		raw, rErr := readZipEntryLimited(f, 16<<20)
		if rErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s übersprungen: %v", f.Name, rErr))
			continue
		}
		parsed := parseImportedMarkdown(f.Name, string(raw))
		if parsed.Title == "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s übersprungen: kein titel", f.Name))
			continue
		}

		var catID *int64
		if parsed.Category != "" {
			key := strings.ToLower(parsed.Category)
			if id, ok := catByName[key]; ok {
				catID = &id
			} else {
				created, cErr := store.CreateCategory(ctx, parsed.Category, "", 50)
				if cErr == nil {
					catByName[key] = created.ID
					catID = &created.ID
					res.CategoriesCreated++
				}
			}
		}

		artAuthor := author
		if defaultAuthor == "" && strings.TrimSpace(parsed.Author) != "" {
			artAuthor = strings.TrimSpace(parsed.Author)
		}

		_, cErr := store.CreateArticle(ctx, db.CreateArticleInput{
			TemplateType: parsed.TemplateType,
			Title:        parsed.Title,
			CategoryID:   catID,
			Status:       parsed.Status,
			ErrorCodes:   parsed.ErrorCodes,
			Symptoms:     resolveMedia(parsed.Symptoms),
			SolutionBody: resolveMedia(parsed.SolutionBody),
			Tags:         parsed.Tags,
			AuthorName:   artAuthor,
			AuthorUserID: defaultUserID,
		})
		if cErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s nicht importiert: %v", f.Name, cErr))
			continue
		}
		res.ArticlesImported++
	}

	if res.ArticlesImported == 0 {
		return res, errors.New("kein artikel aus dem archiv konnte importiert werden")
	}
	return res, nil
}

func readZipEntryLimited(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("datei im archiv zu groß")
	}
	return data, nil
}

func canonicalTermsKey(terms []string) string {
	norm := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			norm = append(norm, t)
		}
	}
	sort.Strings(norm)
	return strings.Join(norm, "|")
}

type parsedMarkdownDoc struct {
	Title        string
	TemplateType string
	Category     string
	Status       string
	ErrorCodes   string
	Tags         []string
	Author       string
	Symptoms     string
	SolutionBody string
}

func parseImportedMarkdown(filename, raw string) parsedMarkdownDoc {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	doc := parsedMarkdownDoc{
		TemplateType: model.TemplateProblemSolution,
		Status:       model.StatusActive,
	}

	body := text
	if strings.HasPrefix(text, "---\n") {
		if endIdx := strings.Index(text[4:], "\n---\n"); endIdx >= 0 {
			fm := text[4 : 4+endIdx]
			body = text[4+endIdx+5:]
			parseFrontmatter(fm, &doc)
		}
	}

	lines := strings.Split(strings.TrimSpace(body), "\n")
	// Führende H1-Überschrift entfernen bzw. als Titel übernehmen
	if len(lines) > 0 {
		if m := h1Pattern.FindStringSubmatch(strings.TrimSpace(lines[0])); len(m) == 2 {
			if doc.Title == "" {
				doc.Title = strings.TrimSpace(m[1])
			}
			lines = lines[1:]
		}
	}

	// Bekannte Export-Abschnitte (## Fehlercode / Fehlermeldung, ## Symptome / Ursache, ## Lösung, ## Anleitung) aufteilen
	var currentSection string
	var unsectioned, errLines, sympLines, solLines []string
	foundStructured := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.EqualFold(trimmed, "## Fehlercode / Fehlermeldung") || strings.EqualFold(trimmed, "## Fehlercode"):
			currentSection = "error"
			foundStructured = true
			continue
		case strings.EqualFold(trimmed, "## Symptome / Ursache") || strings.EqualFold(trimmed, "## Symptome"):
			currentSection = "symptoms"
			foundStructured = true
			continue
		case strings.EqualFold(trimmed, "## Anleitung"):
			currentSection = "howto"
			foundStructured = true
			doc.TemplateType = model.TemplateHowTo
			continue
		case strings.EqualFold(trimmed, "## Lösung"):
			currentSection = "solution"
			foundStructured = true
			continue
		}

		switch currentSection {
		case "error":
			errLines = append(errLines, line)
		case "symptoms":
			sympLines = append(sympLines, line)
		case "howto", "solution":
			solLines = append(solLines, line)
		default:
			unsectioned = append(unsectioned, line)
		}
	}

	if foundStructured {
		if doc.ErrorCodes == "" {
			errText := strings.TrimSpace(strings.Join(errLines, "\n"))
			errText = strings.Trim(errText, "`")
			doc.ErrorCodes = strings.TrimSpace(errText)
		}
		doc.Symptoms = strings.TrimSpace(strings.Join(sympLines, "\n"))
		doc.SolutionBody = strings.TrimSpace(strings.Join(solLines, "\n"))
		if doc.SolutionBody == "" && doc.Symptoms != "" && doc.TemplateType == model.TemplateHowTo {
			doc.SolutionBody = doc.Symptoms
			doc.Symptoms = ""
		}
		if doc.SolutionBody == "" && len(unsectioned) > 0 {
			doc.SolutionBody = strings.TrimSpace(strings.Join(unsectioned, "\n"))
		}
	} else {
		doc.SolutionBody = strings.TrimSpace(strings.Join(unsectioned, "\n"))
	}

	if doc.Title == "" {
		base := filepath.Base(filename)
		doc.Title = strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	if doc.SolutionBody == "" {
		doc.SolutionBody = doc.Title
	}
	return doc
}

func parseFrontmatter(fm string, doc *parsedMarkdownDoc) {
	inTagsList := false
	for _, rawLine := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if inTagsList && strings.HasPrefix(trimmed, "- ") {
			val := unquoteYAML(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			if val != "" {
				doc.Tags = append(doc.Tags, val)
			}
			continue
		}
		inTagsList = false

		colon := strings.IndexByte(trimmed, ':')
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:colon]))
		val := strings.TrimSpace(trimmed[colon+1:])

		switch key {
		case "title":
			doc.Title = unquoteYAML(val)
		case "template", "template_type":
			t := unquoteYAML(val)
			if t == model.TemplateHowTo {
				doc.TemplateType = model.TemplateHowTo
			} else {
				doc.TemplateType = model.TemplateProblemSolution
			}
		case "category":
			doc.Category = unquoteYAML(val)
		case "status":
			s := unquoteYAML(val)
			if s == model.StatusActive || s == model.StatusDraft || s == model.StatusOutdated {
				doc.Status = s
			}
		case "error_codes":
			doc.ErrorCodes = unquoteYAML(val)
		case "author":
			doc.Author = unquoteYAML(val)
		case "tags":
			if val == "" {
				inTagsList = true
			} else {
				val = strings.Trim(val, "[]")
				for _, part := range strings.Split(val, ",") {
					t := unquoteYAML(strings.TrimSpace(part))
					if t != "" {
						doc.Tags = append(doc.Tags, t)
					}
				}
			}
		}
	}
}

func unquoteYAML(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}
