package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coherence/internal/config"
	"coherence/internal/docgen"
	"coherence/internal/server"
	"coherence/internal/versioning"
)

// newVersionedServer builds an API-key server with a snapshot store attached.
// The store's repository lives outside the data dir, as in production.
func newVersionedServer(t *testing.T, apiKey string) (*httptest.Server, string, *versioning.Store) {
	t.Helper()
	dataDir := tempDataDir(t)
	coherenceHome := t.TempDir()
	os.MkdirAll(filepath.Join(coherenceHome, "www", "assets"), 0755)

	cfg := &config.Config{
		DataDir:       dataDir,
		CoherenceHome: coherenceHome,
		DocBase:       "http://localhost",
		CoherencePort: "8080",
		CoherenceBind: "127.0.0.1",
		AuthFile:      filepath.Join(t.TempDir(), "auth.json"),
		SharesFile:    filepath.Join(t.TempDir(), "shares.json"),
		APIKey:        apiKey,
	}
	dgCfg := &docgen.Config{DataDir: dataDir, DocBase: "http://localhost"}
	h := server.New(cfg, dgCfg)

	store, err := versioning.New(filepath.Join(t.TempDir(), "versions.git"), dataDir, 50*time.Millisecond)
	if err != nil {
		t.Skipf("versioning unavailable: %v", err)
	}
	h.SetVersionStore(store)

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, dataDir, store
}

func doJSON(t *testing.T, method, url, key, body string) (int, map[string]any) {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func revisionsOf(t *testing.T, ts *httptest.Server, key, folder, file string) []any {
	t.Helper()
	code, body := doJSON(t, "GET",
		ts.URL+"/doc-history?folder="+folder+"&file="+file, key, "")
	if code != 200 {
		t.Fatalf("GET /doc-history: expected 200, got %d (%v)", code, body)
	}
	revs, _ := body["revisions"].([]any)
	return revs
}

func TestVersionHistoryAccumulates(t *testing.T) {
	ts, _, store := newVersionedServer(t, "k")

	code, body := doJSON(t, "POST", ts.URL+"/create-doc", "k",
		`{"folder":"proj","title":"Doc","filename":"doc.html","content":"# one\n\noriginal body\n"}`)
	if code != 200 {
		t.Fatalf("create-doc: %d (%v)", code, body)
	}
	// Force the snapshot rather than waiting on the debounce.
	if err := store.CommitNow("test v1"); err != nil {
		t.Fatal(err)
	}

	if code, body := doJSON(t, "POST", ts.URL+"/update-doc", "k",
		`{"folder":"proj","filename":"doc.html","title":"Doc","content":"# one\n\nedited body\n"}`); code != 200 {
		t.Fatalf("update-doc: %d (%v)", code, body)
	}
	if err := store.CommitNow("test v2"); err != nil {
		t.Fatal(err)
	}

	revs := revisionsOf(t, ts, "k", "proj", "doc")
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d: %v", len(revs), revs)
	}
}

func TestVersionSourceAndDiff(t *testing.T) {
	ts, _, store := newVersionedServer(t, "k")

	doJSON(t, "POST", ts.URL+"/create-doc", "k",
		`{"folder":"proj","title":"Doc","filename":"doc.html","content":"# h\n\noriginal body\n"}`)
	store.CommitNow("v1")
	doJSON(t, "POST", ts.URL+"/update-doc", "k",
		`{"folder":"proj","filename":"doc.html","title":"Doc","content":"# h\n\nedited body\n"}`)
	store.CommitNow("v2")

	revs := revisionsOf(t, ts, "k", "proj", "doc")
	oldest := revs[len(revs)-1].(map[string]any)
	rev := oldest["rev"].(string)

	// The source of a past revision must come back as markdown, not HTML.
	code, body := doJSON(t, "GET",
		ts.URL+"/doc-version?folder=proj&file=doc&rev="+rev, "k", "")
	if code != 200 {
		t.Fatalf("doc-version: %d (%v)", code, body)
	}
	if body["is_markdown"] != true {
		t.Errorf("expected is_markdown true, got %v", body["is_markdown"])
	}
	content, _ := body["content"].(string)
	if !strings.Contains(content, "original body") {
		t.Errorf("expected the original markdown, got %q", content)
	}
	if strings.Contains(content, "<html") {
		t.Errorf("content should be markdown, not rendered HTML: %q", content)
	}

	// The diff is against the current document.
	code, body = doJSON(t, "GET",
		ts.URL+"/doc-diff?folder=proj&file=doc&rev="+rev, "k", "")
	if code != 200 {
		t.Fatalf("doc-diff: %d (%v)", code, body)
	}
	diff, _ := body["diff"].(string)
	if !strings.Contains(diff, "-original body") || !strings.Contains(diff, "+edited body") {
		t.Errorf("diff did not describe the change:\n%s", diff)
	}
}

