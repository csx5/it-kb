package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

	passphrase := strings.TrimSpace(r.Header.Get("X-Backup-Passphrase"))
	if passphrase != "" {
		plain, err := os.ReadFile(tmpPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Backup-Datei konnte nicht gelesen werden")
			return
		}
		enc, err := backup.EncryptBytes(plain, passphrase)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		filename := fmt.Sprintf("it-kb-vollbackup-%s.itkb.enc", time.Now().Format("20060102-150405"))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_download_encrypted", "full_backup", clientIP(r), filename)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(enc)
		return
	}

	filename := fmt.Sprintf("it-kb-vollbackup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_download", "full_backup", clientIP(r), filename)
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

	raw, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Backup-Upload unvollständig")
		return
	}

	// Automatische Erkennung und Entschlüsselung bei AES-256-GCM Backups (.itkb.enc)
	if backup.IsEncryptedBackup(raw) {
		passphrase := strings.TrimSpace(r.FormValue("passphrase"))
		if passphrase == "" {
			passphrase = strings.TrimSpace(r.Header.Get("X-Backup-Passphrase"))
		}
		dec, err := backup.DecryptBytes(raw, passphrase)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, backup.ErrPassphraseNeeded) {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, status, err.Error())
			return
		}
		raw = dec
	}

	if err := backup.RestoreFullBackup(r.Context(), bytes.NewReader(raw), int64(len(raw)), s.store, s.cfg.MediaDir()); err != nil {
		writeError(w, http.StatusBadRequest, "Restore fehlgeschlagen: "+err.Error())
		return
	}

	// Suchindex nach Restore sofort neu aufbauen
	s.reloadSearchIndex(r.Context())
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_restore", "full_backup", clientIP(r), "Voll-Backup eingespielt")
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "restored",
	})
}
