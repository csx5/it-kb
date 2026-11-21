package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestRevisionsRestoreSynonymsAndCategories(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = false
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
		t.Fatalf("server: %v", err)
	}
	h := srv.Handler()

	// 1. Artikel erstellen (v1) und aktualisieren (v2)
	rec := doJSON(h, http.MethodPost, "/api/articles", `{"template_type":"howto","title":"Firewall-Port öffnen","status":"active","solution_body":"Port 443 freigeben.","author_name":"MK"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var cr struct{ Article struct{ ID int64 `json:"id"` } `json:"article"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &cr)
	idStr := strconv.FormatInt(cr.Article.ID, 10)

	rec = doJSON(h, http.MethodPut, "/api/articles/"+idStr, `{"expected_version":1,"template_type":"howto","title":"Firewall-Port 8443","status":"active","solution_body":"Port 8443 freigeben.","editor_name":"AB","change_summary":"Port geändert"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// 2. Revisionen abrufen (2 Stück) und v1 wiederherstellen -> erzeugt v3
	rec = doJSON(h, http.MethodGet, "/api/articles/"+idStr+"/revisions", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Port geändert") {
		t.Fatalf("revisions: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(h, http.MethodPost, "/api/articles/"+idStr+"/revisions/1/restore", `{"editor_name":"CD"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":3`) || !strings.Contains(rec.Body.String(), "Port 443 freigeben.") {
		t.Fatalf("restore revision: %d %s", rec.Code, rec.Body.String())
	}

	// 3. Neue Synonym-Gruppe anlegen -> muss sofort in der Suche greifen
	rec = doJSON(h, http.MethodPost, "/api/synonyms", `{"terms":["fw","firewall","paketfilter"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("synonym create: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(h, http.MethodGet, "/api/search?q=paketfilter", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Firewall-Port öffnen") {
		t.Fatalf("synonym suche griff nicht: %d %s", rec.Code, rec.Body.String())
	}

	// 4. Kategorie anlegen und wieder löschen
	rec = doJSON(h, http.MethodPost, "/api/categories", `{"name":"Telefonie","description":"VoIP & Teams","sort_order":70}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("category create: %d %s", rec.Code, rec.Body.String())
	}
	var cat struct{ ID int64 `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	rec = doJSON(h, http.MethodDelete, "/api/categories/"+strconv.FormatInt(cat.ID, 10), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("category delete: %d %s", rec.Code, rec.Body.String())
	}

	// 5. Soft-Delete & Papierkorb leeren
	rec = doJSON(h, http.MethodDelete, "/api/articles/"+idStr, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d", rec.Code)
	}
	rec = doJSON(h, http.MethodDelete, "/api/trash", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"deleted":1`) {
		t.Fatalf("empty trash: %d %s", rec.Code, rec.Body.String())
	}
}

func doJSON(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != "" {
		buf = bytes.NewBufferString(body)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
