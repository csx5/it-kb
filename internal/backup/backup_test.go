package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

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

	if err := store.SeedInitialData(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Test-Screenshot mit gültigem PNG-Magic-Header ablegen
	ms := media.NewStorage(mediaDir, media.Limits{MaxImageBytes: 5 << 20, MaxVideoBytes: 10 << 20, MaxAttachmentBytes: 5 << 20})
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	mf, err := ms.Save(bytes.NewReader(validPNG), "fehler.png", "MK")
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

func TestBackupStreamEncryptionMultiChunkAndLegacyV1(t *testing.T) {
	// Über 180 KiB (mehrere 64-KiB-Chunks) testen
	payload := bytes.Repeat([]byte("IT-KB-CHUNK-PAYLOAD-0123456789ABCDEF"), 5500)
	var encBuf bytes.Buffer
	if err := EncryptStream(bytes.NewReader(payload), &encBuf, "LangGenugPassphrase!2026"); err != nil {
		t.Fatalf("encrypt stream: %v", err)
	}
	if !bytes.HasPrefix(encBuf.Bytes(), encryptedBackupV2Magic) {
		t.Fatalf("erwartete ITKBENC2-Header")
	}

	var decBuf bytes.Buffer
	if err := DecryptStream(bytes.NewReader(encBuf.Bytes()), &decBuf, "LangGenugPassphrase!2026"); err != nil {
		t.Fatalf("decrypt stream: %v", err)
	}
	if !bytes.Equal(decBuf.Bytes(), payload) {
		t.Fatalf("stream roundtrip daten weichen ab (got=%d want=%d)", decBuf.Len(), len(payload))
	}

	// Abgeschnittene Datei muss abgelehnt werden
	truncated := encBuf.Bytes()[:len(encBuf.Bytes())-10]
	if err := DecryptStream(bytes.NewReader(truncated), &bytes.Buffer{}, "LangGenugPassphrase!2026"); err == nil {
		t.Fatalf("abgeschnittener stream hätte abgelehnt werden müssen")
	}

	// Leerer Input als gültiger einzelner Final-Chunk
	var emptyEnc, emptyDec bytes.Buffer
	if err := EncryptStream(bytes.NewReader(nil), &emptyEnc, "LangGenugPassphrase!2026"); err != nil {
		t.Fatalf("empty encrypt stream: %v", err)
	}
	if err := DecryptStream(bytes.NewReader(emptyEnc.Bytes()), &emptyDec, "LangGenugPassphrase!2026"); err != nil || emptyDec.Len() != 0 {
		t.Fatalf("empty decrypt stream fehlgeschlagen: err=%v len=%d", err, emptyDec.Len())
	}

	// Abwärtskompatibilität zu Legacy ITKBENC1
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte("LegacyPassphrase!"), salt, 3, 64*1024, 2, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	v1Ct := gcm.Seal(nil, nonce, []byte("legacy-v1-inhalt"), encryptedBackupMagic)
	v1Blob := append(append(append([]byte{}, encryptedBackupMagic...), salt...), append(nonce, v1Ct...)...)

	var v1Dec bytes.Buffer
	if err := DecryptStream(bytes.NewReader(v1Blob), &v1Dec, "LegacyPassphrase!"); err != nil || v1Dec.String() != "legacy-v1-inhalt" {
		t.Fatalf("legacy v1 stream decrypt fehlgeschlagen: err=%v out=%q", err, v1Dec.String())
	}
}

func TestScheduledBackupRotation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := db.Open(filepath.Join(tmp, "kb.sqlite"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer store.Close()
	_ = store.SeedInitialData(ctx)

	mediaDir := filepath.Join(tmp, "media")
	outDir := filepath.Join(tmp, "auto-backups")

	var paths []string
	for i := 0; i < 4; i++ {
		p, err := RunScheduledBackup(ctx, store, mediaDir, outDir, "AutoBackupSecret2026", 2)
		if err != nil {
			t.Fatalf("scheduled backup %d: %v", i, err)
		}
		paths = append(paths, p)
		time.Sleep(5 * time.Millisecond)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("erwartete exakt 2 rotierte backups, gefunden: %d (%v)", len(entries), entries)
	}
	// Neuestes Backup muss entschlüsselbar sein
	f, err := os.Open(paths[len(paths)-1])
	if err != nil {
		t.Fatalf("open newest backup: %v", err)
	}
	defer f.Close()
	var zipOut bytes.Buffer
	if err := DecryptStream(f, &zipOut, "AutoBackupSecret2026"); err != nil || zipOut.Len() == 0 {
		t.Fatalf("rotiertes auto-backup nicht entschlüsselbar: %v", err)
	}
}

func TestMarkdownExportImportRoundtrip(t *testing.T) {
	ctx := context.Background()
	srcTmp := t.TempDir()
	srcStore, err := db.Open(filepath.Join(srcTmp, "src.sqlite"))
	if err != nil {
		t.Fatalf("src db: %v", err)
	}
	defer srcStore.Close()
	_ = srcStore.SeedInitialData(ctx)

	srcMediaDir := filepath.Join(srcTmp, "media")
	srcMS := media.NewStorage(srcMediaDir, media.Limits{MaxImageBytes: 5 << 20, MaxVideoBytes: 10 << 20, MaxAttachmentBytes: 5 << 20})
	validPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	mf, err := srcMS.Save(bytes.NewReader(validPNG), "rdp.png", "MK")
	if err != nil {
		t.Fatalf("save media: %v", err)
	}
	_ = srcStore.InsertMediaFile(ctx, mf)

	cat, err := srcStore.CreateCategory(ctx, "Terminalserver", "RDP Farm", 10)
	if err != nil {
		t.Fatalf("cat: %v", err)
	}
	_, _ = srcStore.CreateSynonymGroup(ctx, []string{"rdp", "remotedesktop", "mstsc"})
	_, err = srcStore.CreateArticle(ctx, db.CreateArticleInput{
		TemplateType: model.TemplateProblemSolution,
		Title:        "RDP CredSSP Fehler",
		CategoryID:   &cat.ID,
		Status:       model.StatusActive,
		ErrorCodes:   "0x80090322",
		Symptoms:     "Anmeldung schlägt fehl:\n![RDP](/media/" + mf.ID + ")",
		SolutionBody: "Client aktualisieren.",
		Tags:         []string{"rdp", "windows"},
		AuthorName:   "MK",
	})
	if err != nil {
		t.Fatalf("create article: %v", err)
	}

	var exportZip bytes.Buffer
	if err := WritePortableMarkdownExport(ctx, srcStore, srcMediaDir, &exportZip); err != nil {
		t.Fatalf("export: %v", err)
	}

	// In frische Ziel-Datenbank importieren
	dstTmp := t.TempDir()
	dstStore, err := db.Open(filepath.Join(dstTmp, "dst.sqlite"))
	if err != nil {
		t.Fatalf("dst db: %v", err)
	}
	defer dstStore.Close()
	dstMS := media.NewStorage(filepath.Join(dstTmp, "media"), media.Limits{MaxImageBytes: 5 << 20, MaxVideoBytes: 10 << 20, MaxAttachmentBytes: 5 << 20})

	impRes, err := ImportMarkdownZIP(ctx, bytes.NewReader(exportZip.Bytes()), int64(exportZip.Len()), dstStore, dstMS, "Importer", nil)
	if err != nil {
		t.Fatalf("zip import: %v", err)
	}
	if impRes.ArticlesImported != 1 || impRes.MediaImported != 1 || impRes.CategoriesCreated < 1 || impRes.SynonymsCreated < 1 {
		t.Fatalf("unerwartetes import-ergebnis: %+v", impRes)
	}

	importedArts, _ := dstStore.ListAllArticles(ctx)
	if len(importedArts) != 1 || importedArts[0].Title != "RDP CredSSP Fehler" || importedArts[0].ErrorCodes != "0x80090322" {
		t.Fatalf("importierter artikel fehlerhaft: %+v", importedArts)
	}
	if !strings.Contains(importedArts[0].Symptoms, "/media/") || strings.Contains(importedArts[0].Symptoms, "../media/") {
		t.Fatalf("medienlink wurde nicht umgeschrieben: %s", importedArts[0].Symptoms)
	}

	// Einzelne .md-Datei ohne Frontmatter testen
	single, err := ImportMarkdownFile(ctx, dstStore, "wlan-reset.md", []byte("# WLAN Reset\n\n`netsh wlan show profiles` ausführen."), "Admin", nil)
	if err != nil || single.Title != "WLAN Reset" {
		t.Fatalf("einzelner md-import fehlgeschlagen: err=%v art=%+v", err, single)
	}
}
