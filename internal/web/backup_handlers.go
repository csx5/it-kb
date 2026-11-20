package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/csx5/it-kb/internal/backup"
)

func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := backup.WritePortableMarkdownExport(r.Context(), s.store, s.cfg.MediaDir(), &buf); err != nil {
		writeError(w, http.StatusInternalServerError, "Markdown-Export fehlgeschlagen: "+err.Error())
		return
	}

	filename := fmt.Sprintf("it-kb-markdown-export-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleFullBackupDownload(w http.ResponseWriter, r *http.Request) {
	tmpFile, err := os.CreateTemp("", "it-kb-dl-backup-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Temporäre Backup-Datei fehlgeschlagen")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := backup.WriteFullBackup(r.Context(), s.store, s.cfg.MediaDir(), tmpFile); err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusInternalServerError, "Voll-Backup fehlgeschlagen: "+err.Error())
		return
	}
	_ = tmpFile.Close()

	filename := fmt.Sprintf("it-kb-vollbackup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, tmpPath)
}

func (s *Server) handleFullBackupRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20) // bis 512 MB Upload
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Backup-Datei konnte nicht gelesen werden")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Feld 'file' fehlt im Restore-Upload")
		return
	}
	defer file.Close()

	tmpFile, err := os.CreateTemp("", "it-kb-upload-restore-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Temporäre Datei fehlgeschlagen")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	size, err := io.Copy(tmpFile, file)
	if err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusBadRequest, "Backup-Upload unvollständig")
		return
	}

	if err := backup.RestoreFullBackup(r.Context(), tmpFile, size, s.store, s.cfg.MediaDir()); err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusBadRequest, "Restore fehlgeschlagen: "+err.Error())
		return
	}
	_ = tmpFile.Close()

	// Suchindex nach Restore sofort neu aufbauen
	s.reloadSearchIndex(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "restored",
	})
}
