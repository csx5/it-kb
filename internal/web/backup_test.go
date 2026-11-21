package web

import (
	"archive/zip"
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestBackupExportRestoreAndZipSlipRejection(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = false
	cfg.AdminPassword = "secret"
	cfg.SeedDemoArticles = true

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(context.Background(), true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	articles, _ := store.ListAllArticles(context.Background())
	syns, _ := store.ListSynonymGroups(context.Background())
	eng := search.NewEngine(180)
	eng.Rebuild(articles, syns)

	srv, err := NewServer(cfg, store, eng, "test")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	h := srv.Handler()

	// 1. Portabler Markdown-Export (offen)
	req := httptest.NewRequest(http.MethodGet, "/api/export/markdown", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("markdown export fehlgeschlagen: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	// 2. Voll-Backup ohne Admin-Passwort -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/backup/full", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("erwartete 401 ohne admin-passwort, erhalten %d", rec.Code)
	}

	// 3. Voll-Backup mit Admin-Passwort -> 200 ZIP
	req = httptest.NewRequest(http.MethodGet, "/api/backup/full", nil)
	req.Header.Set("X-Admin-Password", "secret")
	backupRec := httptest.NewRecorder()
	h.ServeHTTP(backupRec, req)
	if backupRec.Code != http.StatusOK {
		t.Fatalf("voll-backup fehlgeschlagen: %d", backupRec.Code)
	}

	// 4. Zip-Slip-Archiv beim Restore ablehnen
	var badZip bytes.Buffer
	zw := zip.NewWriter(&badZip)
	w, _ := zw.Create("../evil.txt")
	_, _ = w.Write([]byte("pwned"))
	_ = zw.Close()

	slipRec := uploadBackupZIP(t, h, badZip.Bytes(), "secret")
	if slipRec.Code != http.StatusBadRequest || !strings.Contains(slipRec.Body.String(), "zip-slip") {
		t.Fatalf("zip-slip nicht abgelehnt: %d %s", slipRec.Code, slipRec.Body.String())
	}

	// 5. Gültiges Voll-Backup wiederherstellen -> 200
	resRec := uploadBackupZIP(t, h, backupRec.Body.Bytes(), "secret")
	if resRec.Code != http.StatusOK {
		t.Fatalf("restore fehlgeschlagen: %d %s", resRec.Code, resRec.Body.String())
	}
}

func uploadBackupZIP(t *testing.T, h http.Handler, zipBytes []byte, adminPw string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(zipBytes)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if adminPw != "" {
		req.Header.Set("X-Admin-Password", adminPw)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
