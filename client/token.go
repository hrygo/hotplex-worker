package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenGenerator creates ES256 JWT tokens for gateway authentication.
type TokenGenerator struct {
	privateKey *ecdsa.PrivateKey
	issuer     string
	audience   string
	botID      string
}

// NewTokenGenerator creates a TokenGenerator from an ECDSA P-256 private key.
// The key can be provided as:
//   - A PEM-encoded private key file (path or HOTPLEX_SIGNING_KEY env var value)
//   - A 64-character hex string (32 raw bytes, hex-decoded)
//   - A 44-character base64 string (32 raw bytes)
func NewTokenGenerator(keyOrPath string) (*TokenGenerator, error) {
	if keyOrPath == "" {
		return nil, errors.New("token: signing key is empty")
	}
	key, err := loadKey(keyOrPath)
	if err != nil {
		return nil, err
	}
	return &TokenGenerator{privateKey: key, issuer: "hotplex-worker", audience: "gateway"}, nil
}

// WithAudience sets a custom JWT audience (default "gateway").
func (g *TokenGenerator) WithAudience(aud string) *TokenGenerator {
	g.audience = aud
	return g
}

// WithBotID sets a custom Bot ID claim for isolation (default "").
func (g *TokenGenerator) WithBotID(id string) *TokenGenerator {
	g.botID = id
	return g
}

// Generate creates a JWT token for the given subject and scopes.
func (g *TokenGenerator) Generate(subject string, scopes []string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":    g.issuer,
		"sub":    subject,
		"aud":    g.audience,
		"exp":    now.Add(ttl).Unix(),
		"iat":    now.Unix(),
		"nbf":    now.Unix(),
		"jti":    uuid.NewString(),
		"scopes": scopes,
	}
	if g.botID != "" {
		claims["bot_id"] = g.botID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(g.privateKey)
}

func loadKey(keyOrPath string) (*ecdsa.PrivateKey, error) {
	// Try as PEM file.
	if data, err := os.ReadFile(keyOrPath); err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			key, err := jwt.ParseECPrivateKeyFromPEM(block.Bytes)
			if err == nil {
				return key, nil
			}
		}
	}

	// Try as 64-char hex string (32 raw bytes, hex-decoded).
	if len(keyOrPath) == 64 {
		decoded, err := hex.DecodeString(keyOrPath)
		if err == nil {
			return deriveECDSAP256Key(decoded), nil
		}
	}

	// Try as raw 32-byte base64 (URL-safe or standard).
	if decoded, err := base64.URLEncoding.DecodeString(keyOrPath); err == nil && len(decoded) == 32 {
		return deriveECDSAP256Key(decoded), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(keyOrPath); err == nil && len(decoded) == 32 {
		return deriveECDSAP256Key(decoded), nil
	}

	return nil, fmt.Errorf("token: unrecognized key format (expected PEM file, 64-char hex, or 44-char base64): %q", keyOrPath)
}

// deriveECDSAP256Key mirrors internal/security/jwt.go:161-171.
func deriveECDSAP256Key(seed []byte) *ecdsa.PrivateKey {
	var scalarBytes [32]byte
	copy(scalarBytes[:], seed)
	N := elliptic.P256().Params().N
	s := new(big.Int).SetBytes(scalarBytes[:])
	s.Mod(s, new(big.Int).Sub(N, big.NewInt(1)))
	s.Add(s, big.NewInt(1))
	x, y := elliptic.P256().ScalarBaseMult(s.Bytes()) //nolint:staticcheck // SA1019: must use deprecated scalar multiplication for deterministic ECDSA key derivation from seed
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D:         s,
	}
}
