package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// SUTokenClaims represents the claims embedded in a super-user token.
type SUTokenClaims struct {
	Role string `json:"role"` // "su" for super user
	IAT  int64  `json:"iat"`  // issued at (unix seconds)
	ISS  string `json:"iss"`  // issuer
}

// SUManager manages Ed25519 keys and the one-time SU token.
type SUManager struct {
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey

	suToken string
	once    sync.Once
	mu      sync.RWMutex
}

// NewSUManager creates a new SUManager with a freshly generated Ed25519 key pair.
func NewSUManager() *SUManager {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		// This should never happen in practice; panic is acceptable for keygen failure.
		panic("failed to generate Ed25519 key pair: " + err.Error())
	}
	return &SUManager{
		privKey: priv,
		pubKey:  pub,
	}
}

// GenerateSUToken generates the SU token exactly once.
// On first call: creates a signed token, stores it, and returns it.
// On subsequent calls: returns an error indicating the token already exists.
func (m *SUManager) GenerateSUToken() (string, error) {
	var token string
	var genErr error

	m.once.Do(func() {
		claims := SUTokenClaims{
			Role: "su",
			IAT:  time.Now().UTC().Unix(),
			ISS:  "orchestrator",
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			genErr = err
			return
		}

		sig := ed25519.Sign(m.privKey, payload)
		token = encodeToken(payload, sig)

		m.mu.Lock()
		m.suToken = token
		m.mu.Unlock()
	})

	if genErr != nil {
		return "", genErr
	}
	if token == "" {
		return "", errors.New("SU token already generated")
	}
	return token, nil
}

// GetSUToken returns the stored SU token if it has been generated.
// Returns the token and true if present, or empty string and false otherwise.
func (m *SUManager) GetSUToken() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.suToken == "" {
		return "", false
	}
	return m.suToken, true
}

// ValidateToken verifies a token's signature and parses its claims.
// Returns the claims and nil on success, or nil claims and an error on failure.
// Use this only for SU token validation (it parses claims).
func (m *SUManager) ValidateToken(token string) (*SUTokenClaims, error) {
	payload, sig, err := decodeToken(token)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(m.pubKey, payload, sig) {
		return nil, errors.New("invalid signature")
	}
	var claims SUTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// ValidateTokenSignature verifies only the signature of a token.
// It does NOT parse claims. Use this for opaque client/agent tokens.
// Returns nil if valid, error otherwise.
func (m *SUManager) ValidateTokenSignature(token string) error {
	payload, sig, err := decodeToken(token)
	if err != nil {
		return err
	}
	if !ed25519.Verify(m.pubKey, payload, sig) {
		return errors.New("invalid signature")
	}
	return nil
}

// IsSUToken returns true if the token is valid AND its claims indicate role="su".
// This is the only place claims are parsed for authorization decisions.
func (m *SUManager) IsSUToken(token string) bool {
	claims, err := m.ValidateToken(token)
	if err != nil {
		return false
	}
	return claims.Role == "su"
}

// GenerateDefaultToken creates a signed default/guest token.
// The token is opaque — identity is the token string itself, not parsed from claims.
// Use this for clients that request a token without providing one.
func (m *SUManager) GenerateDefaultToken() (string, error) {
	// Minimal payload — orchestrator never parses this for identity.
	// Identity is the token string.
	payload := []byte(`{"role":"guest","iss":"orchestrator"}`)
	sig := ed25519.Sign(m.privKey, payload)
	return encodeToken(payload, sig), nil
}

// payloadPart extracts the base64url payload portion (before first dot) as raw bytes.
func payloadPart(token string) []byte {
	if i := indexByte(token, '.'); i >= 0 {
		b, _ := base64.RawURLEncoding.DecodeString(token[:i])
		return b
	}
	return nil
}

// encodeToken returns base64url(payload) + "." + base64url(signature).
func encodeToken(payload, sig []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// decodeToken splits "payload.sig" and base64url-decodes both parts.
func decodeToken(token string) (payload, sig []byte, err error) {
	parts := split2(token, ".")
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid token format")
	}
	payload, err = base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, nil, errors.New("invalid payload encoding")
	}
	sig, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, errors.New("invalid signature encoding")
	}
	return payload, sig, nil
}

// split2 splits s on the first occurrence of sep and returns at most 2 parts.
func split2(s, sep string) []string {
	if i := indexByte(s, sep[0]); i >= 0 {
		return []string{s[:i], s[i+1:]}
	}
	return []string{s}
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
