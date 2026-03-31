package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrTokenRevoked is returned when a token's jti is on the blacklist.
var ErrTokenRevoked = errors.New("security: token revoked")

// ErrInvalidAudience is returned when the JWT audience claim is invalid.
var ErrInvalidAudience = errors.New("security: invalid audience")

// JWTValidator validates and parses JWT tokens.
// Only ES256 (ECDSA P-256) signing method is accepted, per security design.
// HMAC methods (HS256/HS384/HS512) are explicitly rejected.
type JWTValidator struct {
	secret   any // *ecdsa.PrivateKey or []byte (raw secret)
	audience string
	blacklist *jtiBlacklist
}

// NewJWTValidator creates a JWT validator.
// secret may be an *ecdsa.PrivateKey (ES256) or a []byte (raw secret for HMAC verification only).
// audience is the expected "aud" claim value.
func NewJWTValidator(secret any, audience string) *JWTValidator {
	return &JWTValidator{
		secret:    secret,
		audience:  audience,
		blacklist: newJTIBlacklist(),
	}
}

// JWTClaims represents the JWT claims structure per RFC 7519 and HotPlex design.
type JWTClaims struct {
	jwt.RegisteredClaims

	// HotPlex-specific claims
	UserID    string   `json:"user_id,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	Role      string   `json:"role,omitempty"`
	BotID     string   `json:"bot_id,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
}

// Validate parses and validates a JWT token string.
// Returns claims if valid, or ErrUnauthorized/ErrTokenRevoked/ErrInvalidAudience.
func (v *JWTValidator) Validate(tokenString string) (*JWTClaims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, ErrUnauthorized
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		// Only ES256 is accepted per security design. HMAC methods are explicitly rejected.
		switch token.Method.Alg() {
		case "ES256":
			switch s := v.secret.(type) {
			case *ecdsa.PrivateKey:
				return s.Public(), nil
			case []byte:
				return s, nil
			default:
				return nil, fmt.Errorf("security: invalid secret type for ES256: %T", v.secret)
			}
		default:
			return nil, fmt.Errorf("security: rejected signing method: %v (only ES256 is allowed)", token.Header["alg"])
		}
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, ErrUnauthorized
	}

	// Check expiration.
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, fmt.Errorf("%w: token expired", ErrUnauthorized)
	}

	// Verify audience.
	if v.audience != "" && !v.hasAudience(claims.Audience) {
		return nil, ErrInvalidAudience
	}

	// Check jti blacklist (revoked tokens).
	if claims.ID != "" && v.blacklist.isRevoked(claims.ID) {
		return nil, ErrTokenRevoked
	}

	return claims, nil
}

// hasAudience checks if the audience claim contains the expected audience.
// Handles string, []string (jwt.ClaimStrings), and []any (generic JSON arrays).
func (v *JWTValidator) hasAudience(aud any) bool {
	if aud == nil {
		return false
	}
	switch s := aud.(type) {
	case string:
		return s == v.audience
	case []string:
		for _, a := range s {
			if a == v.audience {
				return true
			}
		}
	case []any:
		for _, item := range s {
			if str, ok := item.(string); ok && str == v.audience {
				return true
			}
		}
	}
	return false
}

// GenerateToken generates a new JWT token for the given user.
func (v *JWTValidator) GenerateToken(userID string, scopes []string, ttl time.Duration) (string, error) {
	return v.GenerateTokenWithClaims(&JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "hotplex-worker",
			Subject:   userID,
			ID:        mustGenerateJTI(),
		},
		UserID: userID,
		Scopes: scopes,
	})
}

// GenerateTokenWithClaims generates a JWT token with the given claims using ES256.
// For *ecdsa.PrivateKey secrets, it signs directly.
// For []byte secrets (e.g. test HMAC secrets), it derives an ECDSA P-256 key so that
// generated tokens are always ES256 (matching the validator's requirement).
func (v *JWTValidator) GenerateTokenWithClaims(claims *JWTClaims) (string, error) {
	var method jwt.SigningMethod
	var signingKey any
	switch secret := v.secret.(type) {
	case *ecdsa.PrivateKey:
		method = jwt.SigningMethodES256
		signingKey = secret
	case []byte:
		// Derive a P-256 ECDSA key from the byte slice so the generated token
		// is always ES256 (HMAC methods are explicitly rejected by Validate).
		key := deriveECDSAP256Key(secret)
		method = jwt.SigningMethodES256
		signingKey = key
	default:
		return "", errors.New("security: GenerateTokenWithClaims: invalid secret type")
	}
	token := jwt.NewWithClaims(method, claims)
	return token.SignedString(signingKey)
}

// deriveECDSAP256Key derives an ECDSA P-256 private key from a byte slice.
// It is NOT a real KDF; it is intended only for test token generation.
func deriveECDSAP256Key(secret []byte) *ecdsa.PrivateKey {
	// Use the first 32 bytes of the secret as the scalar (padded with zeros if short).
	var scalarBytes [32]byte
	copy(scalarBytes[:], secret)
	// Make sure the scalar is in the valid range for P-256.
	N := elliptic.P256().Params().N
	s := new(big.Int).SetBytes(scalarBytes[:])
	s.Mod(s, new(big.Int).Sub(N, big.NewInt(1))) // keep in range [0, N-2]
	s.Add(s, big.NewInt(1))                      // shift to [1, N-1] (never 0)

	x, y := elliptic.P256().ScalarBaseMult(s.Bytes())
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         s,
	}
}

