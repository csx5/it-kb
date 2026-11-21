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

func TestAuthFailClosedRBACCSRFRateLimitAndRevocation(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AuthRequired = true

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _ = store.CreateArticle(context.Background(), db.CreateArticleInput{
		TemplateType: "problem_solution",
		Title:        "Test-Fehler",
		Status:       "active",
		ErrorCodes:   "0x8004010F",
		SolutionBody: "Test-Lösung",
		AuthorName:   "System",
	})
	articles, _ := store.ListAllArticles(context.Background())
	syns, _ := store.ListSynonymGroups(context.Background())
	eng := search.NewEngine(180)
	eng.Rebuild(articles, syns)

	srv, err := NewServer(cfg, store, eng, "test")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	h := srv.Handler()

	// 1. Fail-Closed ohne Login: alle Daten-Endpunkte müssen 401 liefern
	for _, path := range []string{"/api/search", "/api/articles/1", "/api/export/markdown", "/api/admin/users", "/api/admin/audit"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("erwartete 401 auf %s ohne login, erhalten %d", path, rec.Code)
		}
	}

	// 2. Status zeigt needs_setup: true
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if !strings.Contains(rec.Body.String(), `"needs_setup":true`) {
		t.Fatalf("erwartete needs_setup:true, erhalten: %s", rec.Body.String())
	}

	// 3. Initiales Admin-Setup -> 201 + HttpOnly Session-Cookie
	setupReq := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","display_name":"Chef Admin","password":"SuperSicherAdmin2026!"}`))
	setupReq.Header.Set("X-Requested-With", "it-kb")
	setupRec := httptest.NewRecorder()
	h.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusCreated {
		t.Fatalf("setup fehlgeschlagen: %d %s", setupRec.Code, setupRec.Body.String())
	}
	adminCookie := extractSessionCookie(t, setupRec)
	if !adminCookie.HttpOnly {
		t.Fatalf("session-cookie muss HttpOnly sein")
	}

	// Zweites Setup muss gesperrt sein (403)
	setupReq2 := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"hacker","password":"SuperSicherAdmin2026!"}`))
	setupReq2.Header.Set("X-Requested-With", "it-kb")
	setupRec2 := httptest.NewRecorder()
	h.ServeHTTP(setupRec2, setupReq2)
	if setupRec2.Code != http.StatusForbidden {
		t.Fatalf("zweites setup hätte 403 liefern müssen, erhalten %d", setupRec2.Code)
	}

	// 4. CSRF-Prüfung: POST mit Cookie aber ohne X-Requested-With / Sec-Fetch-Site -> 403
	csrfReq := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewBufferString(`{"username":"leser","role":"reader","password":"LeserPasswort2026!"}`))
	csrfReq.AddCookie(adminCookie)
	csrfRec := httptest.NewRecorder()
	h.ServeHTTP(csrfRec, csrfReq)
	if csrfRec.Code != http.StatusForbidden {
		t.Fatalf("erwartete 403 bei fehlendem CSRF-Header, erhalten %d", csrfRec.Code)
	}

	// 5. Admin legt Leser und Bearbeiter an
	readerRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/admin/users", `{"username":"leser","display_name":"Max Leser","role":"reader","password":"LeserPasswort2026!"}`)
	if readerRec.Code != http.StatusCreated {
		t.Fatalf("leser anlegen: %d %s", readerRec.Code, readerRec.Body.String())
	}
	editorRec := doAuthJSON(h, adminCookie, http.MethodPost, "/api/admin/users", `{"username":"bearbeiter","display_name":"Anna IT","role":"editor","password":"EditorPasswort2026!"}`)
	if editorRec.Code != http.StatusCreated {
		t.Fatalf("bearbeiter anlegen: %d %s", editorRec.Code, editorRec.Body.String())
	}
	var edParsed struct{ User struct{ ID int64 `json:"id"` } `json:"user"` }
	_ = json.Unmarshal(editorRec.Body.Bytes(), &edParsed)

	// 6. Leser einloggen -> darf lesen, darf NICHT erstellen (403)
	readerCookie := loginAs(t, h, "leser", "LeserPasswort2026!")
	rec = doAuthJSON(h, readerCookie, http.MethodGet, "/api/search?q=0x8004010F", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("leser suche fehlgeschlagen: %d", rec.Code)
	}
	rec = doAuthJSON(h, readerCookie, http.MethodPost, "/api/articles", `{"title":"Test","solution_body":"Test"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("leser hätte 403 beim erstellen bekommen müssen, erhalten %d", rec.Code)
	}

	// 7. Bearbeiter einloggen -> darf erstellen, darf NICHT in Admin-Konsole (403)
	editorCookie := loginAs(t, h, "bearbeiter", "EditorPasswort2026!")
	rec = doAuthJSON(h, editorCookie, http.MethodPost, "/api/articles", `{"template_type":"howto","title":"VPN Check","solution_body":"Ping testen"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"author_name":"Anna IT"`) {
		t.Fatalf("bearbeiter erstellen fehlgeschlagen oder falscher autor: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAuthJSON(h, editorCookie, http.MethodGet, "/api/admin/users", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bearbeiter hätte 403 in admin-konsole bekommen müssen, erhalten %d", rec.Code)
	}

	// 8. Admin deaktiviert Bearbeiter -> bestehende Session wird SOFORT ungültig (401)!
	rec = doAuthJSON(h, adminCookie, http.MethodPut, "/api/admin/users/"+strconv.FormatInt(edParsed.User.ID, 10), `{"display_name":"Anna IT","role":"editor","is_active":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("deaktivieren fehlgeschlagen: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAuthJSON(h, editorCookie, http.MethodGet, "/api/search", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("deaktivierter benutzer hätte sofort 401 bekommen müssen, erhalten %d", rec.Code)
	}

	// 9. Brute-Force Rate-Limit testen (5 Fehlversuche -> 6. Versuch 429)
	for i := 0; i < 5; i++ {
		badReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"leser","password":"FalschPassword123"}`))
		badReq.Header.Set("X-Requested-With", "it-kb")
		badReq.RemoteAddr = "10.0.0.55:12345"
		badRec := httptest.NewRecorder()
		h.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusUnauthorized {
			t.Fatalf("versuch %d erwartete 401, erhalten %d", i+1, badRec.Code)
		}
	}
	sixthReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"leser","password":"LeserPasswort2026!"}`))
	sixthReq.Header.Set("X-Requested-With", "it-kb")
	sixthReq.RemoteAddr = "10.0.0.55:12345"
	sixthRec := httptest.NewRecorder()
	h.ServeHTTP(sixthRec, sixthReq)
	if sixthRec.Code != http.StatusTooManyRequests {
		t.Fatalf("erwartete 429 nach 5 fehlversuchen, erhalten %d", sixthRec.Code)
	}

	// 10. Audit-Log enthält die Sicherheitsereignisse
	auditRec := doAuthJSON(h, adminCookie, http.MethodGet, "/api/admin/audit", "")
	if auditRec.Code != http.StatusOK || !strings.Contains(auditRec.Body.String(), "setup_admin") || !strings.Contains(auditRec.Body.String(), "login_failed") {
		t.Fatalf("audit-log unvollständig: %d %s", auditRec.Code, auditRec.Body.String())
	}
}

func extractSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("session-cookie fehlt in antwort")
	return nil
}

func loginAs(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("X-Requested-With", "it-kb")
	req.RemoteAddr = "10.0.0.99:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login für %s fehlgeschlagen: %d %s", username, rec.Code, rec.Body.String())
	}
	return extractSessionCookie(t, rec)
}

func doAuthJSON(h http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "it-kb")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
