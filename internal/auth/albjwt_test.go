package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSigner = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/0123456789abcdef"

// albFixture stands in for a load balancer: it holds a signing key and serves
// the matching public key the way the regional endpoint does.
type albFixture struct {
	key    *ecdsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newALBFixture(t *testing.T) *albFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &albFixture{key: key, kid: "test-kid-1"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kid := strings.TrimPrefix(r.URL.Path, "/")
		if kid != f.kid {
			w.WriteHeader(404)
			return
		}
		der, err := x509.MarshalPKIXPublicKey(&f.key.PublicKey)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		pem.Encode(w, &pem.Block{Type: "PUBLIC KEY", Bytes: der})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *albFixture) verifier(t *testing.T) *ALBVerifier {
	t.Helper()
	v, err := NewALBVerifier(testSigner)
	if err != nil {
		t.Fatal(err)
	}
	v.endpoint = f.server.URL + "/"
	return v
}

// sign builds a token. The header and claims are passed as maps so tests can
// make them malformed in specific ways.
func (f *albFixture) sign(t *testing.T, header, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(h) + "." +
		base64.RawURLEncoding.EncodeToString(c)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, f.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func goodHeader(f *albFixture) map[string]any {
	return map[string]any{
		"alg":    "ES256",
		"kid":    f.kid,
		"signer": testSigner,
		"iss":    "https://accounts.example.com",
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
}

func goodClaims() map[string]any {
	return map[string]any{"email": "owner@example.com", "sub": "12345"}
}

func TestALBVerifyGoodToken(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	email, err := v.Email(f.sign(t, goodHeader(f), goodClaims()))
	if err != nil {
		t.Fatalf("a correctly signed token must verify: %v", err)
	}
	if email != "owner@example.com" {
		t.Errorf("got %q", email)
	}
}

// The attack the old code was open to: a token nobody signed.
func TestALBRejectsForgedToken(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	hdr, _ := json.Marshal(goodHeader(f))
	claims, _ := json.Marshal(map[string]any{"email": "owner@example.com"})
	forged := base64.RawURLEncoding.EncodeToString(hdr) + "." +
		base64.RawURLEncoding.EncodeToString(claims) + "." +
		base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	if email, err := v.Email(forged); err == nil {
		t.Fatalf("an unsigned token must not verify, got %q", email)
	}
}

// Someone else's load balancer has a key on the same regional endpoint, so a
// token it signed verifies cryptographically. Only the pin rejects it.
func TestALBRejectsOtherLoadBalancer(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	hdr := goodHeader(f)
	hdr["signer"] = "arn:aws:elasticloadbalancing:us-east-1:999999999999:loadbalancer/app/attacker/abcdef0123456789"
	if email, err := v.Email(f.sign(t, hdr, goodClaims())); err == nil {
		t.Fatalf("a token from an unpinned load balancer must be rejected, got %q", email)
	}
}

func TestALBRejectsAlgConfusion(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	for _, alg := range []string{"none", "None", "HS256", "RS256", ""} {
		hdr := goodHeader(f)
		hdr["alg"] = alg
		if _, err := v.Email(f.sign(t, hdr, goodClaims())); err == nil {
			t.Errorf("alg %q must be rejected", alg)
		}
	}
}

func TestALBRejectsExpired(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)

	hdr := goodHeader(f)
	hdr["exp"] = time.Now().Add(-2 * time.Hour).Unix()
	if _, err := v.Email(f.sign(t, hdr, goodClaims())); err == nil {
		t.Error("an expired header exp must be rejected")
	}

	claims := goodClaims()
	claims["exp"] = time.Now().Add(-2 * time.Hour).Unix()
	if _, err := v.Email(f.sign(t, goodHeader(f), claims)); err == nil {
		t.Error("an expired claims exp must be rejected")
	}
}

func TestALBRejectsTamperedClaims(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	token := f.sign(t, goodHeader(f), goodClaims())
	parts := strings.Split(token, ".")
	swapped, _ := json.Marshal(map[string]any{"email": "attacker@evil.example"})
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(swapped) + "." + parts[2]
	if email, err := v.Email(tampered); err == nil {
		t.Fatalf("rewriting the email must invalidate the signature, got %q", email)
	}
}

func TestALBRejectsUnknownKid(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	hdr := goodHeader(f)
	hdr["kid"] = "no-such-kid"
	if _, err := v.Email(f.sign(t, hdr, hdr)); err == nil {
		t.Error("a kid the endpoint does not serve must fail")
	}
}

func TestALBRejectsKidPathTraversal(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	for _, kid := range []string{"../../etc/passwd", "a/b", "a?b=c", "a#b", strings.Repeat("x", 200), ""} {
		hdr := goodHeader(f)
		hdr["kid"] = kid
		if _, err := v.Email(f.sign(t, hdr, goodClaims())); err == nil {
			t.Errorf("kid %q must be rejected", kid)
		}
	}
}

func TestALBRejectsMalformed(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	for _, tok := range []string{"", "a", "a.b", "a.b.c.d", "....", "!!!.???.***"} {
		if _, err := v.Email(tok); err == nil {
			t.Errorf("malformed token %q must be rejected", tok)
		}
	}
}

func TestALBRejectsTokenWithNoEmail(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	if _, err := v.Email(f.sign(t, goodHeader(f), map[string]any{"sub": "12345"})); err == nil {
		t.Error("a token with no email-like claim must be rejected")
	}
}

func TestALBFallsBackToUsernameClaims(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)
	got, err := v.Email(f.sign(t, goodHeader(f), map[string]any{"preferred_username": "person@example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "person@example.com" {
		t.Errorf("got %q", got)
	}
}

func TestNewALBVerifierRejectsBadARN(t *testing.T) {
	for _, arn := range []string{
		"",
		"not-an-arn",
		"arn:aws:s3:::bucket",
		"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/tg/abc",
		"arn:aws:elasticloadbalancing:us-east-1:12345:loadbalancer/app/x/abc", // short account
		"arn:aws:elasticloadbalancing::123456789012:loadbalancer/app/x/abc",   // no region
	} {
		if _, err := NewALBVerifier(arn); err == nil {
			t.Errorf("ARN %q must be rejected", arn)
		}
	}
}

func TestNewALBVerifierDerivesRegion(t *testing.T) {
	v, err := NewALBVerifier("arn:aws:elasticloadbalancing:eu-west-2:123456789012:loadbalancer/app/x/0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if v.region != "eu-west-2" {
		t.Errorf("region %q", v.region)
	}
	want := "https://public-keys.auth.elb.eu-west-2.amazonaws.com/"
	if v.endpoint != want {
		t.Errorf("endpoint %q, want %q", v.endpoint, want)
	}
}

func TestNilALBVerifierTrustsNothing(t *testing.T) {
	var v *ALBVerifier
	if _, err := v.Email("anything"); err == nil {
		t.Error("a nil verifier must verify nothing")
	}
	if v.SignerARN() != "" {
		t.Error("nil verifier should report no signer")
	}
}

// The key is fetched once and reused, so a token flood cannot turn into a
// request flood against the AWS endpoint.
func TestALBCachesPublicKey(t *testing.T) {
	f := newALBFixture(t)
	var fetches int
	inner := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		inner.ServeHTTP(w, r)
	})
	v := f.verifier(t)
	for i := 0; i < 5; i++ {
		if _, err := v.Email(f.sign(t, goodHeader(f), goodClaims())); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 {
		t.Errorf("expected 1 key fetch, got %d", fetches)
	}
}

func TestALBRejectsNonPEMKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "this is not a pem key")
	}))
	defer srv.Close()
	f := newALBFixture(t)
	v := f.verifier(t)
	v.endpoint = srv.URL + "/"
	if _, err := v.Email(f.sign(t, goodHeader(f), goodClaims())); err == nil {
		t.Error("a non-PEM key response must fail verification")
	}
}

// RFC 7515 forbids padding but AWS ALB has been observed to emit it, and the
// implementation this replaced handled that explicitly. A padded token must
// still verify, and the signature must be checked over the segments as
// received rather than over a normalised copy.
func TestALBAcceptsPaddedSegments(t *testing.T) {
	f := newALBFixture(t)
	v := f.verifier(t)

	h, _ := json.Marshal(goodHeader(f))
	c, _ := json.Marshal(goodClaims())
	padded := base64.URLEncoding.EncodeToString(h) + "." + base64.URLEncoding.EncodeToString(c)
	if !strings.Contains(padded, "=") {
		t.Skip("fixture happened not to need padding")
	}
	digest := sha256.Sum256([]byte(padded))
	r, s, err := ecdsa.Sign(rand.Reader, f.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	email, err := v.Email(padded + "." + base64.RawURLEncoding.EncodeToString(sig))
	if err != nil {
		t.Fatalf("a padded token must verify: %v", err)
	}
	if email != "owner@example.com" {
		t.Errorf("got %q", email)
	}
}
