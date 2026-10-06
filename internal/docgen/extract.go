package docgen

import (
	"html"
	"regexp"
	"strings"
)

// Every generated doc carries its own markdown source in a script tag (see
// docTemplate), which makes the rendered artifact self-describing: a snapshot of
// the HTML is also a snapshot of the source. These helpers read it back, so
// history and restore can work on markdown instead of on rendered markup.

var (
	rawMarkdownRe = regexp.MustCompile(`(?s)<script type="application/x-markdown" id="doc-raw-markdown">(.*?)</script>`)
	docTitleRe    = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
)

// ExtractRawMarkdown returns the markdown source embedded in a generated doc.
// The second result is false for HTML that was not produced by this generator
// (an uploaded file, or a doc generated before the source was embedded).
func ExtractRawMarkdown(doc []byte) (string, bool) {
	m := rawMarkdownRe.FindSubmatch(doc)
	if m == nil {
		return "", false
	}
	// GenerateDoc writes the source with "</" escaped to "<\/" so it cannot
	// terminate the script tag early; undo exactly that substitution.
	return strings.ReplaceAll(string(m[1]), "<\\/", "</"), true
}

// ExtractTitle returns the document title from a generated doc, with the
// " — coherence" suffix the template appends removed.
func ExtractTitle(doc []byte) string {
	m := docTitleRe.FindSubmatch(doc)
	if m == nil {
		return ""
	}
	t := html.UnescapeString(strings.TrimSpace(string(m[1])))
	if i := strings.LastIndex(t, " — "); i > 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}
