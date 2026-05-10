package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coherence/internal/config"
	"coherence/internal/docgen"
	"coherence/internal/server"
)

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dataDir := t.TempDir()
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
