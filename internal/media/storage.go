package media

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

var (
	ErrUnsupportedType = errors.New("dateityp ist nicht in der whitelist erlaubt")
	ErrFileTooLarge    = errors.New("datei überschreitet das erlaubte größenlimit")
)

type Limits struct {
	MaxImageBytes      int64
	MaxVideoBytes      int64
	MaxAttachmentBytes int64
}

type Storage struct {
	baseDir string
	limits  Limits
}

var allowedImages = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
}

var allowedVideos = map[string]bool{
	".mp4": true, ".webm": true,
}

var allowedAttachments = map[string]bool{
	".ps1": true, ".bat": true, ".cmd": true, ".reg": true, ".sh": true,
	".pdf": true, ".txt": true, ".log": true, ".json": true, ".xml": true,
	".csv": true, ".zip": true,
}

func NewStorage(baseDir string, limits Limits) *Storage {
	return &Storage{baseDir: baseDir, limits: limits}
}

func ClassifyExtension(filename string) (kind string, ext string, ok bool) {
	ext = strings.ToLower(filepath.Ext(filename))
	switch {
	case allowedImages[ext]:
		return model.MediaKindImage, ext, true
	case allowedVideos[ext]:
		return model.MediaKindVideo, ext, true
	case allowedAttachments[ext]:
		return model.MediaKindAttachment, ext, true
	default:
		return "", ext, false
	}
}

func (s *Storage) MaxBytesForKind(kind string) int64 {
	switch kind {
	case model.MediaKindImage:
		return s.limits.MaxImageBytes
	case model.MediaKindVideo:
		return s.limits.MaxVideoBytes
	default:
		return s.limits.MaxAttachmentBytes
	}
}

// Save prüft Whitelist + Größenlimit, berechnet SHA256 und legt die Datei unter YYYY/MM/<id>.<ext> ab.
func (s *Storage) Save(r io.Reader, originalName, uploadedBy string) (model.MediaFile, error) {
	kind, ext, ok := ClassifyExtension(originalName)
	if !ok {
		return model.MediaFile{}, ErrUnsupportedType
	}

	maxBytes := s.MaxBytesForKind(kind)
	limited := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return model.MediaFile{}, fmt.Errorf("upload lesen: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return model.MediaFile{}, ErrFileTooLarge
	}

	mimeType, valid := validateMediaContent(kind, ext, data)
	if !valid {
		return model.MediaFile{}, ErrUnsupportedType
	}

	id := newID()
	now := time.Now().UTC()
	relPath := filepath.ToSlash(filepath.Join(now.Format("2006"), now.Format("01"), id+ext))
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(relPath))

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return model.MediaFile{}, fmt.Errorf("medienordner anlegen: %w", err)
	}
	if err := os.WriteFile(fullPath, data, 0o640); err != nil {
		return model.MediaFile{}, fmt.Errorf("medien-datei schreiben: %w", err)
	}

	sum := sha256.Sum256(data)

	return model.MediaFile{
		ID:           id,
		StoredPath:   relPath,
		OriginalName: filepath.Base(originalName),
		MimeType:     mimeType,
		Kind:         kind,
		SizeBytes:    int64(len(data)),
		SHA256:       hex.EncodeToString(sum[:]),
		UploadedBy:   uploadedBy,
		CreatedAt:    now,
	}, nil
}

// validateMediaContent prüft Magic-Bytes gegen den deklarierten Bild-/Videotyp (Schutz vor getarntem HTML/XSS).
func validateMediaContent(kind, ext string, data []byte) (string, bool) {
	if kind == model.MediaKindAttachment {
		return "application/octet-stream", true
	}
	detected := http.DetectContentType(data)
	switch kind {
	case model.MediaKindImage:
		switch ext {
		case ".png":
			return "image/png", detected == "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg", detected == "image/jpeg"
		case ".webp":
			return "image/webp", detected == "image/webp"
		case ".gif":
			return "image/gif", detected == "image/gif"
		}
	case model.MediaKindVideo:
		switch ext {
		case ".mp4":
			// MP4 ISO Base Media File Format: "ftyp" an Byte-Offset 4
			if len(data) >= 12 && string(data[4:8]) == "ftyp" {
				return "video/mp4", true
			}
		case ".webm":
			// WebM / EBML Magic-Header: 0x1A 0x45 0xDF 0xA3
			if len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3 {
				return "video/webm", true
			}
		}
	}
	return "", false
}

func (s *Storage) ResolvePath(storedPath string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(storedPath))
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", errors.New("ungültiger medienpfad")
	}
	return filepath.Join(s.baseDir, cleaned), nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
