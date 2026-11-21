package search

import (
	"html"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

const (
	weightErrorCodes = 5.0
	weightTitle      = 3.5
	weightTags       = 2.5
	weightSymptoms   = 1.5
	weightSolution   = 1.0

	bm25K1 = 1.5
	bm25B  = 0.75
)

type fieldIndex struct {
	tf  map[string]int
	len int
}

type indexedDoc struct {
	article      model.Article
	fields       [5]fieldIndex // 0: error_codes, 1: title, 2: tags, 3: symptoms, 4: solution
	rawErrorLow  string
	rawTitleLow  string
	rawBodyLow   string
	allTerms     map[string]struct{}
}

// Engine ist eine rein algorithmische, deterministische In-Memory-Such-Engine.
type Engine struct {
	mu                 sync.RWMutex
	docs               map[int64]*indexedDoc
	activeDocs         int
	df                 map[string]int
	trigramIndex       map[string]map[string]struct{} // trigram -> set of vocabulary terms
	synonyms           map[string][]string            // normalized term -> expanded terms
	multiWordSynonyms  [][]string
	avgFieldLens       [5]float64
	reviewReminderDays int
}

func NewEngine(reviewReminderDays int) *Engine {
	return &Engine{
		docs:               make(map[int64]*indexedDoc),
		df:                 make(map[string]int),
		trigramIndex:       make(map[string]map[string]struct{}),
		synonyms:           make(map[string][]string),
		reviewReminderDays: reviewReminderDays,
	}
}

func (e *Engine) Rebuild(articles []model.Article, groups []model.SynonymGroup) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.docs = make(map[int64]*indexedDoc, len(articles))
	for _, a := range articles {
		e.docs[a.ID] = buildIndexedDoc(a)
	}
	e.setSynonymsLocked(groups)
	e.recomputeStatsLocked()
}

func (e *Engine) UpsertArticle(a model.Article) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.docs[a.ID] = buildIndexedDoc(a)
	e.recomputeStatsLocked()
}

func (e *Engine) RemoveArticle(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.docs, id)
	e.recomputeStatsLocked()
}

func (e *Engine) SetSynonyms(groups []model.SynonymGroup) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.setSynonymsLocked(groups)
}

func (e *Engine) SetReviewReminderDays(days int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reviewReminderDays = days
}

func (e *Engine) setSynonymsLocked(groups []model.SynonymGroup) {
	e.synonyms = make(map[string][]string)
	e.multiWordSynonyms = nil

	for _, g := range groups {
		cleaned := make([]string, 0, len(g.Terms))
		for _, t := range g.Terms {
			t = strings.TrimSpace(strings.ToLower(t))
			if t != "" {
				cleaned = append(cleaned, t)
			}
		}
		if len(cleaned) < 2 {
			continue
		}
		e.multiWordSynonyms = append(e.multiWordSynonyms, cleaned)
		for _, t := range cleaned {
			norm := Normalize(t)
			for _, other := range cleaned {
				if other == t {
					continue
				}
				e.synonyms[t] = appendUnique(e.synonyms[t], other)
				if norm != t {
					e.synonyms[norm] = appendUnique(e.synonyms[norm], other)
				}
			}
		}
	}
}

func (e *Engine) recomputeStatsLocked() {
	e.df = make(map[string]int)
	e.trigramIndex = make(map[string]map[string]struct{})
	var sums [5]int
	activeDocs := 0

	for _, doc := range e.docs {
		if doc.article.DeletedAt != nil {
			continue
		}
		activeDocs++
		for i := 0; i < 5; i++ {
			sums[i] += doc.fields[i].len
		}
		for term := range doc.allTerms {
			e.df[term]++
			if len([]rune(term)) >= 4 {
				for _, tri := range Trigrams(term) {
					if e.trigramIndex[tri] == nil {
						e.trigramIndex[tri] = make(map[string]struct{})
					}
					e.trigramIndex[tri][term] = struct{}{}
				}
			}
		}
	}

	e.activeDocs = activeDocs
	if activeDocs == 0 {
		e.avgFieldLens = [5]float64{1, 1, 1, 1, 1}
		return
	}
	for i := 0; i < 5; i++ {
		avg := float64(sums[i]) / float64(activeDocs)
		if avg < 1 {
			avg = 1
		}
		e.avgFieldLens[i] = avg
	}
}

