// Package security provides authentication and authorization for the gateway.
package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrTokenRevoked is returned when a token's jti is on the blacklist.
var ErrTokenRevoked = errors.New("security: token revoked")

// ErrInvalidAudience is returned when the JWT audience claim is invalid.
var ErrInvalidAudience = errors.New("security: invalid audience")

// JWTValidator validates and parses JWT tokens.
// Only ES256 (ECDSA P-256) signing method is accepted, per security design.
type JWTValidator struct {
	secret    any // *ecdsa.PrivateKey or []byte (raw secret)
	audience  string
	blacklist *jtiBlacklist
}

// NewJWTValidator creates a JWT validator.
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
func (v *JWTValidator) Validate(tokenString string) (*JWTClaims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, ErrUnauthorized
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		switch token.Method.Alg() {
		case "ES256":
			switch s := v.secret.(type) {
			case *ecdsa.PrivateKey:
				return s.Public(), nil
			case []byte:
				return deriveECDSAP256Key(s).Public(), nil
			default:
				return nil, fmt.Errorf("security: invalid secret type for ES256: %T", v.secret)
			}
		default:
			return nil, fmt.Errorf("security: rejected signing method: %v (only ES256 is allowed)", token.Header["alg"])
		}
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, ErrUnauthorized
	}

	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, fmt.Errorf("%w: token expired", ErrUnauthorized)
	}

	if v.audience != "" && !v.hasAudience(claims.Audience) {
		return nil, ErrInvalidAudience
	}

	if claims.ID != "" && v.blacklist.isRevoked(claims.ID) {
		return nil, ErrTokenRevoked
	}

	return claims, nil
}

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
func (v *JWTValidator) GenerateTokenWithClaims(claims *JWTClaims) (string, error) {
	var method jwt.SigningMethod
	var signingKey any
	switch secret := v.secret.(type) {
	case *ecdsa.PrivateKey:
		method = jwt.SigningMethodES256
		signingKey = secret
	case []byte:
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
func deriveECDSAP256Key(secret []byte) *ecdsa.PrivateKey {
	var scalarBytes [32]byte
	copy(scalarBytes[:], secret)
	N := elliptic.P256().Params().N
	s := new(big.Int).SetBytes(scalarBytes[:])
	s.Mod(s, new(big.Int).Sub(N, big.NewInt(1)))
	s.Add(s, big.NewInt(1))
	x, y := elliptic.P256().ScalarBaseMult(s.Bytes())
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: s}
}

// GenerateTokenWithJTI generates a token and adds its jti to the blacklist.
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
	blacklistTTL := jtiTTL
	if blacklistTTL == 0 {
		blacklistTTL = ttl * 2
		if blacklistTTL == 0 {
			blacklistTTL = 10 * time.Minute
		}
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

// ValidateAPIKey performs constant-time comparison of an API key.
func ValidateAPIKey(provided, expected string) error {
	if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

// ─── JTI Blacklist ────────────────────────────────────────────────────────────

type jtiBlacklist struct {
	entries sync.Map
	stopCh  chan struct{}
}

func newJTIBlacklist() *jtiBlacklist {
	b := &jtiBlacklist{stopCh: make(chan struct{})}
	go b.sweep(1 * time.Minute)
	return b
}

func (b *jtiBlacklist) revoke(jti string, ttl time.Duration) {
	if jti == "" {
		return
	}
	b.entries.Store(jti, time.Now().Add(ttl))
}

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

// ─── JTI Generation ────────────────────────────────────────────────────────────

// GenerateJTI generates a cryptographically secure JWT ID.
func GenerateJTI() (string, error) {
	return uuid.New().String(), nil
}

func mustGenerateJTI() string {
	jti, err := GenerateJTI()
	if err != nil {
		return "" // crypto/rand should never fail on a healthy system
	}
	return jti
}
