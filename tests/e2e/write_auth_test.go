package e2e

import (
	"encoding/base64"
	"io"
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

// newTestServerWithUntrustedProxy models the real deployment shape: an auth
// proxy in front, an allowlist, and no assertion that the bare identity header
// is trustworthy — so a caller-supplied identity is worth nothing.
func newTestServerWithUntrustedProxy(t *testing.T, allowedUsers []string) (*httptest.Server, string) {
	t.Helper()
	dataDir := tempDataDir(t)
	coherenceHome := t.TempDir()
	os.MkdirAll(filepath.Join(coherenceHome, "www", "assets"), 0755)
	cfg := &config.Config{
		DataDir:             dataDir,
		CoherenceHome:       coherenceHome,
		DocBase:             "http://localhost",
		CoherencePort:       "8080",
		CoherenceBind:       "127.0.0.1",
		AuthFile:            filepath.Join(t.TempDir(), "auth.json"),
		SharesFile:          filepath.Join(t.TempDir(), "shares.json"),
		RemoteUserHeader:    "X-Remote-User",
		RemoteUserJWTHeader: "X-Remote-User-JWT",
		AllowedUsers:        allowedUsers,
		GuestAccess:         true,
	}
	h := server.New(cfg, &docgen.Config{DataDir: dataDir, DocBase: "http://localhost"})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, dataDir
}

// Every mutating endpoint. Before version history was added, the ones marked
// below carried no authentication check at all, so an unauthenticated caller
// could recursively delete the document tree or write files into it.
var mutatingEndpoints = []struct {
	path string
	body string
}{
	{"/delete-folder", `{"folder":"victim"}`},
	{"/delete-doc", `{"folder":"victim","file":"doc"}`},
	{"/rename-folder", `{"folder":"victim","new_name":"taken"}`},
	{"/move-folder", `{"folder":"victim","dest_parent":"other"}`},
	{"/rename-doc", `{"folder":"victim","filename":"doc.html","new_name":"other.html"}`},
	{"/move-doc", `{"folder":"victim","filename":"doc.html","dest_folder":"other"}`},
	{"/create-folder", `{"folder":"intruder"}`},
	{"/create-doc", `{"folder":"victim","title":"T","content":"x"}`},
	{"/update-doc", `{"folder":"victim","filename":"doc.html","content":"x"}`},
	{"/comment", `{"folder":"victim","file":"doc","text":"hi"}`},
	{"/acknowledge", `{"folder":"victim","file":"doc","ts":"2026-01-01T00:00:00Z"}`},
	{"/reply-comment", `{"folder":"victim","file":"doc","ts":"2026-01-01T00:00:00Z","reply":"x"}`},
	{"/add-reply", `{"folder":"victim","file":"doc","ts":"2026-01-01T00:00:00Z","text":"x"}`},
	{"/delete-comment", `{"folder":"victim","file":"doc","ts":"2026-01-01T00:00:00Z"}`},
	{"/delete-session", `{"folder":"victim","session_id":"00000000-0000-0000-0000-000000000000"}`},
	{"/exclude-session", `{"folder":"victim","session_id":"x"}`},
	{"/reindex", `{}`},
	{"/auth/share/create", `{"path":"/victim/doc.html","days":30}`},
	// Omitted when this list was written, including the endpoint the version
	// history feature added.
	{"/restore-doc", `{"folder":"victim","file":"doc","rev":"abcdef12"}`},
	{"/upload-file", `{}`},
	{"/upload-image", `{}`},
}

// postAs sends a request the way nginx forwards one: from loopback, but with
// the proxy headers set. Without them the server treats the caller as a local
// CLI, which is a different trust path (see TestLocalCallerCanMutate).
func postAs(t *testing.T, url, user, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Real-IP", "203.0.113.9")
	if user != "" {
		req.Header.Set("X-Remote-User", user)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// An anonymous caller must not be able to mutate anything behind an auth proxy.
// This is the regression test for the unauthenticated-delete finding.
func TestAnonymousCannotMutate(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, []string{"owner@example.com"}, "")
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)

	for _, ep := range mutatingEndpoints {
		if code := postAs(t, ts.URL+ep.path, "", ep.body); code != 401 {
			t.Errorf("POST %s as anonymous: expected 401, got %d", ep.path, code)
		}
	}
	// The tree is still there.
	if _, err := os.Stat(filepath.Join(dataDir, "victim")); err != nil {
		t.Errorf("anonymous request removed the folder: %v", err)
	}
}

// A reader admitted by GUEST_ACCESS can read but must not mutate: the UI hides
// every write control from them, and the server has to agree.
func TestGuestCannotMutate(t *testing.T) {
	ts, dataDir := newTestServerWithGuestAccess(t, []string{"owner@example.com"})
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)
	os.WriteFile(filepath.Join(dataDir, "victim", "doc.html"), []byte("<html></html>"), 0644)

	for _, ep := range mutatingEndpoints {
		if code := postAs(t, ts.URL+ep.path, "guest@example.com", ep.body); code != 401 {
			t.Errorf("POST %s as guest: expected 401, got %d", ep.path, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "victim", "doc.html")); err != nil {
		t.Errorf("guest request removed the doc: %v", err)
	}
}

// The allowlisted owner must still be able to work: these calls may fail on
// their own merits (404, 409) but must never be rejected as unauthorized.
func TestOwnerCanMutate(t *testing.T) {
	ts, dataDir := newTestServerWithGuestAccess(t, []string{"owner@example.com"})
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)
	os.WriteFile(filepath.Join(dataDir, "victim", "doc.html"), []byte("<html></html>"), 0644)

	for _, ep := range mutatingEndpoints {
		if code := postAs(t, ts.URL+ep.path, "owner@example.com", ep.body); code == 401 {
			t.Errorf("POST %s as the allowlisted owner: got 401, expected it to be authorized", ep.path)
		}
	}
}

