package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/model"
)

func (s *Server) handleHelpfulArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	updated, err := s.store.IncrementArticleHelpful(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Zähler konnte nicht erhöht werden")
		return
	}
	s.engine.UpsertArticle(updated)
	writeJSON(w, http.StatusOK, map[string]any{"article": updated})
}

func (s *Server) handlePinArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	updated, err := s.store.SetArticlePinned(r.Context(), id, body.Pinned)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Pin-Status konnte nicht aktualisiert werden")
		return
	}
	s.engine.UpsertArticle(updated)
	writeJSON(w, http.StatusOK, map[string]any{"article": updated})
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	var body struct {
		Body       string `json:"body"`
		AuthorName string `json:"author_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	comment, err := s.store.CreateArticleComment(r.Context(), id, actorName(r, body.AuthorName), actorUserID(r), body.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": comment})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	commentID, err := strconv.ParseInt(r.PathValue("commentId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Kommentar-ID")
		return
	}
	existing, err := s.store.GetArticleComment(r.Context(), commentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Kommentar nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Kommentar konnte nicht geladen werden")
		return
	}

	// Im Auth-Modus darf nur der eigene Autor oder ein Administrator löschen
	if u := currentUser(r); u != nil {
		isOwner := existing.AuthorUserID != nil && *existing.AuthorUserID == u.ID
		if !isOwner && u.Role != model.RoleAdmin {
			writeError(w, http.StatusForbidden, "Nur der Autor oder ein Administrator darf diesen Kommentar löschen")
			return
		}
	}

	if err := s.store.DeleteArticleComment(r.Context(), commentID); err != nil {
		writeError(w, http.StatusInternalServerError, "Kommentar konnte nicht gelöscht werden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
