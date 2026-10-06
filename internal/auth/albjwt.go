package auth

// Verification of the OIDC token an AWS Application Load Balancer attaches to
// authenticated requests (the x-amzn-oidc-data header).
//
// Why this exists: the server previously read the email claim out of that token
// without checking its signature, on the reasoning that "the auth proxy already
// verified the token". That is only true of a token the proxy actually issued.
// Anything that can reach the listener can send a header of its own, and a
// hand-made token with any email in it was accepted — which handed full owner
// authority to any caller.
//
// Two things make verification safe rather than decorative:
//
//   - The signature is checked against the key the ALB names in the token's own
//     kid, fetched from the regional public-key endpoint.
//   - The signer is pinned. That endpoint serves keys for *every* load balancer
//     in the region, so without pinning, anyone with their own ALB could mint a
//     token that verifies perfectly. SignerARN must match exactly.

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// keyTTL bounds how long a fetched ALB public key is reused. The keys rotate,
// and a stale key shows up as a verification failure rather than a false accept.
const keyTTL = 12 * time.Hour

// maxKeyBytes caps the public-key response; a PEM EC key is a few hundred bytes.
const maxKeyBytes = 8 << 10

// clockSkew tolerates small clock differences when checking expiry.
const clockSkew = 60 * time.Second

// signerARNRe matches an ALB ARN and captures the region, which is where the
// matching public key is served from. Taking the region from the pinned ARN
// rather than from the token keeps an attacker from redirecting the key fetch.
var signerARNRe = regexp.MustCompile(`^arn:aws[a-z-]*:elasticloadbalancing:([a-z0-9-]+):\d{12}:loadbalancer/app/[^/]+/[0-9a-f]+$`)

// kidRe constrains the key id before it is placed in a URL path.
var kidRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type cachedKey struct {
	key     *ecdsa.PublicKey
	fetched time.Time
}

// ALBVerifier verifies tokens from one pinned load balancer.
type ALBVerifier struct {
	signerARN string
	region    string
	endpoint  string // overridable in tests
	client    *http.Client

	mu   sync.Mutex
	keys map[string]cachedKey
}

// NewALBVerifier returns a verifier pinned to signerARN, or an error if the ARN
// is not a well-formed ALB ARN. A nil verifier verifies nothing, so a caller
// that cannot build one must treat every token as untrusted.
func NewALBVerifier(signerARN string) (*ALBVerifier, error) {
	signerARN = strings.TrimSpace(signerARN)
	m := signerARNRe.FindStringSubmatch(signerARN)
	if m == nil {
		return nil, fmt.Errorf("not a valid ALB ARN: %q", signerARN)
	}
	region := m[1]
	return &ALBVerifier{
		signerARN: signerARN,
		region:    region,
		endpoint:  "https://public-keys.auth.elb." + region + ".amazonaws.com/",
		client:    &http.Client{Timeout: 5 * time.Second},
		keys:      map[string]cachedKey{},
	}, nil
}

// SignerARN reports the pinned load balancer, for diagnostics.
func (v *ALBVerifier) SignerARN() string {
	if v == nil {
		return ""
	}
	return v.signerARN
}

type albHeader struct {
	Alg    string `json:"alg"`
	Kid    string `json:"kid"`
	Signer string `json:"signer"`
	Iss    string `json:"iss"`
	Exp    int64  `json:"exp"`
}

type albClaims struct {
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Username          string `json:"username"`
	Sub               string `json:"sub"`
	Exp               int64  `json:"exp"`
}

// Email verifies the token and returns its email claim. Every failure path
// returns an error rather than a best-effort identity: an unverified token must
// be indistinguishable from no token at all.
func (v *ALBVerifier) Email(token string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("no verifier configured")
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed token")
	}

	rawHeader, err := b64urlDecode(parts[0])
	if err != nil {
		return "", fmt.Errorf("bad header encoding: %w", err)
	}
	var hdr albHeader
	if err := json.Unmarshal(rawHeader, &hdr); err != nil {
		return "", fmt.Errorf("bad header JSON: %w", err)
	}

	// Reject algorithm confusion outright. ALB signs with ES256; accepting
	// anything else — above all "none" or an HMAC alg — would let a caller pick
	// a scheme whose "verification" it controls.
	if hdr.Alg != "ES256" {
		return "", fmt.Errorf("unexpected alg %q", hdr.Alg)
	}
	// The pin. Without it the regional key endpoint happily serves the key for
	// somebody else's load balancer and their token verifies.
	if hdr.Signer != v.signerARN {
		return "", fmt.Errorf("signer %q is not the pinned load balancer", hdr.Signer)
	}
	if !kidRe.MatchString(hdr.Kid) {
		return "", fmt.Errorf("unacceptable kid")
	}

	now := time.Now()
	if hdr.Exp > 0 && now.After(time.Unix(hdr.Exp, 0).Add(clockSkew)) {
		return "", fmt.Errorf("token expired")
	}

	pub, err := v.publicKey(hdr.Kid)
	if err != nil {
		return "", err
	}

	sig, err := b64urlDecode(parts[2])
	if err != nil {
		return "", fmt.Errorf("bad signature encoding: %w", err)
	}
	// ES256 signatures are the fixed-width concatenation r||s, not DER.
	if len(sig) != 64 {
		return "", fmt.Errorf("unexpected signature length %d", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])

	signed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, signed[:], r, s) {
		return "", fmt.Errorf("signature does not verify")
	}

	rawClaims, err := b64urlDecode(parts[1])
	if err != nil {
		return "", fmt.Errorf("bad claims encoding: %w", err)
	}
	var claims albClaims
	if err := json.Unmarshal(rawClaims, &claims); err != nil {
		return "", fmt.Errorf("bad claims JSON: %w", err)
	}
	if claims.Exp > 0 && now.After(time.Unix(claims.Exp, 0).Add(clockSkew)) {
		return "", fmt.Errorf("claims expired")
	}

	for _, candidate := range []string{claims.Email, claims.PreferredUsername, claims.Username} {
		if c := strings.TrimSpace(candidate); c != "" {
			return c, nil
		}
	}
	return "", fmt.Errorf("token carries no email claim")
}

// publicKey returns the ALB signing key for kid, fetching and caching it.
func (v *ALBVerifier) publicKey(kid string) (*ecdsa.PublicKey, error) {
	v.mu.Lock()
	if c, ok := v.keys[kid]; ok && time.Since(c.fetched) < keyTTL {
		v.mu.Unlock()
		return c.key, nil
	}
	v.mu.Unlock()

	resp, err := v.client.Get(v.endpoint + kid)
	if err != nil {
		return nil, fmt.Errorf("fetching ALB key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ALB key endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading ALB key: %w", err)
	}

	pub, err := parseECPublicKey(body)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	v.keys[kid] = cachedKey{key: pub, fetched: time.Now()}
	v.mu.Unlock()
	return pub, nil
}

// b64urlDecode decodes a JWT segment. RFC 7515 forbids padding, but AWS ALB has
// been observed to emit it, so accept both. Note that the signature is always
// verified over the segments exactly as received — never over a normalised copy
// — so tolerating padding here cannot change what was signed.
func b64urlDecode(seg string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(seg, "="))
}

func parseECPublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("ALB key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing ALB key: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("ALB key is %T, want ECDSA", parsed)
	}
	return pub, nil
}
