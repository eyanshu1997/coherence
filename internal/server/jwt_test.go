package server

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func makeJWTSegment(claims map[string]any) string {
	raw, _ := json.Marshal(claims)
	return base64.URLEncoding.EncodeToString(raw) // standard URL encoding WITH padding (= suffix)
}

func makeJWTSegmentRaw(claims map[string]any) string {
	raw, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(raw) // no padding
}

func fakeJWT(payloadSeg string) string {
	return "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9." + payloadSeg + ".fakesig"
}

// TestExtractEmailFromJWT_WithPadding verifies that JWTs with standard base64
// padding (as emitted by AWS ALB) are decoded correctly.
func TestExtractEmailFromJWT_WithPadding(t *testing.T) {
	seg := makeJWTSegment(map[string]any{"email": "alice@example.com"})
	if !strings.HasSuffix(seg, "=") {
		// If the encoded string happens not to need padding, use a longer email
		// to force at least one = character.
		seg = makeJWTSegment(map[string]any{"email": "alice@example.com", "sub": "extra"})
	}
	got := extractEmailFromJWT(fakeJWT(seg))
	if got != "alice@example.com" {
		t.Errorf("extractEmailFromJWT with padding: got %q, want %q", got, "alice@example.com")
	}
}

// TestExtractEmailFromJWT_NoPadding verifies that JWTs without padding also work.
func TestExtractEmailFromJWT_NoPadding(t *testing.T) {
	seg := makeJWTSegmentRaw(map[string]any{"email": "bob@example.com"})
	got := extractEmailFromJWT(fakeJWT(seg))
	if got != "bob@example.com" {
		t.Errorf("extractEmailFromJWT no padding: got %q, want %q", got, "bob@example.com")
	}
}

// TestExtractEmailFromJWT_DoublePadding verifies == padding is also handled.
func TestExtractEmailFromJWT_DoublePadding(t *testing.T) {
	// Craft a payload that encodes to a length requiring == padding
	seg := makeJWTSegment(map[string]any{"email": "x@y.z"})
	got := extractEmailFromJWT(fakeJWT(seg))
	if got != "x@y.z" {
		t.Errorf("extractEmailFromJWT double-padding: got %q, want %q", got, "x@y.z")
	}
}

func TestExtractEmailFromJWT_NoEmail(t *testing.T) {
	seg := makeJWTSegment(map[string]any{"sub": "no-email-here"})
	got := extractEmailFromJWT(fakeJWT(seg))
	if got != "" {
		t.Errorf("extractEmailFromJWT no email claim: got %q, want empty", got)
	}
}

func TestExtractEmailFromJWT_Garbage(t *testing.T) {
	got := extractEmailFromJWT("notajwt")
	if got != "" {
		t.Errorf("extractEmailFromJWT garbage input: got %q, want empty", got)
	}
}
