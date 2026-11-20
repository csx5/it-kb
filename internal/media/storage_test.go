package media

import (
	"errors"
	"strings"
	"testing"
)

func TestStorage_WhitelistSizeLimitAndPathTraversal(t *testing.T) {
	s := NewStorage(t.TempDir(), Limits{
		MaxImageBytes:      16,
		MaxVideoBytes:      64,
		MaxAttachmentBytes: 32,
	})

	if _, err := s.Save(strings.NewReader("abc"), "evil.exe", "MK"); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("erwartete ErrUnsupportedType, erhalten: %v", err)
	}

	if _, err := s.Save(strings.NewReader(strings.Repeat("a", 20)), "bild.png", "MK"); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("erwartete ErrFileTooLarge, erhalten: %v", err)
	}

	if _, err := s.ResolvePath("../../etc/passwd"); err == nil {
		t.Fatalf("erwartete fehler bei path traversal")
	}
	if _, err := s.ResolvePath("/etc/passwd"); err == nil {
		t.Fatalf("erwartete fehler bei absolutem pfad")
	}
}
