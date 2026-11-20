package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/csx5/it-kb/internal/db"
)

// CreateFullBackup erzeugt ein konsistentes ZIP-Archiv aus SQLite-Hot-Snapshot (VACUUM INTO)
// sowie dem kompletten Medienverzeichnis.
func CreateFullBackup(ctx context.Context, store *db.Store, mediaDir, outZipPath string) error {
	tmpDir, err := os.MkdirTemp("", "it-kb-backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	snapPath := filepath.Join(tmpDir, "kb.sqlite")
	if err := store.VacuumInto(ctx, snapPath); err != nil {
		return fmt.Errorf("sqlite snapshot: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outZipPath), 0o750); err != nil {
		return err
	}
	outFile, err := os.Create(outZipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	if err := addFileToZip(zw, snapPath, "kb.sqlite"); err != nil {
		return err
	}

	if _, err := os.Stat(mediaDir); err == nil {
		err = filepath.Walk(mediaDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return walkErr
			}
			rel, err := filepath.Rel(mediaDir, path)
			if err != nil {
				return err
			}
			return addFileToZip(zw, path, filepath.ToSlash(filepath.Join("media", rel)))
		})
		if err != nil {
			return fmt.Errorf("medien archivieren: %w", err)
		}
	}

	return zw.Close()
}

func addFileToZip(zw *zip.Writer, srcPath, archiveName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := zw.Create(strings.TrimPrefix(archiveName, "/"))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
