// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

// apiKeyQueryParam is the query parameter name for browser-based WebSocket clients
// that cannot send custom headers (CORS restrictions).
const apiKeyQueryParam = "api_key"

// Authenticator validates API keys and user credentials.
type Authenticator struct {
	mu           sync.RWMutex
	cfg          *config.SecurityConfig
	validKey     map[string]bool // set of valid API keys (hashed in production)
	jwtValidator *JWTValidator   // optional; set when JWT botID extraction is needed at HTTP level
}

// NewAuthenticator creates a new authenticator. jwtValidator may be nil.
func NewAuthenticator(cfg *config.SecurityConfig, jwtValidator *JWTValidator) *Authenticator {
	validKey := make(map[string]bool)
	for _, k := range cfg.APIKeys {
		validKey[k] = true
	}
	return &Authenticator{
		cfg:          cfg,
		validKey:     validKey,
		jwtValidator: jwtValidator,
	}
}

// ErrUnauthorized is returned when authentication fails.
var ErrUnauthorized = errors.New("security: unauthorized")

// AuthenticateRequest validates the request's API key.
// Returns the user ID, bot ID (from JWT BotID claim), and any error.
// botID may be empty when no JWT Bearer token is present.
func (a *Authenticator) AuthenticateRequest(r *http.Request) (string, string, error) {
	a.mu.RLock()
	header := a.cfg.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}

	// Check header first, then query param (for browser WebSocket clients).
	key := r.Header.Get(header)
	if key == "" {
		key = r.URL.Query().Get(apiKeyQueryParam)
	}
	if key == "" {
		a.mu.RUnlock()
		return "", "", ErrUnauthorized
	}

	// Key lookup under RLock; map lookup is not constant-time
	// but acceptable for API keys (small set, low timing sensitivity).
	defer a.mu.RUnlock()

	if len(a.validKey) == 0 {
		// No keys configured — allow all (dev mode).
		return "anonymous", a.BotIDFromRequest(r), nil
	}

	if !a.validKey[key] {
		return "", "", ErrUnauthorized
	}

	return "api_user", a.BotIDFromRequest(r), nil
}

// ReloadKeys dynamically replaces the set of valid API keys.
func (a *Authenticator) ReloadKeys(cfg *config.SecurityConfig) {
	validKey := make(map[string]bool)
	for _, k := range cfg.APIKeys {
		validKey[k] = true
	}
	a.mu.Lock()
	a.cfg = cfg
	a.validKey = validKey
	a.mu.Unlock()
}

// ExtractAPIKey returns the API key from header or query param.
// Returns ("", false) if no key found, (key, true) if found (not yet validated).
func (a *Authenticator) ExtractAPIKey(r *http.Request) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	header := a.cfg.APIKeyHeader
	if header == "" {
		header = "X-API-Key"
	}

	key := r.Header.Get(header)
	if key == "" {
		key = r.URL.Query().Get(apiKeyQueryParam)
	}
	if key == "" {
		return "", false
	}
	return key, true
}

// AuthenticateKey validates an API key string directly.
// Returns userID if valid, ("", false) if invalid.
// Handles dev mode (no keys configured → "anonymous").
func (a *Authenticator) AuthenticateKey(key string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.validKey) == 0 {
		// No keys configured — allow all (dev mode).
		return "anonymous", true
	}

	if !a.validKey[key] {
		return "", false
	}
	return "api_user", true
}

// BotIDFromRequest extracts the BotID claim from a JWT Bearer token in the Authorization header.
// Returns "" if no token is present or if extraction fails (fail-open).
func (a *Authenticator) BotIDFromRequest(r *http.Request) string {
	if a.jwtValidator == nil {
		return ""
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	tokenString := strings.TrimPrefix(auth, "Bearer ")
	if tokenString == "" {
		return ""
	}
	// SECURITY: Verify the token signature before extracting botID.
	// We use the same ES256 validation as the full JWT check, but silently
	// ignore errors (fail-open) since the API key is the primary auth gate.
	claims, err := a.jwtValidator.Validate(tokenString)
	if err != nil {
		return ""
	}
	return claims.BotID
}

// Middleware returns an HTTP middleware that enforces authentication.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := a.AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Claims holds authenticated user information attached to a context.
type Claims struct {
	UserID string
	APIKey string
}

type contextKey string

const claimsKey contextKey = "security.claims"

// WithClaims attaches Claims to a context.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFrom extracts Claims from a context.
func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}