// With no allowlist, no password file and no API key the server is in local-only
// mode and stays open, which is how it runs on a laptop.
func TestLocalModeStaysOpen(t *testing.T) {
	ts, dataDir := newTestServer(t)
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)
	if code := postAs(t, ts.URL+"/create-folder", "", `{"folder":"local-ok"}`); code != 200 {
		t.Errorf("local mode should allow writes, got %d", code)
	}
}

// A caller on this host with no proxy headers is a local CLI — the coherence-doc
// binary, or the curl calls the Claude doc skills make. Those carry no
// credentials and must keep working even with an allowlist configured, since a
// local process can write the data directory directly anyway.
func TestLocalCallerCanMutate(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, []string{"owner@example.com"}, "")
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/create-folder",
		strings.NewReader(`{"folder":"from-cli"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("a local CLI call should be allowed, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "from-cli")); err != nil {
		t.Errorf("local CLI call did not take effect: %v", err)
	}
}

// The proxy headers are what make a request untrusted, and a client cannot shed
// them: nginx overwrites whatever it was sent. A forwarded request from an
// unidentified user must be refused even though it arrives from loopback.
func TestProxiedRequestIsNotTreatedAsLocal(t *testing.T) {
	ts, dataDir := newTestServerWithIdentity(t, []string{"owner@example.com"}, "")
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)
	if code := postAs(t, ts.URL+"/delete-folder", "", `{"folder":"victim"}`); code != 401 {
		t.Fatalf("expected 401 for a proxied anonymous delete, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "victim")); err != nil {
		t.Errorf("proxied anonymous request deleted the folder: %v", err)
	}
}

// Dot-prefixed paths are never documents. Refusing the whole class keeps a
// stray repository or editor backup in the tree from being served as content.
func TestDotPathsNotServed(t *testing.T) {
	ts, dataDir := newTestServer(t)
	gitDir := filepath.Join(dataDir, ".git")
	os.MkdirAll(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\tsecret = yes\n"), 0644)

	for _, p := range []string{"/.git/config", "/.git/", "/.env"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		body := make([]byte, 256)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("GET %s: expected 404, got %d", p, resp.StatusCode)
		}
		if strings.Contains(string(body[:n]), "secret") {
			t.Errorf("GET %s leaked repository contents", p)
		}
	}
}

// The confirmed bypass: with the identity header untrusted by default, claiming
// to be an allowlisted user must achieve nothing. Before this, sending
// "X-Remote-User: owner@example.com" granted full owner authority on all 24
// mutating endpoints and defeated COHERENCE_API_KEY entirely.
func TestForgedIdentityHeaderGrantsNothing(t *testing.T) {
	ts, dataDir := newTestServerWithUntrustedProxy(t, []string{"owner@example.com"})
	os.MkdirAll(filepath.Join(dataDir, "victim"), 0755)

	for _, ep := range mutatingEndpoints {
		if code := postAs(t, ts.URL+ep.path, "owner@example.com", ep.body); code != 401 {
			t.Errorf("POST %s with a forged identity: expected 401, got %d", ep.path, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "victim")); err != nil {
		t.Errorf("a forged identity deleted the folder: %v", err)
	}
}

// An unsigned JWT must be worth no more than the bare header.
func TestForgedJWTGrantsNothing(t *testing.T) {
	ts, _ := newTestServerWithUntrustedProxy(t, []string{"owner@example.com"})
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"owner@example.com"}`))
	token := hdr + "." + body + ".forged"

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/delete-folder",
		strings.NewReader(`{"folder":"victim"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Remote-User-JWT", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("a forged OIDC token must be rejected, got %d", resp.StatusCode)
	}
}

// /search, /list-folders and /comments return document content and had no
// authorization check, bypassing the auth applied to the documents themselves.
func TestReadEndpointsRequireAuthorization(t *testing.T) {
	ts, dataDir := newTestServerWithUntrustedProxy(t, []string{"owner@example.com"})
	os.MkdirAll(filepath.Join(dataDir, "secret"), 0755)
	os.WriteFile(filepath.Join(dataDir, "secret", "doc.html"),
		[]byte("<html><title>Secret</title><div class=\"content\">db_password hunter2</div></html>"), 0644)

	for _, path := range []string{
		"/search?q=password",
		"/list-folders",
		"/comments?folder=secret&file=doc",
	} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("GET %s anonymously: expected 401, got %d", path, resp.StatusCode)
		}
		if strings.Contains(string(body), "hunter2") || strings.Contains(string(body), "secret") {
			t.Errorf("GET %s leaked document content: %s", path, body)
		}
	}
}
