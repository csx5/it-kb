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
	"time"

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
	cfg      config.Config
	store    *db.Store
	engine   *search.Engine
	md       *markdown.Renderer
	media    *media.Storage
	locks    *LockManager
	mux      *http.ServeMux
	version  string
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
		locks:   NewLockManager(35 * time.Second),
		mux:     http.NewServeMux(),
		version: version,
	}

	if err := s.registerRoutes(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.mux)
}

func (s *Server) registerRoutes() error {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("POST /api/preview", s.handlePreviewMarkdown)
	s.mux.HandleFunc("POST /api/articles", s.handleCreateArticle)
	s.mux.HandleFunc("GET /api/articles/{id}", s.handleGetArticle)
	s.mux.HandleFunc("PUT /api/articles/{id}", s.handleUpdateArticle)
	s.mux.HandleFunc("DELETE /api/articles/{id}", s.handleSoftDeleteArticle)
	s.mux.HandleFunc("POST /api/articles/{id}/restore", s.handleRestoreArticle)
	s.mux.HandleFunc("POST /api/articles/{id}/verify", s.handleVerifyArticle)
	s.mux.HandleFunc("DELETE /api/articles/{id}/permanent", s.RequireAdmin(s.handlePermanentDeleteArticle))
	s.mux.HandleFunc("POST /api/articles/{id}/lock", s.handleHeartbeatLock)
	s.mux.HandleFunc("DELETE /api/articles/{id}/lock", s.handleReleaseLock)
	s.mux.HandleFunc("GET /api/articles/{id}/revisions", s.handleListRevisions)
	s.mux.HandleFunc("POST /api/articles/{id}/revisions/{version}/restore", s.handleRestoreRevision)
	s.mux.HandleFunc("POST /api/media", s.handleUploadMedia)
	s.mux.HandleFunc("GET /media/{id}", s.handleServeMedia)
	s.mux.HandleFunc("GET /api/media/orphans", s.RequireAdmin(s.handleListOrphanMedia))
	s.mux.HandleFunc("POST /api/media/cleanup", s.RequireAdmin(s.handleCleanupOrphanMedia))
	s.mux.HandleFunc("GET /api/categories", s.handleListCategories)
	s.mux.HandleFunc("POST /api/categories", s.handleCreateCategory)
	s.mux.HandleFunc("PUT /api/categories/{id}", s.handleUpdateCategory)
	s.mux.HandleFunc("DELETE /api/categories/{id}", s.handleDeleteCategory)
	s.mux.HandleFunc("GET /api/synonyms", s.handleListSynonyms)
	s.mux.HandleFunc("POST /api/synonyms", s.handleCreateSynonym)
	s.mux.HandleFunc("PUT /api/synonyms/{id}", s.handleUpdateSynonym)
	s.mux.HandleFunc("DELETE /api/synonyms/{id}", s.handleDeleteSynonym)
	s.mux.HandleFunc("PUT /api/settings", s.RequireAdmin(s.handleUpdateSettings))
	s.mux.HandleFunc("DELETE /api/trash", s.RequireAdmin(s.handleEmptyTrash))

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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin prüft das optionale Admin-Passwort aus der Konfiguration.
// Ist kein Passwort konfiguriert, sind die Endpunkte im internen LAN direkt erreichbar.
func (s *Server) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminPassword == "" {
			next(w, r)
			return
		}
		provided := r.Header.Get("X-Admin-Password")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.AdminPassword)) != 1 {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"site_title":           siteTitle,
		"version":              s.version,
		"admin_protected":      s.cfg.AdminPassword != "",
		"review_reminder_days": reminderDays,
		"categories":           cats,
	})
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
	lock, hasLock := s.locks.Get(article.ID)

	resp := map[string]any{
		"article":       article,
		"symptoms_html": symptomsHTML,
		"solution_html": solutionHTML,
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