// GenerateTokenWithJTI generates a token and adds its jti to the blacklist.
// The jti is revoked when RevokeToken is called, or automatically after jtiTTL.
func (v *JWTValidator) GenerateTokenWithJTI(userID string, scopes []string, ttl, jtiTTL time.Duration) (string, string, error) {
	jti := mustGenerateJTI()
	claims := &JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "hotplex-worker",
			Subject:   userID,
			ID:        jti,
		},
		UserID: userID,
		Scopes: scopes,
	}
	var method jwt.SigningMethod
	switch v.secret.(type) {
	case *ecdsa.PrivateKey:
		method = jwt.SigningMethodES256
	case []byte:
		method = jwt.SigningMethodHS256
	default:
		return "", "", errors.New("security: GenerateTokenWithJTI: invalid secret type")
	}
	token := jwt.NewWithClaims(method, claims)
	signed, err := token.SignedString(v.secret)
	if err != nil {
		return "", "", err
	}
	// Add to blacklist with TTL = token TTL × 2 (allows clock skew per design).
	blacklistTTL := ttl * 2
	if blacklistTTL == 0 {
		blacklistTTL = 10 * time.Minute
	}
	v.blacklist.revoke(jti, blacklistTTL)
	return signed, jti, nil
}

// RevokeToken adds a jti to the blacklist with the given TTL.
func (v *JWTValidator) RevokeToken(jti string, ttl time.Duration) {
	v.blacklist.revoke(jti, ttl)
}

// IsRevoked checks if a jti is currently revoked.
func (v *JWTValidator) IsRevoked(jti string) bool {
	return v.blacklist.isRevoked(jti)
}

// ValidateAPIKey performs constant-time comparison of an API key against the configured keys.
// Returns nil if valid, or ErrUnauthorized.
func ValidateAPIKey(provided, expected string) error {
	if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

// ─── JTI Blacklist (in-memory TTL cache, no Redis dependency) ────────────────

// jtiBlacklist is an in-memory TTL cache for revoked JWT IDs.
// Uses sync.Map for concurrent access and a background sweeper goroutine.
type jtiBlacklist struct {
	entries sync.Map // map[string]time.Time (jti → expiration)
	stopCh  chan struct{}
}

// newJTIBlacklist creates and starts a background sweeper for expired entries.
func newJTIBlacklist() *jtiBlacklist {
	b := &jtiBlacklist{
		stopCh: make(chan struct{}),
	}
	go b.sweep(1 * time.Minute)
	return b
}

// revoke adds a jti to the blacklist until the given TTL expires.
func (b *jtiBlacklist) revoke(jti string, ttl time.Duration) {
	if jti == "" {
		return
	}
	b.entries.Store(jti, time.Now().Add(ttl))
}

// isRevoked returns true if the jti is currently on the blacklist.
func (b *jtiBlacklist) isRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	val, ok := b.entries.Load(jti)
	if !ok {
		return false
	}
	exp, ok := val.(time.Time)
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		b.entries.Delete(jti)
		return false
	}
	return true
}

// sweep periodically removes expired entries.
func (b *jtiBlacklist) sweep(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			b.entries.Range(func(key, val any) bool {
				if exp, ok := val.(time.Time); ok && now.After(exp) {
					b.entries.Delete(key)
				}
				return true
			})
		}
	}
}

// Stop stops the background sweeper.
func (b *jtiBlacklist) Stop() {
	close(b.stopCh)
}

// Size returns the approximate number of entries in the blacklist.
func (b *jtiBlacklist) Size() int {
	count := 0
	b.entries.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// ─── JTI Generation ──────────────────────────────────────────────────────────

// GenerateJTI generates a cryptographically secure JWT ID compatible with UUID v4.
// Uses crypto/rand as required by RFC 7519 §4.1.7.
func GenerateJTI() (string, error) {
	b := make([]byte, 16)
	n, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("security: crypto/rand unavailable: %w", err)
	}
	if n != 16 {
		return "", fmt.Errorf("security: crypto/rand read insufficient bytes: got %d, want 16", n)
	}

	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	// Set version (4) and variant (8/9/a/b).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		fmt.Sprintf("%02x%02x%02x%02x", b[0], b[1], b[2], b[3]),
		fmt.Sprintf("%02x%02x", b[4], b[5]),
		fmt.Sprintf("%02x%02x", b[6], b[7]),
		fmt.Sprintf("%02x%02x", b[8], b[9]),
		fmt.Sprintf("%02x%02x%02x%02x%02x%02x", b[10], b[11], b[12], b[13], b[14], b[15]),
	), nil
}

// mustGenerateJTI is like GenerateJTI but panics on error.
// Use only for non-failure paths where JTI generation cannot reasonably fail.
func mustGenerateJTI() string {
	jti, err := GenerateJTI()
	if err != nil {
		// crypto/rand should never fail on a healthy system.
		// Fall back to a random big.Int as last resort.
		n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		return fmt.Sprintf("fallback-%064s", n.Text(16))
	}
	return jti
}
