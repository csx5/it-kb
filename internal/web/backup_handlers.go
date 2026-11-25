package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/csx5/it-kb/internal/backup"
	"github.com/csx5/it-kb/internal/model"
)

func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := backup.WritePortableMarkdownExport(r.Context(), s.store, s.cfg.MediaDir(), &buf); err != nil {
		writeError(w, http.StatusInternalServerError, "Markdown-Export fehlgeschlagen")
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
		writeError(w, http.StatusInternalServerError, "Voll-Backup fehlgeschlagen")
		return
	}
	_ = tmpFile.Close()

	passphrase := r.Header.Get("X-Backup-Passphrase")
	if passphrase != "" {
		if len(passphrase) < 8 {
			writeError(w, http.StatusBadRequest, "backup-passphrase muss mindestens 8 zeichen lang sein")
			return
		}
		srcFile, err := os.Open(tmpPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Backup-Datei konnte nicht geöffnet werden")
			return
		}
		defer srcFile.Close()

		tmpEnc, err := os.CreateTemp("", "it-kb-dl-enc-*.itkb.enc")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Temporäre verschlüsselte Backup-Datei fehlgeschlagen")
			return
		}
		tmpEncPath := tmpEnc.Name()
		defer os.Remove(tmpEncPath)
		_ = os.Chmod(tmpEncPath, 0o600)

		if err := backup.EncryptStream(srcFile, tmpEnc, passphrase); err != nil {
			_ = tmpEnc.Close()
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = tmpEnc.Close()

		filename := fmt.Sprintf("it-kb-vollbackup-%s.itkb.enc", time.Now().Format("20060102-150405"))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_download_encrypted", "full_backup", s.clientIP(r), filename)
		http.ServeFile(w, r, tmpEncPath)
		return
	}

	filename := fmt.Sprintf("it-kb-vollbackup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_download", "full_backup", s.clientIP(r), filename)
	http.ServeFile(w, r, tmpPath)
}

// handleFullBackupRestoreEntry prüft Auth sicher unter restoreMu.RLock (schützt gegen Nil-DB-Race
// bei parallelen Restore-Requests) und wechselt erst für den eigentlichen Restore auf restoreMu.Lock.
func (s *Server) handleFullBackupRestoreEntry(w http.ResponseWriter, r *http.Request) {
	var authedReq *http.Request
	s.restoreMu.RLock()
	s.requireAuth(model.RoleAdmin, func(_ http.ResponseWriter, req *http.Request) {
		authedReq = req
	})(w, r)
	s.restoreMu.RUnlock()

	if authedReq == nil {
		return
	}
	s.handleFullBackupRestore(w, authedReq)
}

func (s *Server) handleFullBackupRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20) // bis 512 MB Upload
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Backup-Datei konnte nicht gelesen werden")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Feld 'file' fehlt im Restore-Upload")
		return
	}
	defer file.Close()

	// Zuerst in temporäre Datei (0600) streamen statt unnötiger Vollpufferung im RAM
	tmpUpload, err := os.CreateTemp("", "it-kb-restore-in-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Temporäre Datei fehlgeschlagen")
		return
	}
	tmpUploadPath := tmpUpload.Name()
	defer os.Remove(tmpUploadPath)
	_ = os.Chmod(tmpUploadPath, 0o600)

	size, err := io.Copy(tmpUpload, file)
	if err != nil {
		_ = tmpUpload.Close()
		writeError(w, http.StatusBadRequest, "Backup-Upload unvollständig")
		return
	}

	var magic [8]byte
	_, _ = tmpUpload.ReadAt(magic[:], 0)

	var readerAt io.ReaderAt = tmpUpload
	var zipSize = size
	var tmpDecPath string
	var tmpDec *os.File

	if backup.IsEncryptedBackup(magic[:]) {
		if _, err := tmpUpload.Seek(0, io.SeekStart); err != nil {
			_ = tmpUpload.Close()
			writeError(w, http.StatusInternalServerError, "Backup-Datei konnte nicht zurückgesetzt werden")
			return
		}
		passphrase := r.FormValue("passphrase")
		if passphrase == "" {
			passphrase = r.Header.Get("X-Backup-Passphrase")
		}
		tmpDec, err = os.CreateTemp("", "it-kb-restore-dec-*.zip")
		if err != nil {
			_ = tmpUpload.Close()
			writeError(w, http.StatusInternalServerError, "Temporäre Datei fehlgeschlagen")
			return
		}
		tmpDecPath = tmpDec.Name()
		defer os.Remove(tmpDecPath)
		_ = os.Chmod(tmpDecPath, 0o600)

		decErr := backup.DecryptStream(tmpUpload, tmpDec, passphrase)
		_ = tmpUpload.Close()
		if decErr != nil {
			_ = tmpDec.Close()
			status := http.StatusBadRequest
			if errors.Is(decErr, backup.ErrPassphraseNeeded) {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, status, decErr.Error())
			return
		}
		stat, err := tmpDec.Stat()
		if err != nil {
			_ = tmpDec.Close()
			writeError(w, http.StatusInternalServerError, "Entschlüsselte Datei konnte nicht geprüft werden")
			return
		}
		if _, err := tmpDec.Seek(0, io.SeekStart); err != nil {
			_ = tmpDec.Close()
			writeError(w, http.StatusInternalServerError, "Entschlüsselte Datei konnte nicht gelesen werden")
			return
		}
		readerAt = tmpDec
		zipSize = stat.Size()
		defer tmpDec.Close()
	} else {
		defer tmpUpload.Close()
	}

	s.restoreMu.Lock()
	err = backup.RestoreFullBackup(r.Context(), readerAt, zipSize, s.store, s.cfg.MediaDir())
	if err == nil {
		s.reloadSearchIndex(r.Context())
	}
	s.restoreMu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Restore fehlgeschlagen: "+err.Error())
		return
	}

	s.store.WriteAuditLog(r.Context(), actorUserID(r), actorName(r, "Admin"), "backup_restore", "full_backup", s.clientIP(r), "Voll-Backup eingespielt")
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "restored",
	})
}
