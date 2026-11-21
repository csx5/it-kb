package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestArticleCRUDConflictAndAdminProtection(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tmp
	cfg.AuthRequired = false
	cfg.AdminPassword = "geheim-admin"
	cfg.SeedDemoArticles = false

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(context.Background(), false); err != nil {
		t.Fatalf("seed: %v", err)
	}

	eng := search.NewEngine(180)
	srv, err := NewServer(cfg, store, eng, "test")
	if err != nil {
		t.Fatalf("server erstellen: %v", err)
	}
	h := srv.Handler()

	// 1. Artikel erstellen
	createBody := `{"template_type":"problem_solution","title":"Exchange Zertifikat abgelaufen","status":"active","error_codes":"0x80090322","symptoms":"Outlook zeigt Zertifikatswarnung","solution_body":"Neues Zertifikat im IIS binden.","tags":["exchange","tls"],"author_name":"MK"}`
	req := httptest.NewRequest(http.MethodPost, "/api/articles", bytes.NewBufferString(createBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("erwartete 201, erhalten %d: %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Article struct {
			ID      int64 `json:"id"`
			Version int   `json:"version"`
		} `json:"article"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createResp)
	idStr := strconv.FormatInt(createResp.Article.ID, 10)

	// 2. Live-Indexierung prüfen
	req = httptest.NewRequest(http.MethodGet, "/api/search?q=0x80090322", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("Exchange Zertifikat abgelaufen")) {
		t.Fatalf("artikel nicht im live-suchindex gefunden: %d %s", rec.Code, rec.Body.String())
	}

	// 3. Soft-Lock testen
	req = httptest.NewRequest(http.MethodPost, "/api/articles/"+idStr+"/lock", bytes.NewBufferString(`{"editor":"MK"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lock heartbeat fehlgeschlagen: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/articles/"+idStr+"/lock", bytes.NewBufferString(`{"editor":"AB"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"is_other":true`)) {
		t.Fatalf("erwartete soft-lock-hinweis für zweiten bearbeiter: %s", rec.Body.String())
	}

	// 4. Update auf Version 2
	updBody := `{"expected_version":1,"template_type":"problem_solution","title":"Exchange Zertifikat erneuern","status":"active","error_codes":"0x80090322","symptoms":"Zertifikatswarnung","solution_body":"IIS-Bindung aktualisieren.","tags":["exchange"],"editor_name":"AB","change_summary":"Titel präzisiert"}`
	req = httptest.NewRequest(http.MethodPut, "/api/articles/"+idStr, bytes.NewBufferString(updBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update fehlgeschlagen: %d %s", rec.Code, rec.Body.String())
	}

	// 5. Konflikt provozieren (noch mit expected_version: 1)
	req = httptest.NewRequest(http.MethodPut, "/api/articles/"+idStr, bytes.NewBufferString(updBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("server_article")) {
		t.Fatalf("erwartete 409 Conflict mit server_article, erhalten %d: %s", rec.Code, rec.Body.String())
	}

	// 6. Endgültiges Löschen ohne Admin-Passwort -> 401
	req = httptest.NewRequest(http.MethodDelete, "/api/articles/"+idStr+"/permanent", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("erwartete 401 ohne admin-passwort, erhalten %d", rec.Code)
	}

	// 7. Endgültiges Löschen mit Admin-Passwort -> 200
	req = httptest.NewRequest(http.MethodDelete, "/api/articles/"+idStr+"/permanent", nil)
	req.Header.Set("X-Admin-Password", "geheim-admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("erwartete 200 mit admin-passwort, erhalten %d: %s", rec.Code, rec.Body.String())
	}
	_ = filepath.Join(tmp, "done")
}
