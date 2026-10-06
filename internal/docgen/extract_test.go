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
	title := "TICKET-123 Routing & Peering Analysis"
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

// The CLI passes --folder straight through, so containment has to live in the
// generator. Without it, "--folder ../../.ssh" wrote a document into ~/.ssh and
// chmod 0755'd every directory up to the filesystem root on the way out.
func TestGenerateDocRefusesEscapingFolder(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	victim := filepath.Join(root, "victim")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victim, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DataDir: dataDir, DocBase: "http://localhost"}

	for _, folder := range []string{"../victim", "../../victim", "a/../../victim", "/etc"} {
		if _, err := GenerateDoc(cfg, folder, "Escape", "# x\n", "esc.html"); err == nil {
			t.Errorf("folder %q should have been refused", folder)
		}
		if _, err := os.Stat(filepath.Join(victim, "esc.html")); err == nil {
			t.Fatalf("folder %q escaped the data dir", folder)
		}
	}
	// The victim directory's permissions must be untouched.
	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("ancestor permissions were relaxed to %v", info.Mode().Perm())
	}
}

// A document named "index" was written and then destroyed by the folder index
// inside the same call, with a 200 and a working URL returned, and was excluded
// from snapshots so it was unrecoverable.
func TestGenerateDocRefusesReservedName(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DataDir: dir, DocBase: "http://localhost"}
	if _, err := GenerateDoc(cfg, "notes", "Index", "# MY IMPORTANT CONTENT\n", "index.html"); err == nil {
		t.Fatal("writing a doc named index.html should be refused")
	}
	// And the folder index itself must still be generatable.
	if _, err := GenerateDoc(cfg, "notes", "Real Doc", "# fine\n", "real.html"); err != nil {
		t.Fatalf("a normal doc should still generate: %v", err)
	}
}

// The id has to survive every rewrite, or history loses track of the document.
func TestDocUIDIsStableAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DataDir: dir, DocBase: "http://localhost"}
	read := func() string {
		data, err := os.ReadFile(filepath.Join(dir, "proj", "d.html"))
		if err != nil {
			t.Fatal(err)
		}
		return ExtractUID(data)
	}
	if _, err := GenerateDoc(cfg, "proj", "D", "# one\n", "d.html"); err != nil {
		t.Fatal(err)
	}
	first := read()
	if first == "" {
		t.Fatal("no document id was assigned")
	}
	if _, err := GenerateDoc(cfg, "proj", "D", "# two\n", "d.html"); err != nil {
		t.Fatal(err)
	}
	if second := read(); second != first {
		t.Errorf("id changed on rewrite: %q -> %q", first, second)
	}
	// A different document must get a different id.
	if _, err := GenerateDoc(cfg, "proj", "E", "# other\n", "e.html"); err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(dir, "proj", "e.html"))
	if err != nil {
		t.Fatal(err)
	}
	if ExtractUID(other) == first {
		t.Error("two documents share an id")
	}
}
