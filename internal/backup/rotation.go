package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/db"
)

const autoBackupPrefix = "it-kb-auto-"

// RunScheduledBackup erstellt ein automatisches Voll-Backup (optional AES-256-GCM-verschlüsselt)
// im Zielverzeichnis outDir und löscht überzählige ältere Auto-Backups bis maximal keep Dateien.
func RunScheduledBackup(ctx context.Context, store *db.Store, mediaDir, outDir, passphrase string, keep int) (string, error) {
	if strings.TrimSpace(outDir) == "" {
		return "", errors.New("zielverzeichnis für automatisches backup fehlt")
	}
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return "", fmt.Errorf("backup-verzeichnis anlegen: %w", err)
	}

	ext := ".zip"
	if passphrase != "" {
		ext = ".itkb.enc"
	}
	stamp := time.Now().UTC().Format("20060102-150405.000")
	finalPath := filepath.Join(outDir, autoBackupPrefix+stamp+ext)

	tmpZip, err := os.CreateTemp(outDir, ".tmp-backup-*.zip")
	if err != nil {
		return "", fmt.Errorf("temporäre backup-datei anlegen: %w", err)
	}
	tmpZipPath := tmpZip.Name()
	_ = os.Chmod(tmpZipPath, 0o600)

	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpZipPath)
		}
	}()

	if err := WriteFullBackup(ctx, store, mediaDir, tmpZip); err != nil {
		_ = tmpZip.Close()
		return "", err
	}
	if err := tmpZip.Close(); err != nil {
		return "", err
	}

	if passphrase == "" {
		if err := os.Rename(tmpZipPath, finalPath); err != nil {
			return "", err
		}
		cleanupTemp = false
	} else {
		src, err := os.Open(tmpZipPath)
		if err != nil {
			return "", err
		}
		tmpEnc, err := os.CreateTemp(outDir, ".tmp-backup-*.enc")
		if err != nil {
			_ = src.Close()
			return "", err
		}
		tmpEncPath := tmpEnc.Name()
		_ = os.Chmod(tmpEncPath, 0o600)

		encErr := EncryptStream(src, tmpEnc, passphrase)
		_ = src.Close()
		closeErr := tmpEnc.Close()
		if encErr != nil {
			_ = os.Remove(tmpEncPath)
			return "", encErr
		}
		if closeErr != nil {
			_ = os.Remove(tmpEncPath)
			return "", closeErr
		}
		if err := os.Rename(tmpEncPath, finalPath); err != nil {
			_ = os.Remove(tmpEncPath)
			return "", err
		}
	}

	if err := pruneOldAutoBackups(outDir, keep); err != nil {
		return finalPath, err
	}
	return finalPath, nil
}

func pruneOldAutoBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, autoBackupPrefix) {
			continue
		}
		if strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".itkb.enc") {
			backups = append(backups, name)
		}
	}
	if len(backups) <= keep {
		return nil
	}
	// Zeitstempel im Dateinamen sortiert lexikografisch absteigend = neueste zuerst
	sort.Slice(backups, func(i, j int) bool {
		return backups[i] > backups[j]
	})
	for _, oldName := range backups[keep:] {
		_ = os.Remove(filepath.Join(dir, oldName))
	}
	return nil
}