func (e *Engine) Search(q model.SearchQuery) []model.SearchHit {
	e.mu.RLock()
	defer e.mu.RUnlock()

	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	queryText := strings.TrimSpace(q.Text)
	queryLow := strings.ToLower(queryText)
	baseTerms := uniqueStrings(Tokenize(queryText))

	// Synonyme und Fuzzy-Terme mit Gewichtungen sammeln
	termWeights := make(map[string]float64, len(baseTerms)*3)
	for _, t := range baseTerms {
		termWeights[t] = 1.0
	}
	// Mehrwort-Synonyme nur an echten Wort-/Token-Grenzen prüfen (z. B. "ad" matcht nicht "admin")
	for _, group := range e.multiWordSynonyms {
		matched := false
		for _, phrase := range group {
			if !strings.Contains(phrase, " ") {
				continue
			}
			if containsWholePhrase(queryLow, phrase) || containsWholePhrase(Normalize(queryLow), Normalize(phrase)) {
				matched = true
				break
			}
		}
		if matched {
			for _, phrase := range group {
				for _, tok := range Tokenize(phrase) {
					if _, exists := termWeights[tok]; !exists {
						termWeights[tok] = 0.65
					}
				}
			}
		}
	}
	// Einzelwort-Synonyme
	for _, t := range baseTerms {
		for _, syn := range e.synonyms[t] {
			for _, tok := range Tokenize(syn) {
				if _, exists := termWeights[tok]; !exists {
					termWeights[tok] = 0.65
				}
			}
		}
	}
	// Trigramm / Damerau-Levenshtein Tippfehler-Toleranz
	for _, t := range baseTerms {
		if len([]rune(t)) < 4 {
			continue
		}
		if _, exactVocab := e.df[t]; exactVocab {
			continue
		}
		for _, candidate := range e.fuzzyCandidatesLocked(t) {
			if _, exists := termWeights[candidate]; !exists {
				termWeights[candidate] = 0.55
			}
		}
	}

	totalDocs := float64(maxInt(e.activeDocs, 1))
	fieldWeights := [5]float64{weightErrorCodes, weightTitle, weightTags, weightSymptoms, weightSolution}
	fieldNames := [5]string{"error_codes", "title", "tags", "symptoms", "solution_body"}

	hits := make([]model.SearchHit, 0, 32)
	for _, doc := range e.docs {
		if !e.matchesFilters(doc.article, q) {
			continue
		}
		needsReview := e.isReviewDue(doc.article)

		if queryText == "" {
			baseScore := float64(doc.article.UpdatedAt.Unix())
			if doc.article.IsPinned {
				// Angepinnte Dauerbrenner stehen auf der Startseite immer ganz oben
				baseScore += 1e12
			}
			hits = append(hits, model.SearchHit{
				Article:      doc.article,
				Score:        baseScore,
				SnippetHTML:  buildDefaultSnippet(doc.article),
				MatchedField: "title",
				NeedsReview:  needsReview,
			})
			continue
		}

		var totalScore float64
		bestFieldIdx := 4
		bestFieldScore := -1.0

		for term, tw := range termWeights {
			df := float64(e.df[term])
			if df == 0 {
				continue
			}
			idf := math.Log(1.0 + (totalDocs-df+0.5)/(df+0.5))

			for fIdx := 0; fIdx < 5; fIdx++ {
				tf := float64(doc.fields[fIdx].tf[term])
				if tf == 0 {
					continue
				}
				dl := float64(doc.fields[fIdx].len)
				avgdl := e.avgFieldLens[fIdx]
				bm25 := idf * ((tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*(dl/avgdl))))
				s := bm25 * fieldWeights[fIdx] * tw
				totalScore += s
				if s > bestFieldScore {
					bestFieldScore = s
					bestFieldIdx = fIdx
				}
			}
		}

		// Exakt-Boost für IT-Fehlercodes, Pfade und exakte Phrasen
		if strings.Contains(doc.rawErrorLow, queryLow) {
			totalScore += 18.0
			bestFieldIdx = 0
		} else if strings.Contains(doc.rawTitleLow, queryLow) {
			totalScore += 9.0
			if bestFieldIdx != 0 {
				bestFieldIdx = 1
			}
		} else if len(queryLow) >= 4 && strings.Contains(doc.rawBodyLow, queryLow) {
			totalScore += 3.0
		}

		if totalScore <= 0 {
			continue
		}

		// Sanfter Popularitäts-Boost für nachweislich hilfreiche Lösungen
		if doc.article.HelpfulCount > 0 {
			totalScore += math.Log1p(float64(doc.article.HelpfulCount)) * 0.6
		}

		hits = append(hits, model.SearchHit{
			Article:      doc.article,
			Score:        math.Round(totalScore*100) / 100,
			SnippetHTML:  buildHighlightedSnippet(doc.article, bestFieldIdx, termWeights),
			MatchedField: fieldNames[bestFieldIdx],
			NeedsReview:  needsReview,
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Article.UpdatedAt.After(hits[j].Article.UpdatedAt)
		}
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// RelatedArticles liefert algorithmisch verwandte Artikel über gemeinsame Tags,
// Kategorie und Token-Überlappung (komplett lokal, ohne KI).
func (e *Engine) RelatedArticles(articleID int64, limit int) []model.Article {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 {
		limit = 4
	}
	src, ok := e.docs[articleID]
	if !ok {
		return nil
	}

	srcTags := make(map[string]struct{}, len(src.article.Tags))
	for _, t := range src.article.Tags {
		srcTags[strings.ToLower(t)] = struct{}{}
	}
	srcKeyTerms := make(map[string]struct{})
	for term := range src.fields[0].tf {
		srcKeyTerms[term] = struct{}{}
	}
	for term := range src.fields[1].tf {
		if len([]rune(term)) >= 4 {
			srcKeyTerms[term] = struct{}{}
		}
	}

	type scored struct {
		article model.Article
		score   float64
	}
	var cands []scored
	for id, doc := range e.docs {
		if id == articleID || doc.article.DeletedAt != nil || doc.article.Status == model.StatusDraft {
			continue
		}
		var s float64
		if src.article.CategoryID != nil && doc.article.CategoryID != nil && *src.article.CategoryID == *doc.article.CategoryID {
			s += 2.5
		}
		for _, t := range doc.article.Tags {
			if _, ok := srcTags[strings.ToLower(t)]; ok {
				s += 3.0
			}
		}
		for term := range srcKeyTerms {
			if _, ok := doc.allTerms[term]; ok {
				s += 1.2
			}
		}
		if s >= 2.0 {
			cands = append(cands, scored{article: doc.article, score: s})
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score == cands[j].score {
			return cands[i].article.HelpfulCount > cands[j].article.HelpfulCount
		}
		return cands[i].score > cands[j].score
	})

	if len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]model.Article, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.article)
	}
	return out
}

