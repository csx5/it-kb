package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/csx5/it-kb/internal/db"
)

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	revs, err := s.store.ListArticleRevisions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Versionshistorie konnte nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

func (s *Server) handleRestoreRevision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	ver, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || ver <= 0 {
		writeError(w, http.StatusBadRequest, "Ungültige Versionsnummer")
		return
	}

	var body struct {
		EditorName string `json:"editor_name"`
	}
	_ = decodeJSON(r, &body)

	restored, err := s.store.RestoreArticleRevision(r.Context(), id, ver, actorName(r, body.EditorName), actorUserID(r))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Revision oder Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.engine.UpsertArticle(restored)
	writeJSON(w, http.StatusOK, map[string]any{"article": restored})
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	cat, err := s.store.CreateCategory(r.Context(), body.Name, body.Description, body.SortOrder)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cat)
}

func (s *Server) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Kategorie-ID")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	cat, err := s.store.UpdateCategory(r.Context(), id, body.Name, body.Description, body.SortOrder)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Kategorie nicht gefunden")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Artikel im Suchindex haben CategoryName gecacht -> Index aktualisieren
	s.reloadSearchIndex(r.Context())
	writeJSON(w, http.StatusOK, cat)
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Kategorie-ID")
		return
	}
	if err := s.store.DeleteCategory(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Kategorie nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Kategorie konnte nicht gelöscht werden")
		return
	}
	s.reloadSearchIndex(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleCreateSynonym(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Terms []string `json:"terms"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	group, err := s.store.CreateSynonymGroup(r.Context(), body.Terms)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.syncSynonyms(r.Context())
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleUpdateSynonym(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Synonym-ID")
		return
	}
	var body struct {
		Terms []string `json:"terms"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	group, err := s.store.UpdateSynonymGroup(r.Context(), id, body.Terms)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Synonym-Gruppe nicht gefunden")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.syncSynonyms(r.Context())
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) handleDeleteSynonym(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Synonym-ID")
		return
	}
	if err := s.store.DeleteSynonymGroup(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Synonym-Gruppe nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Synonym-Gruppe konnte nicht gelöscht werden")
		return
	}
	s.syncSynonyms(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	ids, err := s.store.EmptyTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Papierkorb konnte nicht geleert werden")
		return
	}
	for _, id := range ids {
		s.engine.RemoveArticle(id)
	}
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "trash_emptied", "trash", s.clientIP(r), strconv.Itoa(len(ids))+" Artikel gelöscht")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(ids)})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SiteTitle          *string `json:"site_title"`
		ReviewReminderDays *int    `json:"review_reminder_days"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	if body.SiteTitle != nil {
		t := strings.TrimSpace(*body.SiteTitle)
		if t != "" {
			if err := s.store.SetSetting(r.Context(), "site_title", t); err != nil {
				writeError(w, http.StatusInternalServerError, "Seitentitel konnte nicht gespeichert werden")
				return
			}
		}
	}
	if body.ReviewReminderDays != nil {
		days := *body.ReviewReminderDays
		if days < 0 || days > 3650 {
			writeError(w, http.StatusBadRequest, "Prüf-Intervall muss zwischen 0 und 3650 Tagen liegen")
			return
		}
		if err := s.store.SetSetting(r.Context(), "review_reminder_days", strconv.Itoa(days)); err != nil {
			writeError(w, http.StatusInternalServerError, "Prüf-Intervall konnte nicht gespeichert werden")
			return
		}
		s.engine.SetReviewReminderDays(days)
	}
	s.handleMeta(w, r)
}

func (s *Server) syncSynonyms(ctx context.Context) {
	if groups, err := s.store.ListSynonymGroups(ctx); err == nil {
		s.engine.SetSynonyms(groups)
	}
}

func (s *Server) reloadSearchIndex(ctx context.Context) {
	articles, err1 := s.store.ListAllArticles(ctx)
	groups, err2 := s.store.ListSynonymGroups(ctx)
	if err1 == nil && err2 == nil {
		s.engine.Rebuild(articles, groups)
	}
}
