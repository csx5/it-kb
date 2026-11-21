package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/auth"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/model"
)

const sessionCookieName = "itkb_session"

type ctxKeyUser struct{}

// Dummy-Hash zur Verhinderung von Timing-User-Enumeration bei unbekannten Benutzernamen.
var dummyArgonHash, _ = auth.HashPassword("dummy-timing-protection-passphrase-2026")

func currentUser(r *http.Request) *model.User {
	if u, ok := r.Context().Value(ctxKeyUser{}).(*model.User); ok {
		return u
	}
	return nil
}

func actorName(r *http.Request, fallback string) string {
	if u := currentUser(r); u != nil {
		if u.DisplayName != "" {
			return u.DisplayName
		}
		return u.Username
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		return fallback
	}
	return "Unbekannt"
}

func actorUserID(r *http.Request) *int64 {
	if u := currentUser(r); u != nil {
		id := u.ID
		return &id
	}
	return nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func roleRank(role string) int {
	switch role {
	case model.RoleReader:
		return 1
	case model.RoleEditor:
		return 2
	case model.RoleAdmin:
		return 3
	default:
		return 0
	}
}

func (s *Server) requireAuth(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Falls Authentifizierung explizit per Config deaktiviert ist (reiner LAN-/Testmodus)
		if !s.cfg.AuthRequired {
			if minRole == model.RoleAdmin {
				s.RequireAdmin(next)(w, r)
				return
			}
			next(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, http.StatusUnauthorized, "Anmeldung erforderlich")
			return
		}

		tokenHash := auth.HashSessionToken(cookie.Value)
		user, err := s.store.GetSessionUser(r.Context(), tokenHash)
		if err != nil {
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "Sitzung abgelaufen oder ungültig")
			return
		}

		// CSRF-Schutz für alle zustandsändernden Anfragen bei Cookie-Authentifizierung
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !isSameOriginCSRFValid(r) {
				writeError(w, http.StatusForbidden, "CSRF-Prüfung fehlgeschlagen")
				return
			}
		}

		if roleRank(user.Role) < roleRank(minRole) {
			writeError(w, http.StatusForbidden, "Unzureichende Berechtigung für diese Aktion")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUser{}, &user)
		next(w, r.WithContext(ctx))
	}
}

