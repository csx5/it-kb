package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestMediaUploadWhitelistAttachmentHeadersAndOrphanCleanup(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = false

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	eng := search.NewEngine(180)
	srv, err := NewServer(cfg, store, eng, "test")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	h := srv.Handler()

	// 1. Nicht erlaubte Datei (.exe) & als .webm getarntes HTML (XSS) müssen abgelehnt werden
	rec := uploadTestFile(t, h, "malware.exe", []byte("MZ..."))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("erwartete 400 für .exe, erhalten %d: %s", rec.Code, rec.Body.String())
	}
	rec = uploadTestFile(t, h, "evil.webm", []byte("<html><script>alert(1)</script></html>"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("als .webm getarntes HTML hätte 400 liefern müssen, erhalten %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Erlaubtes IT-Skript (.ps1) hochladen
	rec = uploadTestFile(t, h, "reset-spooler.ps1", []byte("Restart-Service Spooler\n"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("erwartete 201 für .ps1, erhalten %d: %s", rec.Code, rec.Body.String())
	}
	var upResp struct {
		URL      string `json:"url"`
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &upResp)
	if !strings.HasPrefix(upResp.URL, "/media/") || !strings.Contains(upResp.Markdown, "reset-spooler.ps1") {
		t.Fatalf("unerwartete upload-antwort: %+v", upResp)
	}

	// 3. Download-Header für .ps1 prüfen (erzwungener Attachment-Download!)
	req := httptest.NewRequest(http.MethodGet, upResp.URL, nil)
	dlRec := httptest.NewRecorder()
	h.ServeHTTP(dlRec, req)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("download fehlgeschlagen: %d", dlRec.Code)
	}
	if !strings.Contains(dlRec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("erwartete Content-Disposition attachment, erhalten: %s", dlRec.Header().Get("Content-Disposition"))
	}
	if dlRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("erwartete X-Content-Type-Options nosniff")
	}

	// 4. Nicht referenzierte Datei erscheint in Orphans und wird per Cleanup entfernt
	req = httptest.NewRequest(http.MethodGet, "/api/media/orphans", nil)
	orRec := httptest.NewRecorder()
	h.ServeHTTP(orRec, req)
	if orRec.Code != http.StatusOK || !strings.Contains(orRec.Body.String(), `"count":1`) {
		t.Fatalf("erwartete 1 verwaiste datei, erhalten %d: %s", orRec.Code, orRec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/media/cleanup", nil)
	clRec := httptest.NewRecorder()
	h.ServeHTTP(clRec, req)
	if clRec.Code != http.StatusOK || !strings.Contains(clRec.Body.String(), `"deleted":1`) {
		t.Fatalf("cleanup fehlgeschlagen: %d %s", clRec.Code, clRec.Body.String())
	}
}

func uploadTestFile(t *testing.T, h http.Handler, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(content)
	_ = mw.WriteField("uploaded_by", "MK")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
