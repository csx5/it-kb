package web

import (
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/csx5/it-kb/internal/auth"
	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/markdown"
	"github.com/csx5/it-kb/internal/media"
	"github.com/csx5/it-kb/internal/model"
	"github.com/csx5/it-kb/internal/search"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	cfg          config.Config
	store        *db.Store
	engine       *search.Engine
	md           *markdown.Renderer
	media        *media.Storage
	locks        *LockManager
	userLimiter  *auth.LoginRateLimiter
	ipLimiter    *auth.LoginRateLimiter
	restoreMu    sync.RWMutex
	setupMu      sync.Mutex
	totpMu       sync.Mutex
	lastTOTPStep map[int64]int64
	orphanMinAge time.Duration
	mux          *http.ServeMux
	version      string
}

func NewServer(cfg config.Config, store *db.Store, engine *search.Engine, version string) (*Server, error) {
	s := &Server{
		cfg:    cfg,
		store:  store,
		engine: engine,
		md:     markdown.New(),
		media: media.NewStorage(cfg.MediaDir(), media.Limits{
			MaxImageBytes:      cfg.MaxImageMB * 1024 * 1024,
			MaxVideoBytes:      cfg.MaxVideoMB * 1024 * 1024,
			MaxAttachmentBytes: cfg.MaxAttachmentMB * 1024 * 1024,
		}),
		locks:        NewLockManager(35 * time.Second),
		userLimiter:  auth.NewLoginRateLimiter(5, 5*time.Minute, 5*time.Minute),
		ipLimiter:    auth.NewLoginRateLimiter(15, 5*time.Minute, 5*time.Minute),
		lastTOTPStep: make(map[int64]int64),
		orphanMinAge: time.Hour,
		mux:          http.NewServeMux(),
		version:      version,
	}

	if err := s.registerRoutes(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.restoreGuard(s.mux))
}

