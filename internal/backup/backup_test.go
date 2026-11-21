package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/media"
	"github.com/csx5/it-kb/internal/model"
)

func TestPortableMarkdownExportAndFullBackupRestore(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "kb.sqlite")
	mediaDir := filepath.Join(tmp, "media")

	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(ctx, false); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Test-Screenshot ablegen
	ms := media.NewStorage(mediaDir, media.Limits{MaxImageBytes: 5 << 20, MaxVideoBytes: 10 << 20, MaxAttachmentBytes: 5 << 20})
	mf, err := ms.Save(strings.NewReader("fake-png-content"), "fehler.png", "MK")
	if err != nil {
		t.Fatalf("media save: %v", err)
	}
	if err := store.InsertMediaFile(ctx, mf); err != nil {
		t.Fatalf("media insert: %v", err)
	}

	// Artikel mit Medien-Link anlegen
	art, err := store.CreateArticle(ctx, db.CreateArticleInput{
		TemplateType: model.TemplateProblemSolution,
		Title:        "DNS Namensauflösung defekt",
		Status:       model.StatusActive,
		ErrorCodes:   "DNS_PROBE_FINISHED_NXDOMAIN",
		Symptoms:     "Screenshot:\n![Fehler](/media/" + mf.ID + ")",
		SolutionBody: "`ipconfig /flushdns` ausführen.",
		Tags:         []string{"dns", "netzwerk"},
		AuthorName:   "MK",
	})
	if err != nil {
		t.Fatalf("artikel erstellen: %v", err)
	}

	// 1. Portablen Markdown-Export testen
	var exportBuf bytes.Buffer
	if err := WritePortableMarkdownExport(ctx, store, mediaDir, &exportBuf); err != nil {
		t.Fatalf("markdown export: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(exportBuf.Bytes()), int64(exportBuf.Len()))
	if err != nil {
		t.Fatalf("export zip lesen: %v", err)
	}
	var foundMD, foundMedia, foundCats, foundSyns bool
	for _, f := range zr.File {
		switch {
		case strings.HasPrefix(f.Name, "articles/") && strings.HasSuffix(f.Name, ".md"):
			foundMD = true
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			_ = rc.Close()
			text := string(raw)
			if !strings.Contains(text, "DNS_PROBE_FINISHED_NXDOMAIN") || !strings.Contains(text, "../media/") {
				t.Fatalf("markdown-inhalt unerwartet: %s", text)
			}
		case strings.HasPrefix(f.Name, "media/"):
			foundMedia = true
		case f.Name == "categories.json":
			foundCats = true
		case f.Name == "synonyms.json":
			foundSyns = true
		}
	}
	if !foundMD || !foundMedia || !foundCats || !foundSyns {
		t.Fatalf("export unvollständig: md=%v media=%v cats=%v syns=%v", foundMD, foundMedia, foundCats, foundSyns)
	}

	// 2. Voll-Backup erstellen
	backupPath := filepath.Join(tmp, "full-backup.zip")
	if err := CreateFullBackup(ctx, store, mediaDir, backupPath); err != nil {
		t.Fatalf("full backup: %v", err)
	}

	// 3. Artikel und Medien löschen, danach Voll-Restore ausführen
	if err := store.PermanentDeleteArticle(ctx, art.ID); err != nil {
		t.Fatalf("delete article: %v", err)
	}
	_ = os.RemoveAll(mediaDir)

	bf, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("backup öffnen: %v", err)
	}
	defer bf.Close()
	stat, _ := bf.Stat()

	if err := RestoreFullBackup(ctx, bf, stat.Size(), store, mediaDir); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// 4. Prüfen, ob Artikel & Mediendatei wieder vorhanden sind
	restoredArt, err := store.GetArticleByID(ctx, art.ID)
	if err != nil || restoredArt.Title != art.Title {
		t.Fatalf("artikel nach restore fehlt: %v %+v", err, restoredArt)
	}
	restoredMediaPath := filepath.Join(mediaDir, filepath.FromSlash(mf.StoredPath))
	if _, err := os.Stat(restoredMediaPath); err != nil {
		t.Fatalf("mediendatei nach restore fehlt: %v", err)
	}
}

func TestBackupEncryptionRoundtrip(t *testing.T) {
	plain := []byte("geheimes-backup-zip-payload")
	enc, err := EncryptBytes(plain, "StarkeBackupPassphrase!")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncryptedBackup(enc) {
		t.Fatalf("verschlüsseltes backup nicht erkannt")
	}

	if _, err := DecryptBytes(enc, "FalschePassphrase!"); err == nil {
		t.Fatalf("falsche passphrase hätte abgelehnt werden müssen")
	}

	dec, err := DecryptBytes(enc, "StarkeBackupPassphrase!")
	if err != nil || !bytes.Equal(dec, plain) {
		t.Fatalf("decrypt fehlgeschlagen: err=%v dec=%q", err, string(dec))
	}
}
