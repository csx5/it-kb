package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/media"
	"github.com/csx5/it-kb/internal/model"
)

func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	// Maximales Request-Limit großzügig am Video-Limit + Multipart-Overhead ausrichten
	maxReq := (s.cfg.MaxVideoMB + 5) * 1024 * 1024
	if maxReq < 32<<20 {
		maxReq = 32 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReq)

	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Upload konnte nicht gelesen werden (Datei ggf. zu groß)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Feld 'file' fehlt im Upload")
		return
	}
	defer file.Close()

	uploadedBy := actorName(r, r.FormValue("uploaded_by"))

	saved, err := s.media.Save(file, header.Filename, uploadedBy)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrUnsupportedType):
			writeError(w, http.StatusBadRequest, "Dateityp ist nicht erlaubt")
		case errors.Is(err, media.ErrFileTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "Datei überschreitet das Größenlimit für diesen Medientyp")
		default:
			writeError(w, http.StatusInternalServerError, "Datei konnte nicht gespeichert werden")
		}
		return
	}

	if err := s.store.InsertMediaFile(r.Context(), saved); err != nil {
		if p, pErr := s.media.ResolvePath(saved.StoredPath); pErr == nil {
			_ = os.Remove(p)
		}
		writeError(w, http.StatusInternalServerError, "Medien-Metadaten konnten nicht gespeichert werden")
		return
	}

	url := "/media/" + saved.ID
	var mdSnippet string
	switch saved.Kind {
	case model.MediaKindImage:
		alt := strings.TrimSuffix(saved.OriginalName, filepath.Ext(saved.OriginalName))
		if alt == "" {
			alt = "Screenshot"
		}
		mdSnippet = fmt.Sprintf("![%s](%s)", escapeMarkdownLabel(alt), url)
	case model.MediaKindVideo:
		mdSnippet = fmt.Sprintf(`<video controls preload="metadata" src="%s"></video>`, url)
	default:
		mdSnippet = fmt.Sprintf("[Anhang: %s](%s)", escapeMarkdownLabel(saved.OriginalName), url)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"media":    saved,
		"url":      url,
		"markdown": mdSnippet,
	})
}

func (s *Server) handleServeMedia(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !isHexID(id) {
		http.NotFound(w, r)
		return
	}

	m, err := s.store.GetMediaFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Medienfehler", http.StatusInternalServerError)
		return
	}

	fullPath, err := s.media.ResolvePath(m.StoredPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	safeName := sanitizeDownloadName(m.OriginalName)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")

	if m.Kind == model.MediaKindAttachment {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
	} else {
		if m.MimeType != "" {
			w.Header().Set("Content-Type", m.MimeType)
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, safeName))
	}

	http.ServeFile(w, r, fullPath)
}

func (s *Server) handleListOrphanMedia(w http.ResponseWriter, r *http.Request) {
	orphans, err := s.store.ListOrphanedMediaFiles(r.Context(), s.orphanMinAge)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Verwaiste Dateien konnten nicht ermittelt werden")
		return
	}
	var totalBytes int64
	for _, o := range orphans {
		totalBytes += o.SizeBytes
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":       len(orphans),
		"total_bytes": totalBytes,
		"files":       orphans,
	})
}

func (s *Server) handleCleanupOrphanMedia(w http.ResponseWriter, r *http.Request) {
	orphans, err := s.store.ListOrphanedMediaFiles(r.Context(), s.orphanMinAge)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Verwaiste Dateien konnten nicht ermittelt werden")
		return
	}

	deleted := 0
	var freedBytes int64
	for _, o := range orphans {
		if p, err := s.media.ResolvePath(o.StoredPath); err == nil {
			_ = os.Remove(p)
		}
		if err := s.store.DeleteMediaFile(r.Context(), o.ID); err == nil {
			deleted++
			freedBytes += o.SizeBytes
		}
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "media_orphan_cleanup", "media", s.clientIP(r),
		fmt.Sprintf("%d Dateien entfernt (%d Bytes)", deleted, freedBytes))
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":     deleted,
		"freed_bytes": freedBytes,
	})
}

func isHexID(s string) bool {
	if len(s) < 16 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func sanitizeDownloadName(name string) string {
	base := filepath.Base(name)
	r := strings.NewReplacer(`"`, "_", `\`, "_", "\r", "", "\n", "")
	cleaned := strings.TrimSpace(r.Replace(base))
	if cleaned == "" || cleaned == "." {
		return "datei"
	}
	return cleaned
}

func escapeMarkdownLabel(s string) string {
	r := strings.NewReplacer("[", "(", "]", ")", "\r", "", "\n", " ")
	return strings.TrimSpace(r.Replace(s))
}