func isSameOriginCSRFValid(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "it-kb" {
		return true
	}
	secFetch := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if secFetch == "same-origin" || secFetch == "none" {
		return true
	}
	return false
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, rawToken string, expires time.Time) {
	isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionDuration() time.Duration {
	h := s.cfg.SessionHours
	if h <= 0 {
		h = 12
	}
	return time.Duration(h) * time.Hour
}

// --- Öffentliche Auth-Endpunkte ---

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	userCount, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Authentifizierungsstatus konnte nicht geprüft werden")
		return
	}

	settings, _ := s.store.GetSettings(r.Context())
	siteTitle := s.cfg.SiteTitle
	if v := strings.TrimSpace(settings["site_title"]); v != "" {
		siteTitle = v
	}

	resp := map[string]any{
		"auth_required": s.cfg.AuthRequired,
		"needs_setup":   s.cfg.AuthRequired && userCount == 0,
		"authenticated": false,
		"site_title":    siteTitle,
		"version":       s.version,
	}

	if s.cfg.AuthRequired {
		if cookie, err := r.Cookie(sessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
			if user, err := s.store.GetSessionUser(r.Context(), auth.HashSessionToken(cookie.Value)); err == nil {
				resp["authenticated"] = true
				resp["user"] = user
			}
		}
	} else {
		resp["authenticated"] = true
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleInitialAdminSetup(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Benutzeranzahl konnte nicht geprüft werden")
		return
	}
	if count > 0 {
		writeError(w, http.StatusForbidden, "Ersteinrichtung wurde bereits abgeschlossen")
		return
	}

	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.store.CreateUser(r.Context(), body.Username, body.DisplayName, hash, model.RoleAdmin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawToken, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Sitzung konnte nicht erstellt werden")
		return
	}
	expires := time.Now().UTC().Add(s.sessionDuration())
	if err := s.store.CreateSession(r.Context(), tokenHash, user.ID, expires); err != nil {
		writeError(w, http.StatusInternalServerError, "Sitzung konnte nicht gespeichert werden")
		return
	}

	s.setSessionCookie(w, r, rawToken, expires)
	s.store.WriteAuditLog(r.Context(), &user.ID, user.DisplayName, "setup_admin", user.Username, clientIP(r), "Erster Administrator angelegt")
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	username := strings.ToLower(strings.TrimSpace(body.Username))
	ip := clientIP(r)
	userKey := ip + "|" + username

	if ok, wait := s.userLimiter.Allow(userKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Zu viele Fehlversuche. Bitte in %d Sekunden erneut versuchen.", int(wait.Seconds())+1))
		return
	}
	if ok, wait := s.ipLimiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Zu viele Anmeldeversuche von dieser IP. Bitte in %d Sekunden erneut versuchen.", int(wait.Seconds())+1))
		return
	}

	userWithHash, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil {
		// Dummy-Argon2id-Prüfung verhindert Timing-Rückschlüsse auf existierende Benutzernamen
		_, _ = auth.VerifyPassword(body.Password, dummyArgonHash)
		s.userLimiter.RecordFailure(userKey)
		s.ipLimiter.RecordFailure(ip)
		s.store.WriteAuditLog(r.Context(), nil, username, "login_failed", username, ip, "Unbekannter Benutzer")
		writeError(w, http.StatusUnauthorized, "Benutzername oder Passwort ungültig")
		return
	}

	valid, _ := auth.VerifyPassword(body.Password, userWithHash.PasswordHash)
	if !valid || !userWithHash.IsActive {
		s.userLimiter.RecordFailure(userKey)
		s.ipLimiter.RecordFailure(ip)
		reason := "Falsches Passwort"
		if !userWithHash.IsActive {
			reason = "Konto deaktiviert"
		}
		s.store.WriteAuditLog(r.Context(), &userWithHash.ID, userWithHash.Username, "login_failed", userWithHash.Username, ip, reason)
		writeError(w, http.StatusUnauthorized, "Benutzername oder Passwort ungültig")
		return
	}

	s.userLimiter.Reset(userKey)
	s.ipLimiter.Reset(ip)

	rawToken, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Sitzung konnte nicht erstellt werden")
		return
	}
	expires := time.Now().UTC().Add(s.sessionDuration())
	if err := s.store.CreateSession(r.Context(), tokenHash, userWithHash.ID, expires); err != nil {
		writeError(w, http.StatusInternalServerError, "Sitzung konnte nicht gespeichert werden")
		return
	}

	s.setSessionCookie(w, r, rawToken, expires)
	s.store.WriteAuditLog(r.Context(), &userWithHash.ID, userWithHash.DisplayName, "login_ok", userWithHash.Username, ip, "Anmeldung erfolgreich")
	writeJSON(w, http.StatusOK, map[string]any{"user": userWithHash.User})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		hash := auth.HashSessionToken(cookie.Value)
		if user, err := s.store.GetSessionUser(r.Context(), hash); err == nil {
			s.store.WriteAuditLog(r.Context(), &user.ID, user.DisplayName, "logout", user.Username, clientIP(r), "Abgemeldet")
		}
		_ = s.store.DeleteSession(r.Context(), hash)
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Nicht angemeldet")
		return
	}

	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	fullUser, err := s.store.GetUserByID(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Benutzer nicht gefunden")
		return
	}
	ok, _ := auth.VerifyPassword(body.CurrentPassword, fullUser.PasswordHash)
	if !ok {
		writeError(w, http.StatusBadRequest, "Aktuelles Passwort ist nicht korrekt")
		return
	}

	newHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.UpdateUserPassword(r.Context(), u.ID, newHash, true); err != nil {
		writeError(w, http.StatusInternalServerError, "Passwort konnte nicht aktualisiert werden")
		return
	}

	// Frische Session für die aktuelle Sitzung ausstellen (alle anderen Geräte sind abgemeldet)
	rawToken, tokenHash, err := auth.GenerateSessionToken()
	if err == nil {
		expires := time.Now().UTC().Add(s.sessionDuration())
		if err := s.store.CreateSession(r.Context(), tokenHash, u.ID, expires); err == nil {
			s.setSessionCookie(w, r, rawToken, expires)
		}
	}

	s.store.WriteAuditLog(r.Context(), &u.ID, u.DisplayName, "password_changed", u.Username, clientIP(r), "Eigenes Passwort geändert (andere Sitzungen beendet)")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- Admin-Konsole: Benutzerverwaltung & Audit-Log ---

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Benutzer konnten nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := s.store.CreateUser(r.Context(), body.Username, body.DisplayName, hash, body.Role)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, db.ErrUserExists) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "user_created", created.Username, clientIP(r), "Rolle: "+created.Role)
	writeJSON(w, http.StatusCreated, map[string]any{"user": created})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Benutzer-ID")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		IsActive    bool   `json:"is_active"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	updated, err := s.store.UpdateUser(r.Context(), id, body.DisplayName, body.Role, body.IsActive)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Benutzer nicht gefunden")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "user_updated", updated.Username, clientIP(r),
		fmt.Sprintf("Rolle=%s, Aktiv=%v", updated.Role, updated.IsActive))
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (s *Server) handleAdminResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Benutzer-ID")
		return
	}
	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Benutzer nicht gefunden")
		return
	}

	if err := s.store.UpdateUserPassword(r.Context(), id, hash, true); err != nil {
		writeError(w, http.StatusInternalServerError, "Passwort-Reset fehlgeschlagen")
		return
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "user_password_reset", target.Username, clientIP(r), "Passwort zurückgesetzt & Sitzungen beendet")
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Benutzer-ID")
		return
	}
	if u := currentUser(r); u != nil && u.ID == id {
		writeError(w, http.StatusBadRequest, "Das eigene Administrator-Konto kann nicht selbst gelöscht werden")
		return
	}
	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Benutzer nicht gefunden")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "user_deleted", target.Username, clientIP(r), "Benutzerkonto gelöscht")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAdminListAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListAuditLogs(r.Context(), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Audit-Log konnte nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
