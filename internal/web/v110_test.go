package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestTemplatesCRUDAndMarkdownImportEndpoints(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = true

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer store.Close()
	_ = store.SeedInitialData(context.Background())

	eng := search.NewEngine(180)
	srv, err := NewServer(cfg, store, eng, "test")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	h := srv.Handler()

	setupRec := doAuthJSON(h, nil, http.MethodPost, "/api/auth/setup", `{"username":"admin","display_name":"Chef","password":"SuperSicherAdmin2026!"}`)
	adminCookie := extractSessionCookie(t, setupRec)

	// Leser anlegen -> darf keine Vorlage anlegen / Markdown importieren (403)
	_ = doAuthJSON(h, adminCookie, http.MethodPost, "/api/admin/users", `{"username":"leser","display_name":"Leser","role":"reader","password":"LeserPasswort2026!"}`)
	readerCookie := loginAs(t, h, "leser", "LeserPasswort2026!")

	// 1. Vorlagen CRUD
	createBody := `{"name":"Drucker-Triage","template_type":"problem_solution","title_prefix":"[Drucker] ","error_codes":"0x0000011b","symptoms":"Verbindungsfehler","solution_body":"Registry-Fix pruefen","tags":["drucker","spooler"],"sort_order":10}`
	if rec := doAuthJSON(h, readerCookie, http.MethodPost, "/api/templates", createBody); rec.Code != http.StatusForbidden {
		t.Fatalf("leser hätte 403 bekommen müssen, erhalten %d", rec.Code)
	}

	cRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/templates", createBody)
	if cRec.Code != http.StatusCreated {
		t.Fatalf("vorlage anlegen fehlgeschlagen: %d %s", cRec.Code, cRec.Body.String())
	}
	var cParsed struct {
		Template struct {
			ID int64 `json:"id"`
		} `json:"template"`
	}
	_ = json.Unmarshal(cRec.Body.Bytes(), &cParsed)

	lRec := doAuthJSON(h, readerCookie, http.MethodGet, "/api/templates", "")
	if lRec.Code != http.StatusOK || !strings.Contains(lRec.Body.String(), "Drucker-Triage") {
		t.Fatalf("vorlagen laden fehlgeschlagen: %d %s", lRec.Code, lRec.Body.String())
	}

	uRec := doAuthJSON(h, adminCookie, http.MethodPut, "/api/templates/"+strconv.FormatInt(cParsed.Template.ID, 10), `{"name":"Drucker-Triage v2","template_type":"howto","solution_body":"Spooler neu starten","tags":["spooler"]}`)
	if uRec.Code != http.StatusOK || !strings.Contains(uRec.Body.String(), "Drucker-Triage v2") {
		t.Fatalf("vorlage aktualisieren fehlgeschlagen: %d %s", uRec.Code, uRec.Body.String())
	}

	// 2. Einzel-Markdown Import
	mdPayload := "---\ntitle: \"Teams Cache leeren\"\ntemplate: \"howto\"\ntags:\n  - \"teams\"\n---\n# Teams Cache leeren\n\n## Anleitung\n\nApp beenden und `%appdata%` leeren.\n"
	impRec := uploadMultipartFile(t, h, adminCookie, "/api/import/markdown", "file", "teams.md", []byte(mdPayload))
	if impRec.Code != http.StatusCreated || !strings.Contains(impRec.Body.String(), "Teams Cache leeren") {
		t.Fatalf("md-import fehlgeschlagen: %d %s", impRec.Code, impRec.Body.String())
	}

	// Suchindex muss importierten Artikel sofort finden
	searchRec := doAuthJSON(h, readerCookie, http.MethodGet, "/api/search?q=Teams", "")
	if searchRec.Code != http.StatusOK || !strings.Contains(searchRec.Body.String(), "Teams Cache leeren") {
		t.Fatalf("importierter artikel nicht im suchindex: %d %s", searchRec.Code, searchRec.Body.String())
	}

	// 3. ZIP Markdown Import
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, _ := zw.Create("articles/dns-test.md")
	_, _ = fw.Write([]byte("# DNS Testartikel\n\n`ipconfig /registerdns`\n"))
	_ = zw.Close()

	zipImpRec := uploadMultipartFile(t, h, adminCookie, "/api/import/markdown", "file", "export.zip", zipBuf.Bytes())
	if zipImpRec.Code != http.StatusOK || !strings.Contains(zipImpRec.Body.String(), `"articles_imported":1`) {
		t.Fatalf("zip-import fehlgeschlagen: %d %s", zipImpRec.Code, zipImpRec.Body.String())
	}

	// 4. Vorlage löschen
	dRec := doAuthJSON(h, adminCookie, http.MethodDelete, "/api/templates/"+strconv.FormatInt(cParsed.Template.ID, 10), "")
	if dRec.Code != http.StatusOK {
		t.Fatalf("vorlage löschen fehlgeschlagen: %d %s", dRec.Code, dRec.Body.String())
	}
}

func uploadMultipartFile(t *testing.T, h http.Handler, cookie *http.Cookie, path, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "it-kb")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
