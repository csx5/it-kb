package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/csx5/it-kb/internal/backup"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/model"
)

// --- Markdown & ZIP Import ---

func (s *Server) handleImportMarkdown(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Import-Datei konnte nicht gelesen werden")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Feld 'file' fehlt im Import-Upload")
		return
	}
	defer file.Close()

	filename := "import.md"
	if header != nil && strings.TrimSpace(header.Filename) != "" {
		filename = filepath.Base(header.Filename)
	}
	ext := strings.ToLower(filepath.Ext(filename))

	var defaultAuthor string
	if u := currentUser(r); u != nil {
		defaultAuthor = actorName(r, "")
	}

	// Upload in temporäre Datei schreiben, um ZIP-Magic sicher zu prüfen und RAM-Verbrauch gering zu halten
	tmpFile, err := os.CreateTemp("", "it-kb-import-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Temporäre Import-Datei fehlgeschlagen")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	_ = os.Chmod(tmpPath, 0o600)

	size, err := io.Copy(tmpFile, file)
	if err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusBadRequest, "Import-Upload unvollständig")
		return
	}

	var magic [4]byte
	_, _ = tmpFile.ReadAt(magic[:], 0)
	isZIP := ext == ".zip" || bytes.Equal(magic[:], []byte{'P', 'K', 0x03, 0x04})

	if isZIP {
		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			_ = tmpFile.Close()
			writeError(w, http.StatusInternalServerError, "Import-Archiv konnte nicht gelesen werden")
			return
		}
		res, err := backup.ImportMarkdownZIP(r.Context(), tmpFile, size, s.store, s.media, defaultAuthor, actorUserID(r))
		_ = tmpFile.Close()
		if err != nil {
			writeError(w, http.StatusBadRequest, "Markdown-ZIP-Import fehlgeschlagen: "+err.Error())
			return
		}
		s.reloadSearchIndex(r.Context())
		s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Import"), "markdown_import_zip", filename, s.clientIP(r),
			fmt.Sprintf("%d Artikel, %d Medien", res.ArticlesImported, res.MediaImported))
		writeJSON(w, http.StatusOK, res)
		return
	}
	_ = tmpFile.Close()

	if ext != ".md" && ext != ".markdown" && ext != ".txt" && ext != "" {
		writeError(w, http.StatusBadRequest, "Nur .md-Dateien oder .zip-Exportarchive werden unterstützt")
		return
	}
	if size > 16<<20 {
		writeError(w, http.StatusBadRequest, "Markdown-Datei ist zu groß (max. 16 MB)")
		return
	}
	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Markdown-Datei konnte nicht gelesen werden")
		return
	}
	art, err := backup.ImportMarkdownFile(r.Context(), s.store, filename, raw, defaultAuthor, actorUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Markdown-Import fehlgeschlagen: "+err.Error())
		return
	}
	s.reloadSearchIndex(r.Context())
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Import"), "markdown_import_file", filename, s.clientIP(r), art.Title)
	writeJSON(w, http.StatusCreated, map[string]any{
		"articles_imported": 1,
		"article":           art,
	})
}

// --- Eigene Artikel-Vorlagen / Textbausteine ---

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListArticleTemplates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Vorlagen konnten nicht geladen werden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": list})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var in model.ArticleTemplate
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	created, err := s.store.CreateArticleTemplate(r.Context(), in)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "bereits") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Editor"), "template_created", created.Name, s.clientIP(r), created.TemplateType)
	writeJSON(w, http.StatusCreated, map[string]any{"template": created})
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Vorlagen-ID")
		return
	}
	var in model.ArticleTemplate
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige JSON-Daten")
		return
	}
	updated, err := s.store.UpdateArticleTemplate(r.Context(), id, in)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Vorlage nicht gefunden")
			return
		}
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "bereits") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Editor"), "template_updated", updated.Name, s.clientIP(r), updated.TemplateType)
	writeJSON(w, http.StatusOK, map[string]any{"template": updated})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Vorlagen-ID")
		return
	}
	if err := s.store.DeleteArticleTemplate(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Vorlage nicht gefunden")
			return
		}
		writeError(w, http.StatusInternalServerError, "Vorlage konnte nicht gelöscht werden")
		return
	}
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Editor"), "template_deleted", strconv.FormatInt(id, 10), s.clientIP(r), "Vorlage gelöscht")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
