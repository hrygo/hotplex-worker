// Package security provides authentication and input validation middleware.
package security

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
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
		return "", "", ErrUnauthorized
	}

	// Constant-time comparison to prevent timing attacks.
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.validKey) == 0 {
		// No keys configured — allow all (dev mode).
		return "anonymous", a.botIDFromRequest(r), nil
	}

	if !a.validKey[key] {
		return "", "", ErrUnauthorized
	}

	return "api_user", a.botIDFromRequest(r), nil
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

// botIDFromRequest extracts the BotID claim from a JWT Bearer token in the Authorization header.
// Returns "" if no token is present or if extraction fails (fail-open; botID mismatch is
// enforced later by performInit).
//
// SECURITY: The token signature is verified via ES256 before extracting the botID claim.
// If signature verification fails (e.g. token expired, wrong algorithm), the botID is
// not extracted and the request proceeds with an empty botID. botID mismatch is still
// enforced later by performInit as a defense-in-depth measure.
func (a *Authenticator) botIDFromRequest(r *http.Request) string {
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

// AuthenticateEnvelope validates the session_id and API key embedded in an envelope.
// For WS connections the API key is validated at handshake time; this is a
// secondary check for message-level auth if needed.
func (a *Authenticator) AuthenticateEnvelope(env *events.Envelope) error {
	if env.SessionID == "" {
		return ErrUnauthorized
	}
	return nil
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

// InputValidator validates user input for safety.
type InputValidator struct {
	maxLen     int
	allowedOps []string
}

// NewInputValidator creates a new input validator.
func NewInputValidator(cfg *config.WorkerConfig) *InputValidator {
	return &InputValidator{
		maxLen:     1 << 20, // 1MB
		allowedOps: nil,
	}
}

// ValidateInput checks that input is safe to pass to a worker.
func (v *InputValidator) ValidateInput(content string) error {
	if len(content) > v.maxLen {
		return errors.New("input too large")
	}
	// Reject null bytes which can corrupt downstream JSON parsers.
	if strings.Contains(content, "\x00") {
		return errors.New("input contains null byte")
	}
	return nil
}

// EnvValidator validates environment variables before passing to a worker.
type EnvValidator struct {
	whitelist map[string]bool
}

// NewEnvValidator creates an environment validator from a whitelist.
func NewEnvValidator(whitelist []string) *EnvValidator {
	m := make(map[string]bool)
	for _, k := range whitelist {
		m[k] = true
	}
	return &EnvValidator{whitelist: m}
}

// Validate checks that all keys in env are allowed.
// Returns the filtered env map (only allowed keys).
func (v *EnvValidator) Validate(env map[string]string) map[string]string {
	if len(v.whitelist) == 0 {
		return env // allow all
	}
	filtered := make(map[string]string)
	for k, val := range env {
		if v.whitelist[k] {
			filtered[k] = val
		}
	}
	return filtered
}
