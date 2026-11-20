package markdown

import (
	"bytes"

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

func New() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.TaskList,
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
		),
	)

	p := bluemonday.UGCPolicy()
	// Lokale Video-Tags (MP4/WebM) kontrolliert erlauben
	p.AllowElements("video", "source")
	p.AllowAttrs("controls", "preload").OnElements("video")
	p.AllowAttrs("src", "type").OnElements("source", "video")
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
