package search

import (
	"testing"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

func TestSearchEngine_ErrorCodeSynonymAndFuzzy(t *testing.T) {
	now := time.Now().UTC()
	articles := []model.Article{
		{
			ID:           1,
			Title:        "Outlook: Fehler 0x8004010F beim Senden",
			Status:       model.StatusActive,
			TemplateType: model.TemplateProblemSolution,
			ErrorCodes:   "0x8004010F",
			Symptoms:     "Auf die Outlook-Datendatei kann nicht zugegriffen werden.",
			SolutionBody: "Neues Profil anlegen und OST neu synchronisieren.",
			Tags:         []string{"outlook", "exchange"},
			UpdatedAt:    now,
		},
		{
			ID:           2,
			Title:        "Netzwerkdrucker Warteschlange hängt",
			Status:       model.StatusActive,
			TemplateType: model.TemplateProblemSolution,
			ErrorCodes:   "0x0000011b",
			Symptoms:     "Druckaufträge bleiben hängen.",
			SolutionBody: "Spooler Dienst neu starten.",
			Tags:         []string{"drucker", "spooler"},
			UpdatedAt:    now,
		},
	}

	synonyms := []model.SynonymGroup{
		{ID: 1, Terms: []string{"o365", "outlook", "exchange"}},
		{ID: 2, Terms: []string{"drucker", "printer", "spooler"}},
	}

	eng := NewEngine(180)
	eng.Rebuild(articles, synonyms)

	// 1. Exakter Fehlercode
	hits := eng.Search(model.SearchQuery{Text: "0x8004010F"})
	if len(hits) != 1 || hits[0].Article.ID != 1 {
		t.Fatalf("erwartete Artikel 1 für 0x8004010F, erhalten: %+v", hits)
	}

	// 2. Synonym-Suche (o365 -> outlook)
	hits = eng.Search(model.SearchQuery{Text: "o365"})
	if len(hits) == 0 || hits[0].Article.ID != 1 {
		t.Fatalf("erwartete Artikel 1 für Synonym o365, erhalten: %+v", hits)
	}

	// 3. Tippfehler-Toleranz (druker -> drucker)
	hits = eng.Search(model.SearchQuery{Text: "druker"})
	if len(hits) == 0 || hits[0].Article.ID != 2 {
		t.Fatalf("erwartete Artikel 2 für Tippfehler druker, erhalten: %+v", hits)
	}

	// 4. Umlaut-Normalisierung (haengt -> hängt)
	hits = eng.Search(model.SearchQuery{Text: "haengt"})
	if len(hits) == 0 || hits[0].Article.ID != 2 {
		t.Fatalf("erwartete Artikel 2 für Umlaut-Suche haengt, erhalten: %+v", hits)
	}
}
