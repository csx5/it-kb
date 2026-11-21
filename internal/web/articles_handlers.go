package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/csx5/it-kb/internal/db"
)

type articlePayload struct {
	TemplateType    string   `json:"template_type"`
	Title           string   `json:"title"`
	CategoryID      *int64   `json:"category_id"`
	Status          string   `json:"status"`
	ErrorCodes      string   `json:"error_codes"`
	Symptoms        string   `json:"symptoms"`
	SolutionBody    string   `json:"solution_body"`
	Tags            []string `json:"tags"`
	AuthorName      string   `json:"author_name"`
	EditorName      string   `json:"editor_name"`
	ChangeSummary   string   `json:"change_summary"`
	ExpectedVersion int      `json:"expected_version"`
	Force           bool     `json:"force"`
}

func (s *Server) handleCreateArticle(w http.ResponseWriter, r *http.Request) {
	var p articlePayload
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	author := actorName(r, p.AuthorName)
	if strings.TrimSpace(p.AuthorName) == "" && currentUser(r) == nil {
		author = actorName(r, p.EditorName)
	}

	created, err := s.store.CreateArticle(r.Context(), db.CreateArticleInput{
		TemplateType: p.TemplateType,
		Title:        p.Title,
		CategoryID:   p.CategoryID,
		Status:       p.Status,
		ErrorCodes:   p.ErrorCodes,
		Symptoms:     p.Symptoms,
		SolutionBody: p.SolutionBody,
		Tags:         p.Tags,
		AuthorName:   author,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.engine.UpsertArticle(created)
	writeJSON(w, http.StatusCreated, map[string]any{"article": created})
}

func (s *Server) handleUpdateArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}

	var p articlePayload
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}

	editor := actorName(r, p.EditorName)
	if strings.TrimSpace(p.EditorName) == "" && currentUser(r) == nil {
		editor = actorName(r, p.AuthorName)
	}

	updated, err := s.store.UpdateArticle(r.Context(), id, db.UpdateArticleInput{
		ExpectedVersion: p.ExpectedVersion,
		Force:           p.Force,
		TemplateType:    p.TemplateType,
		Title:           p.Title,
		CategoryID:      p.CategoryID,
		Status:          p.Status,
		ErrorCodes:      p.ErrorCodes,
		Symptoms:        p.Symptoms,
		SolutionBody:    p.SolutionBody,
		Tags:            p.Tags,
		EditorName:      editor,
		ChangeSummary:   p.ChangeSummary,
	})
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
		case errors.Is(err, db.ErrVersionConflict):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":          "Der Artikel wurde zwischenzeitlich von jemand anderem geändert.",
				"server_article": updated,
			})
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	s.engine.UpsertArticle(updated)
	if editor != "" {
		s.locks.Release(id, editor)
	}
	writeJSON(w, http.StatusOK, map[string]any{"article": updated})
}

func (s *Server) handleVerifyArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	var body struct {
		Verifier string `json:"verifier"`
	}
	_ = decodeJSON(r, &body)

	updated, err := s.store.VerifyArticle(r.Context(), id, actorName(r, body.Verifier))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Prüfstatus konnte nicht aktualisiert werden")
		return
	}

	s.engine.UpsertArticle(updated)
	writeJSON(w, http.StatusOK, map[string]any{"article": updated})
}

func (s *Server) handleSoftDeleteArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}

	deleted, err := s.store.SoftDeleteArticle(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Artikel konnte nicht in den Papierkorb verschoben werden")
		return
	}

	s.engine.UpsertArticle(deleted)
	writeJSON(w, http.StatusOK, map[string]any{"article": deleted})
}

func (s *Server) handleRestoreArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}

	restored, err := s.store.RestoreArticle(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht im Papierkorb gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Artikel konnte nicht wiederhergestellt werden")
		return
	}

	s.engine.UpsertArticle(restored)
	writeJSON(w, http.StatusOK, map[string]any{"article": restored})
}

func (s *Server) handlePermanentDeleteArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}

	if err := s.store.PermanentDeleteArticle(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Artikel nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Artikel konnte nicht endgültig gelöscht werden")
		return
	}

	s.engine.RemoveArticle(id)
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "article_permanent_delete", strconv.FormatInt(id, 10), clientIP(r), "Artikel endgültig gelöscht")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleHeartbeatLock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	var body struct {
		Editor string `json:"editor"`
	}
	_ = decodeJSON(r, &body)
	editor := actorName(r, body.Editor)
	if editor == "" || editor == "Unbekannt" {
		writeError(w, http.StatusBadRequest, "Editor-Name erforderlich")
		return
	}
	lock := s.locks.Heartbeat(id, editor)
	writeJSON(w, http.StatusOK, map[string]any{
		"lock":     lock,
		"is_other": lock.Editor != editor,
	})
}

func (s *Server) handleReleaseLock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Artikel-ID")
		return
	}
	editor := strings.TrimSpace(r.URL.Query().Get("editor"))
	if editor != "" {
		s.locks.Release(id, editor)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (s *Server) handlePreviewMarkdown(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Symptoms     string `json:"symptoms"`
		SolutionBody string `json:"solution_body"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	symptomsHTML, err := s.md.RenderHTML(body.Symptoms)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Vorschau fehlgeschlagen")
		return
	}
	solutionHTML, err := s.md.RenderHTML(body.SolutionBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Vorschau fehlgeschlagen")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"symptoms_html": symptomsHTML,
		"solution_html": solutionHTML,
	})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 2<<20))
	return dec.Decode(dst)
}
