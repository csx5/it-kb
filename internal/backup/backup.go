package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/model"
)

const (
	maxRestoreUncompressedBytes = 2 << 30 // 2 GiB Schutz gegen Zip-Bomben
	sqliteMagicHeader           = "SQLite format 3\x00"
)

var mediaRefPattern = regexp.MustCompile(`/media/([a-f0-9]{16,64})`)

// CreateFullBackup erzeugt ein konsistentes Voll-Backup-ZIP auf Disk (für CLI & Cronjobs).
func CreateFullBackup(ctx context.Context, store *db.Store, mediaDir, outZipPath string) error {
	if err := os.MkdirAll(filepath.Dir(outZipPath), 0o750); err != nil {
		return err
	}
	outFile, err := os.Create(outZipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return WriteFullBackup(ctx, store, mediaDir, outFile)
}

// WriteFullBackup schreibt ein konsistentes Voll-Backup (SQLite-Hot-Snapshot via VACUUM INTO + media/) als ZIP.
func WriteFullBackup(ctx context.Context, store *db.Store, mediaDir string, dst io.Writer) error {
	tmpDir, err := os.MkdirTemp("", "it-kb-backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	snapPath := filepath.Join(tmpDir, "kb.sqlite")
	if err := store.VacuumInto(ctx, snapPath); err != nil {
		return fmt.Errorf("sqlite snapshot: %w", err)
	}

	zw := zip.NewWriter(dst)
	defer zw.Close()

	if err := addFileToZip(zw, snapPath, "kb.sqlite"); err != nil {
		return err
	}

	if _, err := os.Stat(mediaDir); err == nil {
		err = filepath.Walk(mediaDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return walkErr
			}
			rel, err := filepath.Rel(mediaDir, path)
			if err != nil {
				return err
			}
			return addFileToZip(zw, path, filepath.ToSlash(filepath.Join("media", rel)))
		})
		if err != nil {
			return fmt.Errorf("medien archivieren: %w", err)
		}
	}

	return zw.Close()
}

// WritePortableMarkdownExport exportiert alle aktiven Artikel als lesbare Markdown-Dateien
// mit YAML-Frontmatter, relativen Medien-Links sowie categories.json und synonyms.json (kein Vendor-Lock-in).
func WritePortableMarkdownExport(ctx context.Context, store *db.Store, mediaDir string, dst io.Writer) error {
	articles, err := store.ListAllArticles(ctx)
	if err != nil {
		return fmt.Errorf("artikel laden: %w", err)
	}
	categories, err := store.ListCategories(ctx)
	if err != nil {
		return fmt.Errorf("kategorien laden: %w", err)
	}
	synonyms, err := store.ListSynonymGroups(ctx)
	if err != nil {
		return fmt.Errorf("synonyme laden: %w", err)
	}

	zw := zip.NewWriter(dst)
	defer zw.Close()

	// Metadaten exportieren
	if err := writeJSONToZip(zw, "categories.json", categories); err != nil {
		return err
	}
	if err := writeJSONToZip(zw, "synonyms.json", synonyms); err != nil {
		return err
	}

	readme := fmt.Sprintf("IT-Wissensdatenbank – Portabler Markdown-Export\nErstellt am: %s\n\nStruktur:\n- articles/*.md   Alle aktiven Artikel inkl. YAML-Frontmatter\n- media/*         Zugehörige Screenshots, Videos und IT-Anhänge\n- categories.json Kategorien\n- synonyms.json   Team-Synonyme\n",
		time.Now().Format("02.01.2006 15:04:05"))
	if err := writeBytesToZip(zw, "README.txt", []byte(readme)); err != nil {
		return err
	}

	exportedMedia := make(map[string]string) // mediaID -> zip relative filename inside media/

	for _, a := range articles {
		if a.DeletedAt != nil {
			continue
		}

		// Medien-Referenzen auflösen und für Markdown umschreiben
		rewrite := func(src string) string {
			return mediaRefPattern.ReplaceAllStringFunc(src, func(match string) string {
				sub := mediaRefPattern.FindStringSubmatch(match)
				if len(sub) < 2 {
					return match
				}
				mediaID := sub[1]
				if relName, ok := exportedMedia[mediaID]; ok {
					return "../media/" + relName
				}
				mf, err := store.GetMediaFile(ctx, mediaID)
				if err != nil {
					return match
				}
				fullPath := filepath.Join(mediaDir, filepath.FromSlash(mf.StoredPath))
				if _, err := os.Stat(fullPath); err != nil {
					return match
				}
				safeBase := sanitizeExportFileName(mf.OriginalName)
				zipFileName := mediaID[:8] + "-" + safeBase
				if err := addFileToZip(zw, fullPath, "media/"+zipFileName); err != nil {
					return match
				}
				exportedMedia[mediaID] = zipFileName
				return "../media/" + zipFileName
			})
		}

		mdContent := formatArticleMarkdown(a, rewrite(a.Symptoms), rewrite(a.SolutionBody))
		fileName := fmt.Sprintf("articles/%s.md", a.Slug)
		if err := writeBytesToZip(zw, fileName, []byte(mdContent)); err != nil {
			return err
		}
	}

	return zw.Close()
}

