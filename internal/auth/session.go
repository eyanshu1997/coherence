package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func MakeSessionToken(secret string) string {
	raw := make([]byte, 24)
	rand.Read(raw)
	rawHex := hex.EncodeToString(raw)
	sig := signHex(rawHex, secret)
	return rawHex + "." + sig
}

func VerifySessionToken(token, secret string) bool {
	raw, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	expected := signHex(raw, secret)
	return hmac.Equal([]byte(sig), []byte(expected))
}

func HashPassword(pw string) string {
	h := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(h[:])
}

func ConstantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func signHex(raw, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}
