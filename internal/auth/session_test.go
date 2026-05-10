package auth

import (
	"strings"
	"testing"
)

func TestSessionTokenRoundTrip(t *testing.T) {
	secret := "testsecret1234"
	token := MakeSessionToken(secret)
	if !strings.Contains(token, ".") {
		t.Fatalf("token missing dot separator: %q", token)
	}
	if !VerifySessionToken(token, secret) {
		t.Error("valid token failed verification")
	}
	if VerifySessionToken(token, "wrongsecret") {
		t.Error("token verified with wrong secret")
	}
	if VerifySessionToken("invalid.token", secret) {
		t.Error("invalid token should not verify")
	}
	if VerifySessionToken("", secret) {
		t.Error("empty token should not verify")
	}
}

func TestHashPasswordConsistency(t *testing.T) {
	h1 := HashPassword("mypassword")
	h2 := HashPassword("mypassword")
	if h1 != h2 {
		t.Error("HashPassword is not deterministic")
	}
	if HashPassword("a") == HashPassword("b") {
		t.Error("different passwords should have different hashes")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got len %d", len(h1))
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("abc", "abc") {
		t.Error("equal strings should match")
	}
	if ConstantTimeEqual("abc", "def") {
		t.Error("different strings should not match")
	}
	if ConstantTimeEqual("abc", "") {
		t.Error("non-empty vs empty should not match")
	}
}
