package markdown

import (
	"bytes"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Renderer konvertiert Markdown serverseitig und bereinigt das Ergebnis strikt gegen XSS.
type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

var localMediaURL = regexp.MustCompile(`^/media/[a-f0-9]{16,64}$`)

func New() *Renderer {
	// WithUnsafe erlaubt kontrolliertes HTML (z. B. <video>) vor dem strengen bluemonday-Sanitizer.
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.TaskList,
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)

	p := bluemonday.UGCPolicy()
	// Lokale Video-Tags (MP4/WebM) kontrolliert erlauben, ausschließlich für lokale /media/-Pfade
	p.AllowElements("video", "source")
	p.AllowAttrs("controls", "preload").OnElements("video")
	p.AllowAttrs("src").Matching(localMediaURL).OnElements("video", "source")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^video/(mp4|webm)$`)).OnElements("source")
	p.AllowAttrs("class").OnElements("code", "pre", "span")

	return &Renderer{md: md, policy: p}
}

func (r *Renderer) RenderHTML(src string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return r.policy.Sanitize(buf.String()), nil
}