func (s *Server) restoreGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/backup/restore" {
			next.ServeHTTP(w, r)
			return
		}
		s.restoreMu.RLock()
		defer s.restoreMu.RUnlock()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerRoutes() error {
	// Öffentlich (Health + Authentifizierung)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleInitialAdminSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/auth/password", s.requireAuth(model.RoleReader, s.handleChangeOwnPassword))
	s.mux.HandleFunc("POST /api/auth/totp/setup", s.requireAuth(model.RoleReader, s.handleTOTPSetup))
	s.mux.HandleFunc("POST /api/auth/totp/enable", s.requireAuth(model.RoleReader, s.handleTOTPEnable))
	s.mux.HandleFunc("POST /api/auth/totp/disable", s.requireAuth(model.RoleReader, s.handleTOTPDisable))

	// Lesen & Feedback (Reader+)
	s.mux.HandleFunc("GET /api/meta", s.requireAuth(model.RoleReader, s.handleMeta))
	s.mux.HandleFunc("GET /api/search", s.requireAuth(model.RoleReader, s.handleSearch))
	s.mux.HandleFunc("GET /api/articles/{id}", s.requireAuth(model.RoleReader, s.handleGetArticle))
	s.mux.HandleFunc("GET /api/articles/{id}/revisions", s.requireAuth(model.RoleReader, s.handleListRevisions))
	s.mux.HandleFunc("POST /api/articles/{id}/helpful", s.requireAuth(model.RoleReader, s.handleHelpfulArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/comments", s.requireAuth(model.RoleReader, s.handleCreateComment))
	s.mux.HandleFunc("DELETE /api/articles/{id}/comments/{commentId}", s.requireAuth(model.RoleReader, s.handleDeleteComment))
	s.mux.HandleFunc("GET /media/{id}", s.requireAuth(model.RoleReader, s.handleServeMedia))
	s.mux.HandleFunc("GET /api/categories", s.requireAuth(model.RoleReader, s.handleListCategories))
	s.mux.HandleFunc("GET /api/synonyms", s.requireAuth(model.RoleReader, s.handleListSynonyms))

	// Bearbeiten (Editor+)
	s.mux.HandleFunc("POST /api/preview", s.requireAuth(model.RoleEditor, s.handlePreviewMarkdown))
	s.mux.HandleFunc("POST /api/articles", s.requireAuth(model.RoleEditor, s.handleCreateArticle))
	s.mux.HandleFunc("PUT /api/articles/{id}", s.requireAuth(model.RoleEditor, s.handleUpdateArticle))
	s.mux.HandleFunc("DELETE /api/articles/{id}", s.requireAuth(model.RoleEditor, s.handleSoftDeleteArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/restore", s.requireAuth(model.RoleEditor, s.handleRestoreArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/verify", s.requireAuth(model.RoleEditor, s.handleVerifyArticle))
	s.mux.HandleFunc("DELETE /api/articles/{id}/verify", s.requireAuth(model.RoleEditor, s.handleUnverifyArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/pin", s.requireAuth(model.RoleEditor, s.handlePinArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/lock", s.requireAuth(model.RoleEditor, s.handleHeartbeatLock))
	s.mux.HandleFunc("DELETE /api/articles/{id}/lock", s.requireAuth(model.RoleEditor, s.handleReleaseLock))
	s.mux.HandleFunc("POST /api/articles/{id}/revisions/{version}/restore", s.requireAuth(model.RoleEditor, s.handleRestoreRevision))
	s.mux.HandleFunc("POST /api/media", s.requireAuth(model.RoleEditor, s.handleUploadMedia))
	s.mux.HandleFunc("POST /api/categories", s.requireAuth(model.RoleEditor, s.handleCreateCategory))
	s.mux.HandleFunc("PUT /api/categories/{id}", s.requireAuth(model.RoleEditor, s.handleUpdateCategory))
	s.mux.HandleFunc("DELETE /api/categories/{id}", s.requireAuth(model.RoleEditor, s.handleDeleteCategory))
	s.mux.HandleFunc("POST /api/synonyms", s.requireAuth(model.RoleEditor, s.handleCreateSynonym))
	s.mux.HandleFunc("PUT /api/synonyms/{id}", s.requireAuth(model.RoleEditor, s.handleUpdateSynonym))
	s.mux.HandleFunc("DELETE /api/synonyms/{id}", s.requireAuth(model.RoleEditor, s.handleDeleteSynonym))
	s.mux.HandleFunc("GET /api/export/markdown", s.requireAuth(model.RoleEditor, s.handleExportMarkdown))

	// Admin-Konsole (Admin)
	s.mux.HandleFunc("DELETE /api/articles/{id}/permanent", s.requireAuth(model.RoleAdmin, s.handlePermanentDeleteArticle))
	s.mux.HandleFunc("GET /api/media/orphans", s.requireAuth(model.RoleAdmin, s.handleListOrphanMedia))
	s.mux.HandleFunc("POST /api/media/cleanup", s.requireAuth(model.RoleAdmin, s.handleCleanupOrphanMedia))
	s.mux.HandleFunc("PUT /api/settings", s.requireAuth(model.RoleAdmin, s.handleUpdateSettings))
	s.mux.HandleFunc("DELETE /api/trash", s.requireAuth(model.RoleAdmin, s.handleEmptyTrash))
	s.mux.HandleFunc("GET /api/backup/full", s.requireAuth(model.RoleAdmin, s.handleFullBackupDownload))
	// Restore prüft Auth intern unter restoreMu.RLock und wechselt danach exklusiv auf restoreMu.Lock
	s.mux.HandleFunc("POST /api/backup/restore", s.handleFullBackupRestoreEntry)
	s.mux.HandleFunc("GET /api/admin/users", s.requireAuth(model.RoleAdmin, s.handleAdminListUsers))
	s.mux.HandleFunc("POST /api/admin/users", s.requireAuth(model.RoleAdmin, s.handleAdminCreateUser))
	s.mux.HandleFunc("PUT /api/admin/users/{id}", s.requireAuth(model.RoleAdmin, s.handleAdminUpdateUser))
	s.mux.HandleFunc("POST /api/admin/users/{id}/password", s.requireAuth(model.RoleAdmin, s.handleAdminResetUserPassword))
	s.mux.HandleFunc("POST /api/admin/users/{id}/totp-reset", s.requireAuth(model.RoleAdmin, s.handleAdminResetUserTOTP))
	s.mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAuth(model.RoleAdmin, s.handleAdminDeleteUser))
	s.mux.HandleFunc("GET /api/admin/audit", s.requireAuth(model.RoleAdmin, s.handleAdminListAudit))

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(sub))
	s.mux.Handle("/", fileServer)
	return nil
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'")
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin prüft das optionale Admin-Passwort im Modus --auth=false (mit Rate-Limit & Audit-Log).
func (s *Server) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminPassword == "" {
			next(w, r)
			return
		}
		ip := clientIP(r)
		if ok, wait := s.ipLimiter.Allow("adminpw|" + ip); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "Zu viele Fehlversuche")
			return
		}
		provided := r.Header.Get("X-Admin-Password")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.AdminPassword)) != 1 {
			s.ipLimiter.RecordFailure("adminpw|" + ip)
			s.store.WriteAuditLog(r.Context(), nil, "Admin-Passwort", "admin_auth_failed", r.URL.Path, ip, "Ungültiges Admin-Passwort")
			writeError(w, http.StatusUnauthorized, "Admin-Passwort erforderlich oder ungültig")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.version,
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Kategorien konnten nicht geladen werden")
		return
	}
	settings, _ := s.store.GetSettings(r.Context())
	siteTitle := s.cfg.SiteTitle
	if v := strings.TrimSpace(settings["site_title"]); v != "" {
		siteTitle = v
	}
	reminderDays := s.cfg.ReviewReminderDays
	if v := strings.TrimSpace(settings["review_reminder_days"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			reminderDays = n
		}
	}
	resp := map[string]any{
		"site_title":           siteTitle,
		"version":              s.version,
		"auth_required":        s.cfg.AuthRequired,
		"admin_protected":      !s.cfg.AuthRequired && s.cfg.AdminPassword != "",
		"review_reminder_days": reminderDays,
		"categories":           cats,
	}
	if u := currentUser(r); u != nil {
		resp["user"] = u
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sq := model.SearchQuery{
		Text:           q.Get("q"),
		Tag:            strings.TrimSpace(q.Get("tag")),
		Status:         strings.TrimSpace(q.Get("status")),
		TemplateType:   strings.TrimSpace(q.Get("template")),
		Author:         strings.TrimSpace(q.Get("author")),
		NeedsReview:    q.Get("needs_review") == "true" || q.Get("needs_review") == "1",
		IncludeDeleted: q.Get("deleted") == "true" || q.Get("deleted") == "1",
	}
	if catStr := q.Get("category_id"); catStr != "" {
		if id, err := strconv.ParseInt(catStr, 10, 64); err == nil {
			sq.CategoryID = &id
		}
	}
	if limStr := q.Get("limit"); limStr != "" {
		if lim, err := strconv.Atoi(limStr); err == nil {
			sq.Limit = lim
		}
	}

	hits := s.engine.Search(sq)
	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(hits),
		"hits":  hits,
	})
}

func (s *Server) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	article, err := s.store.GetArticleByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Artikel konnte nicht geladen werden")
		return
	}

	symptomsHTML, _ := s.md.RenderHTML(article.Symptoms)
	solutionHTML, _ := s.md.RenderHTML(article.SolutionBody)
	comments, _ := s.store.ListArticleComments(r.Context(), article.ID)
	if comments == nil {
		comments = []model.ArticleComment{}
	}
	related := s.engine.RelatedArticles(article.ID, 4)
	if related == nil {
		related = []model.Article{}
	}
	lock, hasLock := s.locks.Get(article.ID)

	resp := map[string]any{
		"article":          article,
		"symptoms_html":    symptomsHTML,
		"solution_html":    solutionHTML,
		"comments":         comments,
		"related_articles": related,
	}
	if hasLock {
		resp["active_lock"] = lock
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Kategorien konnten nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *Server) handleListSynonyms(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListSynonymGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Synonyme konnten nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
