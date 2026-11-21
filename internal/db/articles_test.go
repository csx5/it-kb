package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/csx5/it-kb/internal/model"
)

func TestArticleLifecycleAndOptimisticLocking(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cats, err := store.ListCategories(ctx)
	if err != nil || len(cats) == 0 {
		t.Fatalf("kategorien laden: %v", err)
	}
	catID := cats[0].ID

	// 1. Erstellen
	created, err := store.CreateArticle(ctx, CreateArticleInput{
		TemplateType: model.TemplateProblemSolution,
		Title:        "VPN bricht ab bei Standortwechsel",
		CategoryID:   &catID,
		Status:       model.StatusActive,
		ErrorCodes:   "ERR_VPN_809",
		Symptoms:     "Tunnel trennt nach 30 Sekunden.",
		SolutionBody: "MTU auf 1400 senken.",
		Tags:         []string{"VPN", "#netzwerk", "vpn"},
		AuthorName:   "MK",
	})
	if err != nil {
		t.Fatalf("artikel erstellen: %v", err)
	}
	if created.Version != 1 || len(created.Tags) != 2 {
		t.Fatalf("unerwarteter artikelzustand: %+v", created)
	}

	// 2. Erfolgreiches Update -> Version 2
	updated, err := store.UpdateArticle(ctx, created.ID, UpdateArticleInput{
		ExpectedVersion: 1,
		TemplateType:    model.TemplateProblemSolution,
		Title:           created.Title,
		CategoryID:      &catID,
		Status:          model.StatusActive,
		ErrorCodes:      "ERR_VPN_809, Event 20227",
		Symptoms:        created.Symptoms,
		SolutionBody:    "MTU auf 1380 senken und Client neu starten.",
		Tags:            created.Tags,
		EditorName:      "AB",
		ChangeSummary:   "MTU-Wert präzisiert",
	})
	if err != nil {
		t.Fatalf("artikel aktualisieren: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("erwartete version 2, erhalten %d", updated.Version)
	}

	// 3. Veraltete Version -> ErrVersionConflict inkl. aktuellem Server-Stand
	latest, err := store.UpdateArticle(ctx, created.ID, UpdateArticleInput{
		ExpectedVersion: 1,
		TemplateType:    model.TemplateProblemSolution,
		Title:           "Konflikt-Änderung",
		Status:          model.StatusActive,
		SolutionBody:    "Alt",
		EditorName:      "MK",
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("erwartete ErrVersionConflict, erhalten: %v", err)
	}
	if latest.Version != 2 || latest.ErrorCodes != "ERR_VPN_809, Event 20227" {
		t.Fatalf("konflikt-snapshot fehlerhaft: %+v", latest)
	}

	// 4. Als geprüft markieren (Version bleibt 2)
	verified, err := store.VerifyArticle(ctx, created.ID, "CD")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Version != 2 || verified.LastVerifiedBy != "CD" {
		t.Fatalf("verify fehlerhaft: %+v", verified)
	}

	// 5. Soft-Delete & Restore
	deleted, err := store.SoftDeleteArticle(ctx, created.ID)
	if err != nil || deleted.DeletedAt == nil {
		t.Fatalf("soft-delete fehlgeschlagen: %v", err)
	}
	restored, err := store.RestoreArticle(ctx, created.ID)
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restore fehlgeschlagen: %v", err)
	}

	// 6. Endgültig löschen
	if err := store.PermanentDeleteArticle(ctx, created.ID); err != nil {
		t.Fatalf("permanent delete fehlgeschlagen: %v", err)
	}
}
