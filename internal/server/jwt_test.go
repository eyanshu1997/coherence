package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coherence/internal/config"
	"coherence/internal/docgen"
)

// Identity used to come from decoding a JWT without checking its signature, so
// a hand-made token named whoever the caller liked. These tests pin the
// replacement contract: an unverifiable token is not an identity.

func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	hdr, _ := json.Marshal(map[string]any{"alg": "ES256", "typ": "JWT"})
	body, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(body) + ".not-a-real-signature"
}

func identityHandler(t *testing.T, cfg *config.Config) *Handler {
	t.Helper()
	cfg.DataDir = t.TempDir()
	cfg.CoherenceHome = t.TempDir()
	cfg.AuthFile = t.TempDir() + "/auth.json"
	cfg.SharesFile = t.TempDir() + "/shares.json"
	return New(cfg, &docgen.Config{DataDir: cfg.DataDir, DocBase: "http://localhost"})
}

func userFor(h *Handler, headers map[string]string) string {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return h.currentUser(r)
}

func TestUnsignedJWTIsNotAnIdentity(t *testing.T) {
	h := identityHandler(t, &config.Config{
		RemoteUserJWTHeader: "X-Remote-User-JWT",
		RemoteUserJWTSigner: "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/0123456789abcdef",
		AllowedUsers:        []string{"owner@example.com"},
	})
	tok := unsignedJWT(t, map[string]any{"email": "owner@example.com"})
	if got := userFor(h, map[string]string{"X-Remote-User-JWT": tok}); got != "anonymous" {
		t.Fatalf("a forged token must not yield an identity, got %q", got)
	}
}

func TestJWTWithoutPinnedSignerIsNotAnIdentity(t *testing.T) {
	// No REMOTE_USER_JWT_SIGNER: nothing can be verified, so nothing is trusted.
	h := identityHandler(t, &config.Config{
		RemoteUserJWTHeader: "X-Remote-User-JWT",
		AllowedUsers:        []string{"owner@example.com"},
	})
	tok := unsignedJWT(t, map[string]any{"email": "owner@example.com"})
	if got := userFor(h, map[string]string{"X-Remote-User-JWT": tok}); got != "anonymous" {
		t.Fatalf("without a pinned signer no token is an identity, got %q", got)
	}
}

func TestBareRemoteUserHeaderUntrustedByDefault(t *testing.T) {
	h := identityHandler(t, &config.Config{
		RemoteUserHeader: "X-Remote-User",
		AllowedUsers:     []string{"owner@example.com"},
	})
	if got := userFor(h, map[string]string{"X-Remote-User": "owner@example.com"}); got != "anonymous" {
		t.Fatalf("an unsigned identity header must not be trusted by default, got %q", got)
	}
}

func TestBareRemoteUserHeaderTrustedWhenOptedIn(t *testing.T) {
	h := identityHandler(t, &config.Config{
		RemoteUserHeader:        "X-Remote-User",
		RemoteUserHeaderTrusted: true,
		AllowedUsers:            []string{"owner@example.com"},
	})
	if got := userFor(h, map[string]string{"X-Remote-User": "owner@example.com"}); got != "owner@example.com" {
		t.Fatalf("an operator-trusted proxy header should be used, got %q", got)
	}
}

func TestNoIdentityHeadersIsAnonymous(t *testing.T) {
	h := identityHandler(t, &config.Config{
		RemoteUserHeader:        "X-Remote-User",
		RemoteUserHeaderTrusted: true,
	})
	if got := userFor(h, nil); got != "anonymous" {
		t.Fatalf("got %q", got)
	}
	if got := userFor(h, map[string]string{"X-Remote-User": "   "}); got != "anonymous" {
		t.Fatalf("a whitespace-only header is not an identity, got %q", got)
	}
}