func (e *Engine) fuzzyCandidatesLocked(term string) []string {
	tris := Trigrams(term)
	if len(tris) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, tri := range tris {
		for vocabTerm := range e.trigramIndex[tri] {
			counts[vocabTerm]++
		}
	}

	maxDist := 1
	if len([]rune(term)) >= 7 {
		maxDist = 2
	}

	var out []string
	for cand, overlap := range counts {
		if overlap < 2 {
			continue
		}
		if EditDistance(term, cand) <= maxDist {
			out = append(out, cand)
		}
	}
	return out
}

func (e *Engine) matchesFilters(a model.Article, q model.SearchQuery) bool {
	if q.IncludeDeleted {
		if a.DeletedAt == nil {
			return false
		}
	} else {
		if a.DeletedAt != nil {
			return false
		}
	}
	if q.CategoryID != nil {
		if a.CategoryID == nil || *a.CategoryID != *q.CategoryID {
			return false
		}
	}
	if q.Status != "" && a.Status != q.Status {
		return false
	}
	if q.TemplateType != "" && a.TemplateType != q.TemplateType {
		return false
	}
	if q.Author != "" && !strings.EqualFold(a.AuthorName, q.Author) {
		return false
	}
	if q.Tag != "" {
		found := false
		for _, t := range a.Tags {
			if strings.EqualFold(t, q.Tag) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q.NeedsReview && !e.isReviewDue(a) {
		return false
	}
	return true
}

func (e *Engine) isReviewDue(a model.Article) bool {
	if e.reviewReminderDays <= 0 || a.Status != model.StatusActive {
		return false
	}
	ref := a.UpdatedAt
	if a.LastVerifiedAt != nil {
		ref = *a.LastVerifiedAt
	}
	return time.Since(ref) > time.Duration(e.reviewReminderDays)*24*time.Hour
}

func buildIndexedDoc(a model.Article) *indexedDoc {
	doc := &indexedDoc{
		article:     a,
		rawErrorLow: strings.ToLower(a.ErrorCodes),
		rawTitleLow: strings.ToLower(a.Title),
		rawBodyLow:  strings.ToLower(a.Symptoms + "\n" + a.SolutionBody),
		allTerms:    make(map[string]struct{}),
	}

	texts := [5]string{
		a.ErrorCodes,
		a.Title,
		strings.Join(a.Tags, " "),
		a.Symptoms,
		a.SolutionBody,
	}

	for i, txt := range texts {
		toks := Tokenize(txt)
		tf := make(map[string]int, len(toks))
		for _, tok := range toks {
			tf[tok]++
			doc.allTerms[tok] = struct{}{}
		}
		doc.fields[i] = fieldIndex{tf: tf, len: len(toks)}
	}
	return doc
}

func buildDefaultSnippet(a model.Article) string {
	src := strings.TrimSpace(a.Symptoms)
	if src == "" {
		src = strings.TrimSpace(a.SolutionBody)
	}
	return html.EscapeString(truncateRunes(cleanMarkdownPreview(src), 190))
}

func buildHighlightedSnippet(a model.Article, fieldIdx int, terms map[string]float64) string {
	var src string
	switch fieldIdx {
	case 0:
		src = a.ErrorCodes
	case 3:
		src = a.Symptoms
	case 4:
		src = a.SolutionBody
	default:
		if strings.TrimSpace(a.Symptoms) != "" {
			src = a.Symptoms
		} else {
			src = a.SolutionBody
		}
	}
	src = cleanMarkdownPreview(src)
	if src == "" {
		src = a.Title
	}
	clipped := truncateRunes(src, 210)
	escaped := html.EscapeString(clipped)

	// Markierung für die wichtigsten Treffer-Wörter
	words := strings.Fields(escaped)
	for i, w := range words {
		low := strings.Trim(strings.ToLower(w), `.,;:!?()[]"'`)
		if low == "" {
			continue
		}
		if _, ok := terms[low]; ok {
			words[i] = "<mark>" + w + "</mark>"
			continue
		}
		if _, ok := terms[Normalize(low)]; ok {
			words[i] = "<mark>" + w + "</mark>"
		}
	}
	return strings.Join(words, " ")
}

func cleanMarkdownPreview(s string) string {
	r := strings.NewReplacer("\r\n", " ", "\n", " ", "```", " ", "**", "", "##", "", "###", "")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + " …"
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsWholePhrase(haystack, phrase string) bool {
	if phrase == "" || haystack == "" {
		return false
	}
	hWords := strings.Fields(haystack)
	pWords := strings.Fields(phrase)
	if len(pWords) == 0 || len(hWords) < len(pWords) {
		return false
	}
	for i := 0; i <= len(hWords)-len(pWords); i++ {
		ok := true
		for j := range pWords {
			if hWords[i+j] != pWords[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
