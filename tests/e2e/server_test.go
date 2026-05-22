package e2e

import (
	"encoding/json"
	"io"
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
)

// tempDataDir creates a temp dir and registers cleanup with a brief delay so
// async ReindexAll goroutines finish before the dir is removed.
func tempDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "coherence-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		time.Sleep(50 * time.Millisecond)
		os.RemoveAll(dir)
	})
	return dir
}

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dataDir := tempDataDir(t)
	coherenceHome := t.TempDir()
	assetsDir := filepath.Join(coherenceHome, "www", "assets")
	os.MkdirAll(assetsDir, 0755)

	cfg := &config.Config{
		DataDir:       dataDir,
		CoherenceHome: coherenceHome,
		DocBase:       "http://localhost",
		CoherencePort: "8080",
		CoherenceBind: "127.0.0.1",
		AuthFile:      filepath.Join(t.TempDir(), "auth.json"),
		SharesFile:    filepath.Join(t.TempDir(), "shares.json"),
	}
	dgCfg := &docgen.Config{
		DataDir: dataDir,
		DocBase: "http://localhost",
	}
	h := server.New(cfg, dgCfg)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, dataDir
}

func TestGetCommentsEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/comments?folder=test-folder&file=test-doc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result []any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("expected empty array, got %v", result)
	}
}

func TestPostAndGetComment(t *testing.T) {
	ts, dataDir := newTestServer(t)
	os.MkdirAll(filepath.Join(dataDir, "test-folder"), 0755)

	body := `{"folder":"test-folder","file":"test-doc","text":"Hello comment"}`
	resp, err := http.Post(ts.URL+"/comment", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST /comment: expected 200, got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/comments?folder=test-folder&file=test-doc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var comments []map[string]any
	json.NewDecoder(resp.Body).Decode(&comments)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0]["text"] != "Hello comment" {
		t.Errorf("unexpected comment text: %v", comments[0]["text"])
	}
}

func TestListFolders(t *testing.T) {
	ts, dataDir := newTestServer(t)
	os.MkdirAll(filepath.Join(dataDir, "folder-a"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "folder-b", "sub"), 0755)

	resp, err := http.Get(ts.URL + "/list-folders")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	folders, _ := result["folders"].([]any)
	if len(folders) < 3 {
		t.Errorf("expected at least 3 folders, got %d: %v", len(folders), folders)
	}
}

func TestAuthCheckNoConfig(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/auth/check")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("auth/check with no config: expected 200, got %d", resp.StatusCode)
	}
}

func TestLoginPage(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestCreateFolder(t *testing.T) {
	ts, _ := newTestServer(t)
	body := `{"folder":"new-test-folder"}`
	resp, err := http.Post(ts.URL+"/create-folder", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("create-folder: expected 200, got %d", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result)
	}
}

func TestSearch(t *testing.T) {
	ts, dataDir := newTestServer(t)
	// Write a fake HTML doc
	docDir := filepath.Join(dataDir, "test-search")
	os.MkdirAll(docDir, 0755)
	htmlContent := `<html><head><title>My Doc — coherence</title></head><body><div class="content">uniqueword alpha bravo</div></body></html>`
	os.WriteFile(filepath.Join(docDir, "test.html"), []byte(htmlContent), 0644)

	resp, err := http.Get(ts.URL + "/search?q=uniqueword")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	results, _ := result["results"].([]any)
	if len(results) == 0 {
		t.Error("expected search result for 'uniqueword', got none")
	}
}

func TestInvalidCommentPath(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/comments?folder=../escape&file=test")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("path traversal: expected 400, got %d", resp.StatusCode)
	}
}

// newTestServerWithIdentity builds a server configured with an allowlist.
func newTestServerWithIdentity(t *testing.T, allowedUsers []string, allowedDomain string) (*httptest.Server, string) {
	t.Helper()
	dataDir := tempDataDir(t)
	coherenceHome := t.TempDir()
	os.MkdirAll(filepath.Join(coherenceHome, "www", "assets"), 0755)

	cfg := &config.Config{
		DataDir:          dataDir,
		CoherenceHome:    coherenceHome,
		DocBase:          "http://localhost",
		CoherencePort:    "8080",
		CoherenceBind:    "127.0.0.1",
		AuthFile:         filepath.Join(t.TempDir(), "auth.json"),
		SharesFile:       filepath.Join(t.TempDir(), "shares.json"),
		RemoteUserHeader: "X-Remote-User",
		AllowedUsers:     allowedUsers,
		AllowedDomain:    allowedDomain,
	}
	dgCfg := &docgen.Config{DataDir: dataDir, DocBase: "http://localhost"}
	h := server.New(cfg, dgCfg)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, dataDir
}

func TestCommentStoresAuthor(t *testing.T) {
	ts, dataDir := newTestServer(t)
	os.MkdirAll(filepath.Join(dataDir, "test-folder"), 0755)

	req, _ := http.NewRequest("POST", ts.URL+"/comment", strings.NewReader(`{"folder":"test-folder","file":"test-doc","text":"with author"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Remote-User", "alice@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	resp2, err := http.Get(ts.URL + "/comments?folder=test-folder&file=test-doc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var comments []map[string]any
	json.NewDecoder(resp2.Body).Decode(&comments)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	// Default config has no RemoteUserHeader set, so author will be anonymous.
	// This test just verifies the field is present.
	if _, ok := comments[0]["author"]; !ok {
		t.Error("comment missing author field")
	}
}

func TestAllowlistBlocks403(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, []string{"bob@example.com"}, "")
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(dataDir, "index.html"), []byte(`<html><head></head><body>home</body></html>`), 0644)

	// Request with no identity header — should get 403
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("anonymous request with allowlist: expected 403, got %d", resp.StatusCode)
	}
}

func TestAllowlistPermitsUser(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, []string{"alice@example.com"}, "")
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(dataDir, "index.html"), []byte(`<html><head></head><body>home</body></html>`), 0644)

	req, _ := http.NewRequest("GET", ts.URL+"/", nil)
	req.Header.Set("X-Remote-User", "alice@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("permitted user: expected 200, got %d", resp.StatusCode)
	}
}

func TestAllowlistDomainPermits(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, nil, "example.com")
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(dataDir, "index.html"), []byte(`<html><head></head><body>home</body></html>`), 0644)

	req, _ := http.NewRequest("GET", ts.URL+"/", nil)
	req.Header.Set("X-Remote-User", "anyone@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("domain-allowed user: expected 200, got %d", resp.StatusCode)
	}
}

func TestNoAllowlistOpenAccess(t *testing.T) {
	ts, dataDir := newTestServer(t) // no AllowedUsers, no AllowedDomain
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(dataDir, "index.html"), []byte(`<html><head></head><body>home</body></html>`), 0644)

	// No identity header — should still get 200 (open access preserved)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("no-allowlist open access: expected 200, got %d", resp.StatusCode)
	}
}

func TestRemoteUserInjectedInHTML(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, nil, "")
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(dataDir, "index.html"), []byte(`<html><head></head><body>home</body></html>`), 0644)

	req, _ := http.NewRequest("GET", ts.URL+"/", nil)
	req.Header.Set("X-Remote-User", "carol@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	io.Copy(&buf, resp.Body)
	body := buf.String()
	if !strings.Contains(body, `window.REMOTE_USER="carol@example.com"`) {
		t.Errorf("REMOTE_USER not injected into HTML; body: %s", body)
	}
}
