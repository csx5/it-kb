package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/csx5/it-kb/internal/auth"
	"github.com/csx5/it-kb/internal/backup"
	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
)

func TestTOTP2FAEncryptedBackupHelpfulPinCommentsAndRelated(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = true
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

	// 1. Admin einrichten
	setupRec := httptest.NewRecorder()
	h.ServeHTTP(setupRec, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","display_name":"Admin","password":"SuperSicherAdmin2026!"}`)))
	if setupRec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", setupRec.Code, setupRec.Body.String())
	}
	adminCookie := extractSessionCookie(t, setupRec)

	// 2. TOTP 2FA einrichten & aktivieren
	totpRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/auth/totp/setup", `{}`)
	if totpRec.Code != http.StatusOK {
		t.Fatalf("totp setup: %d %s", totpRec.Code, totpRec.Body.String())
	}
	var totpSetup struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	_ = json.Unmarshal(totpRec.Body.Bytes(), &totpSetup)
	code, err := auth.GenerateTOTPCode(totpSetup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	enRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/auth/totp/enable", `{"code":"`+code+`"}`)
	if enRec.Code != http.StatusOK {
		t.Fatalf("totp enable: %d %s", enRec.Code, enRec.Body.String())
	}

	// Login ohne TOTP-Code muss totp_required melden (ohne Session-Cookie)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"SuperSicherAdmin2026!"}`))
	loginReq.RemoteAddr = "10.0.0.10:1234"
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK || !strings.Contains(loginRec.Body.String(), `"totp_required":true`) {
		t.Fatalf("erwartete totp_required, erhalten %d %s", loginRec.Code, loginRec.Body.String())
	}

	// Login mit gültigem TOTP-Code gelingt
	loginReq2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"SuperSicherAdmin2026!","totp_code":"`+code+`"}`))
	loginReq2.RemoteAddr = "10.0.0.10:1234"
	loginRec2 := httptest.NewRecorder()
	h.ServeHTTP(loginRec2, loginReq2)
	if loginRec2.Code != http.StatusOK {
		t.Fatalf("totp login fehlgeschlagen: %d %s", loginRec2.Code, loginRec2.Body.String())
	}

	// 3. Helpful, Pin, Kommentar & Related Articles testen
	rec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/articles/1/helpful", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"helpful_count":1`) {
		t.Fatalf("helpful: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAuthJSON(h, adminCookie, http.MethodPost, "/api/articles/2/pin", `{"pinned":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_pinned":true`) {
		t.Fatalf("pin: %d %s", rec.Code, rec.Body.String())
	}
	// Leere Suche muss angepinnten Artikel 2 ganz oben liefern
	searchRec := doAuthJSON(h, adminCookie, http.MethodGet, "/api/search", "")
	var sr struct {
		Hits []struct {
			Article struct{ ID int64 `json:"id"` } `json:"article"`
		} `json:"hits"`
	}
	_ = json.Unmarshal(searchRec.Body.Bytes(), &sr)
	if len(sr.Hits) == 0 || sr.Hits[0].Article.ID != 2 {
		t.Fatalf("angepinnter artikel 2 stand nicht oben: %+v", sr.Hits)
	}

	// Kommentar anlegen & lesen
	cRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/articles/1/comments", `{"body":"Gilt auch unter Outlook 2024."}`)
	if cRec.Code != http.StatusCreated {
		t.Fatalf("kommentar anlegen: %d %s", cRec.Code, cRec.Body.String())
	}
	var cParsed struct{ Comment struct{ ID int64 `json:"id"` } `json:"comment"` }
	_ = json.Unmarshal(cRec.Body.Bytes(), &cParsed)

	artRec := doAuthJSON(h, adminCookie, http.MethodGet, "/api/articles/1", "")
	if artRec.Code != http.StatusOK || !strings.Contains(artRec.Body.String(), "Outlook 2024") || !strings.Contains(artRec.Body.String(), "related_articles") {
		t.Fatalf("artikel-detail unvollständig: %d %s", artRec.Code, artRec.Body.String())
	}
	delCRec := doAuthJSON(h, adminCookie, http.MethodDelete, "/api/articles/1/comments/"+strconv.FormatInt(cParsed.Comment.ID, 10), "")
	if delCRec.Code != http.StatusOK {
		t.Fatalf("kommentar löschen: %d %s", delCRec.Code, delCRec.Body.String())
	}

	// 4. Verschlüsseltes Voll-Backup (AES-256-GCM) & Restore
	dlReq := httptest.NewRequest(http.MethodGet, "/api/backup/full", nil)
	dlReq.AddCookie(adminCookie)
	dlReq.Header.Set("X-Backup-Passphrase", "SehrStarkesBackupPW2026!")
	dlRec := httptest.NewRecorder()
	h.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK || !backup.IsEncryptedBackup(dlRec.Body.Bytes()) {
		t.Fatalf("verschlüsseltes backup fehlgeschlagen: %d", dlRec.Code)
	}

	// Restore ohne Passphrase -> 422
	noPwRec := uploadEncryptedBackup(t, h, adminCookie, dlRec.Body.Bytes(), "")
	if noPwRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("erwartete 422 ohne passphrase, erhalten %d: %s", noPwRec.Code, noPwRec.Body.String())
	}

	// Restore mit korrekter Passphrase -> 200
	okRec := uploadEncryptedBackup(t, h, adminCookie, dlRec.Body.Bytes(), "SehrStarkesBackupPW2026!")
	if okRec.Code != http.StatusOK {
		t.Fatalf("restore mit passphrase fehlgeschlagen: %d %s", okRec.Code, okRec.Body.String())
	}
}

func uploadEncryptedBackup(t *testing.T, h http.Handler, cookie *http.Cookie, blob []byte, passphrase string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "backup.itkb.enc")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(blob)
	if passphrase != "" {
		_ = mw.WriteField("passphrase", passphrase)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "it-kb")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