// RestoreFullBackup prüft ein Voll-Backup-ZIP (Zip-Slip-Schutz, SQLite-Header, Schema-Prüfung)
// und stellt Datenbank sowie Medienverzeichnis wieder her.
func RestoreFullBackup(ctx context.Context, r io.ReaderAt, size int64, store *db.Store, mediaDir string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("ungültiges zip-archiv: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "it-kb-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	stagedDB := filepath.Join(tmpDir, "kb.sqlite")
	stagedMedia := filepath.Join(tmpDir, "media")
	hasDB := false
	var totalBytes int64

	for _, f := range zr.File {
		name := filepath.ToSlash(strings.TrimPrefix(f.Name, "/"))
		cleaned := filepath.Clean(filepath.FromSlash(name))
		if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
			return errors.New("unsicherer pfad im backup-archiv (zip-slip abgelehnt)")
		}
		if f.FileInfo().IsDir() {
			continue
		}

		var target string
		switch {
		case name == "kb.sqlite":
			target = stagedDB
			hasDB = true
		case strings.HasPrefix(name, "media/"):
			rel := strings.TrimPrefix(name, "media/")
			target = filepath.Join(stagedMedia, filepath.FromSlash(rel))
		default:
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		n, err := writeLimitedFile(target, rc, maxRestoreUncompressedBytes-totalBytes)
		_ = rc.Close()
		if err != nil {
			return err
		}
		totalBytes += n
	}

	if !hasDB {
		return errors.New("backup-archiv enthält keine kb.sqlite")
	}

	// SQLite-Magic-Header und Schema verifizieren, bevor Live-Daten berührt werden
	if err := verifySQLiteSnapshot(ctx, stagedDB); err != nil {
		return err
	}

	// 1. Datenbank ersetzen und neu öffnen
	if err := store.ReplaceFromFile(stagedDB); err != nil {
		return fmt.Errorf("datenbank wiederherstellen: %w", err)
	}

	// 2. Medienverzeichnis ersetzen
	_ = os.RemoveAll(mediaDir)
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		return fmt.Errorf("medienordner neu anlegen: %w", err)
	}
	if _, err := os.Stat(stagedMedia); err == nil {
		if err := copyDir(stagedMedia, mediaDir); err != nil {
			return fmt.Errorf("medien wiederherstellen: %w", err)
		}
	}

	return nil
}

func verifySQLiteSnapshot(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	header := make([]byte, len(sqliteMagicHeader))
	_, readErr := io.ReadFull(f, header)
	_ = f.Close()
	if readErr != nil || !bytes.Equal(header, []byte(sqliteMagicHeader)) {
		return errors.New("kb.sqlite im archiv ist keine gültige sqlite-datenbank")
	}

	testStore, err := db.Open(path)
	if err != nil {
		return fmt.Errorf("snapshot-prüfung fehlgeschlagen: %w", err)
	}
	defer testStore.Close()
	if _, err := testStore.ListCategories(ctx); err != nil {
		return fmt.Errorf("snapshot-schema ungültig: %w", err)
	}
	return nil
}

func formatArticleMarkdown(a model.Article, symptoms, solution string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %d\n", a.ID)
	fmt.Fprintf(&b, "slug: %q\n", a.Slug)
	fmt.Fprintf(&b, "title: %q\n", a.Title)
	fmt.Fprintf(&b, "template: %q\n", a.TemplateType)
	fmt.Fprintf(&b, "category: %q\n", a.CategoryName)
	fmt.Fprintf(&b, "status: %q\n", a.Status)
	if a.ErrorCodes != "" {
		fmt.Fprintf(&b, "error_codes: %q\n", a.ErrorCodes)
	}
	if len(a.Tags) > 0 {
		b.WriteString("tags:\n")
		for _, t := range a.Tags {
			fmt.Fprintf(&b, "  - %q\n", t)
		}
	}
	fmt.Fprintf(&b, "author: %q\n", a.AuthorName)
	fmt.Fprintf(&b, "version: %d\n", a.Version)
	if a.LastVerifiedAt != nil {
		fmt.Fprintf(&b, "last_verified_at: %q\n", a.LastVerifiedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "updated_at: %q\n", a.UpdatedAt.Format(time.RFC3339))
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", a.Title)
	if a.ErrorCodes != "" {
		b.WriteString("## Fehlercode / Fehlermeldung\n\n")
		fmt.Fprintf(&b, "`%s`\n\n", a.ErrorCodes)
	}
	if strings.TrimSpace(symptoms) != "" {
		b.WriteString("## Symptome / Ursache\n\n")
		b.WriteString(strings.TrimSpace(symptoms))
		b.WriteString("\n\n")
	}
	if a.TemplateType == model.TemplateHowTo {
		b.WriteString("## Anleitung\n\n")
	} else {
		b.WriteString("## Lösung\n\n")
	}
	b.WriteString(strings.TrimSpace(solution))
	b.WriteString("\n")
	return b.String()
}

func sanitizeExportFileName(name string) string {
	base := filepath.Base(name)
	r := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", "..", "_")
	out := strings.TrimSpace(r.Replace(base))
	if out == "" {
		return "datei"
	}
	return out
}

func writeLimitedFile(dstPath string, src io.Reader, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("backup-archiv überschreitet die maximal zulässige größe")
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
		return 0, err
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	n, err := io.Copy(out, io.LimitReader(src, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, errors.New("backup-archiv überschreitet die maximal zulässige größe")
	}
	return n, nil
}

func copyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func writeJSONToZip(zw *zip.Writer, name string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writeBytesToZip(zw, name, raw)
}

func writeBytesToZip(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(strings.TrimPrefix(name, "/"))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func addFileToZip(zw *zip.Writer, srcPath, archiveName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := zw.Create(strings.TrimPrefix(archiveName, "/"))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
