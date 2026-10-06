package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The round trip is what makes version history work: a snapshot of the rendered
// HTML is also a snapshot of the markdown source, so restore can re-render
// rather than resurrect markup.
func TestExtractRawMarkdownRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DataDir: dir, DocBase: "http://localhost"}

	content := "# Title\n\nSome *markdown* with a closing tag `</div>` in it.\n\n" +
		"```html\n<script>alert(1)</script>\n```\n\n- item one\n- item two\n"

	if _, err := GenerateDoc(cfg, "proj", "My Doc", content, "my-doc.html"); err != nil {
		t.Fatalf("GenerateDoc: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "proj", "my-doc.html"))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := ExtractRawMarkdown(data)
	if !ok {
		t.Fatal("ExtractRawMarkdown reported no embedded source")
	}
	if got != content {
		t.Errorf("round trip changed the source.\nwant:\n%q\ngot:\n%q", content, got)
	}
}

func TestExtractRawMarkdownAbsent(t *testing.T) {
	if _, ok := ExtractRawMarkdown([]byte("<html><body>uploaded file</body></html>")); ok {
		t.Error("expected no embedded source in foreign HTML")
	}
}

func TestExtractTitle(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DataDir: dir, DocBase: "http://localhost"}
	title := "VAL-123 Gateway & Spoke Analysis"
	if _, err := GenerateDoc(cfg, "proj", title, "# body\n", "t.html"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "proj", "t.html"))
	if err != nil {
		t.Fatal(err)
	}
	// The template appends " — coherence" and HTML-escapes the ampersand; both
	// must be undone.
	if got := ExtractTitle(data); got != title {
		t.Errorf("want %q, got %q", title, got)
	}
}

func TestExtractTitleAbsent(t *testing.T) {
	if got := ExtractTitle([]byte("<html><body>no title</body></html>")); got != "" {
		t.Errorf("expected empty title, got %q", got)
	}
}

func TestExtractRawMarkdownScriptTerminator(t *testing.T) {
	// A source body containing the literal script close tag must not truncate
	// the extraction — GenerateDoc escapes "</" for exactly this reason.
	dir := t.TempDir()
	cfg := &Config{DataDir: dir, DocBase: "http://localhost"}
	content := "before\n</script>\nafter\n"
	if _, err := GenerateDoc(cfg, "proj", "Edge", content, "edge.html"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "proj", "edge.html"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ExtractRawMarkdown(data)
	if !ok {
		t.Fatal("no embedded source found")
	}
	if got != content {
		t.Errorf("want %q, got %q", content, got)
	}
	if !strings.Contains(got, "after") {
		t.Error("extraction truncated at the embedded close tag")
	}
}
