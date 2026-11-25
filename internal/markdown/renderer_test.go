package markdown

import (
	"strings"
	"testing"
)

func TestRenderer_AllowsLocalVideoAndBlocksXSS(t *testing.T) {
	r := New()

	in := "Hallo **Welt**\n\n<script>alert(1)</script>\n<img src=x onerror=alert(2)>\n<video controls preload=\"metadata\" src=\"/media/0123456789abcdef0123456789abcdef\"></video>\n<video controls src=\"https://evil.example/a.mp4\"></video>"
	out, err := r.RenderHTML(in)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	if strings.Contains(out, "<script") || strings.Contains(out, "onerror") {
		t.Fatalf("XSS nicht entfernt: %s", out)
	}
	if !strings.Contains(out, `src="/media/0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("lokales video-tag fehlt: %s", out)
	}
	if strings.Contains(out, "evil.example") {
		t.Fatalf("externes video-src hätte geblockt werden müssen: %s", out)
	}

	linkHTML, err := r.RenderHTML("Siehe [Spooler-Reset](#article/42)")
	if err != nil || !strings.Contains(linkHTML, `href="#article/42"`) {
		t.Fatalf("interner artikel-querverweis ging verloren: err=%v out=%s", err, linkHTML)
	}
}