// Restore must add a new version rather than rewrite history.
func TestRestoreAddsNewVersion(t *testing.T) {
	ts, dataDir, store := newVersionedServer(t, "k")

	doJSON(t, "POST", ts.URL+"/create-doc", "k",
		`{"folder":"proj","title":"Doc","filename":"doc.html","content":"# h\n\noriginal body\n"}`)
	store.CommitNow("v1")
	doJSON(t, "POST", ts.URL+"/update-doc", "k",
		`{"folder":"proj","filename":"doc.html","title":"Doc","content":"# h\n\nedited body\n"}`)
	store.CommitNow("v2")

	before := revisionsOf(t, ts, "k", "proj", "doc")
	oldest := before[len(before)-1].(map[string]any)
	oldRev := oldest["rev"].(string)

	code, body := doJSON(t, "POST", ts.URL+"/restore-doc", "k",
		`{"folder":"proj","file":"doc","rev":"`+oldRev+`"}`)
	if code != 200 {
		t.Fatalf("restore-doc: %d (%v)", code, body)
	}
	store.CommitNow("post-restore")

	// The doc on disk holds the restored source again.
	data, err := os.ReadFile(filepath.Join(dataDir, "proj", "doc.html"))
	if err != nil {
		t.Fatal(err)
	}
	md, ok := docgen.ExtractRawMarkdown(data)
	if !ok {
		t.Fatal("restored doc has no embedded source")
	}
	if !strings.Contains(md, "original body") {
		t.Errorf("expected the restored body, got %q", md)
	}

	// History grew; the version that was replaced is still reachable.
	after := revisionsOf(t, ts, "k", "proj", "doc")
	if len(after) <= len(before) {
		t.Fatalf("restore must append to history, not rewrite it: %d -> %d", len(before), len(after))
	}
	for _, r := range after {
		if r.(map[string]any)["rev"] == oldRev {
			goto found
		}
	}
	t.Error("the restored-from revision disappeared from history")
found:
	code, body = doJSON(t, "GET", ts.URL+"/doc-version?folder=proj&file=doc&rev="+
		before[0].(map[string]any)["rev"].(string), "k", "")
	if code != 200 {
		t.Fatalf("the pre-restore version should still be readable: %d (%v)", code, body)
	}
	if c, _ := body["content"].(string); !strings.Contains(c, "edited body") {
		t.Errorf("pre-restore content lost, got %q", c)
	}
}

func TestVersionEndpointsRejectWrongKey(t *testing.T) {
	ts, _, _ := newVersionedServer(t, "correct")
	for _, u := range []string{
		"/doc-history?folder=proj&file=doc",
		"/doc-version?folder=proj&file=doc&rev=abcdef12",
		"/doc-diff?folder=proj&file=doc&rev=abcdef12",
	} {
		if code, _ := doJSON(t, "GET", ts.URL+u, "wrong", ""); code != 401 {
			t.Errorf("GET %s with a wrong key: expected 401, got %d", u, code)
		}
	}
	if code, _ := doJSON(t, "POST", ts.URL+"/restore-doc", "wrong",
		`{"folder":"proj","file":"doc","rev":"abcdef12"}`); code != 401 {
		t.Errorf("POST /restore-doc with a wrong key: expected 401, got %d", code)
	}
}

func TestVersioningDisabledReturns503(t *testing.T) {
	// No store attached: history is unavailable but the server still serves.
	ts, _ := newTestServerWithAPIKey(t, "k")
	if code, _ := doJSON(t, "GET", ts.URL+"/doc-history?folder=proj&file=doc", "k", ""); code != 503 {
		t.Errorf("expected 503 when versioning is off, got %d", code)
	}
}
