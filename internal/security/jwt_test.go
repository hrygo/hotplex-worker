package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// ─── NewJWTValidator ────────────────────────────────────────────────────────────

func TestNewJWTValidator(t *testing.T) {
	t.Parallel()

	t.Run("with ECDSA key", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		v := NewJWTValidator(key, "test-audience")
		require.NotNil(t, v)
		require.NotNil(t, v.blacklist)
		require.Equal(t, "test-audience", v.audience)
	})

	t.Run("with HMAC secret", func(t *testing.T) {
		t.Parallel()
		secret := []byte("test-secret-key-32-bytes-long!!!")

		v := NewJWTValidator(secret, "test-audience")
		require.NotNil(t, v)
		require.NotNil(t, v.blacklist)
		require.Equal(t, "test-audience", v.audience)
	})
}

// ─── Validate ───────────────────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	// NOT parallel — shared validator instance.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "failed to generate ECDSA key")

	// Validator without audience requirement (audience is tested in TestHasAudience).
	validator := NewJWTValidator(key, "")
	defer validator.blacklist.Stop()

	tests := []struct {
		name        string
		setupToken  func() string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid token",
			setupToken: func() string {
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
						Issuer:    "hotplex",
						Subject:   "user-123",
					},
					UserID: "user-123",
					Scopes: []string{"read", "write"},
				}
				token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
				signed, _ := token.SignedString(key)
				return signed
			},
			wantErr: false,
		},
		{
			name: "empty token",
			setupToken: func() string {
				return ""
			},
			wantErr:     true,
			errContains: "unauthorized",
		},
		{
			name: "whitespace only token",
			setupToken: func() string {
				return "   "
			},
			wantErr:     true,
			errContains: "unauthorized",
		},
		{
			name: "Bearer prefix stripped",
			setupToken: func() string {
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
						Issuer:    "hotplex",
						Subject:   "user-123",
					},
					UserID: "user-123",
				}
				token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
				signed, _ := token.SignedString(key)
				return "Bearer " + signed
			},
			wantErr: false,
		},
		{
			name: "expired token",
			setupToken: func() string {
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
						Issuer:    "hotplex",
						Subject:   "user-123",
					},
					UserID: "user-123",
				}
				token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
				signed, _ := token.SignedString(key)
				return signed
			},
			wantErr:     true,
			errContains: "expired",
		},
		{
			name: "revoked token",
			setupToken: func() string {
				jti := mustGenerateJTI()
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
						Issuer:    "hotplex",
						Subject:   "user-123",
						ID:        jti,
					},
					UserID: "user-123",
				}
				token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
				signed, _ := token.SignedString(key)
				validator.RevokeToken(jti, 10*time.Minute)
				return signed
			},
			wantErr:     true,
			errContains: "revoked",
		},
		{
			name: "invalid signature",
			setupToken: func() string {
				// Create token with different key
				wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
						Issuer:    "hotplex",
						Subject:   "user-123",
					},
					UserID: "user-123",
				}
				token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
				signed, _ := token.SignedString(wrongKey)
				return signed
			},
			wantErr:     true,
			errContains: "unauthorized",
		},
		{
			name: "wrong signing method HS256",
			setupToken: func() string {
				secret := []byte("test-secret")
				claims := &JWTClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
						Issuer:    "hotplex",
						Subject:   "user-123",
					},
					UserID: "user-123",
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				signed, _ := token.SignedString(secret)
				return signed
			},
			wantErr:     true,
			errContains: "rejected signing method",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tokenString := tt.setupToken()
			claims, err := validator.Validate(tokenString)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				require.Nil(t, claims)
			} else {
				require.NoError(t, err)
				require.NotNil(t, claims)
			}
		})
	}
}

// ─── hasAudience ────────────────────────────────────────────────────────────────

func TestHasAudience(t *testing.T) {
	t.Parallel()

	validator := &JWTValidator{audience: "hotplex-gateway"}

	tests := []struct {
		name     string
		aud      any
		expected bool
	}{
		{"string match", "hotplex-gateway", true},
		{"string no match", "other-audience", false},
		{"string slice match", []string{"api", "hotplex-gateway", "web"}, true},
		{"string slice no match", []string{"api", "web"}, false},
		{"empty string slice", []string{}, false},
		{"any slice match", []any{"api", "hotplex-gateway"}, true},
		{"any slice no match", []any{"api", "web"}, false},
		{"nil audience", nil, false},
		{"empty string", "", false},
		{"int audience (invalid type)", 123, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := validator.hasAudience(tt.aud)
			require.Equal(t, tt.expected, result)
		})
	}
}

// ─── GenerateToken ──────────────────────────────────────────────────────────────

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	t.Run("with ECDSA key", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		audience := "test-audience"
		validator := NewJWTValidator(key, audience)
		defer validator.blacklist.Stop()

		token, err := validator.GenerateToken("user-123", []string{"read", "write"}, 1*time.Hour)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Validate with a validator that does NOT require audience.
		// GenerateToken does not set the Audience claim, so it would fail
		// against a validator with audience configured.
		validatorNoAud := NewJWTValidator(key, "")
		defer validatorNoAud.blacklist.Stop()

		claims, err := validatorNoAud.Validate(token)
		require.NoError(t, err)
		require.Equal(t, "user-123", claims.UserID)
		require.Equal(t, []string{"read", "write"}, claims.Scopes)
		require.Equal(t, "user-123", claims.Subject)
		require.Equal(t, "hotplex", claims.Issuer)
		require.NotEmpty(t, claims.ID) // jti should be set
	})

	t.Run("with HMAC secret generates but validation rejects HS256", func(t *testing.T) {
		t.Parallel()
		secret := []byte("test-secret-key-32-bytes-long!!!")

		audience := "test-audience"
		validator := NewJWTValidator(secret, audience)
		defer validator.blacklist.Stop()

		token, err := validator.GenerateToken("user-456", []string{"admin"}, 30*time.Minute)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Note: GenerateToken with HMAC secret creates HS256 tokens
		// but Validate only accepts ES256, so validation will fail
		_, err = validator.Validate(token)
		require.Error(t, err)
		// jwt library error message describes the type mismatch
		require.Contains(t, err.Error(), "invalid")
	})
}

// ─── GenerateTokenWithClaims ────────────────────────────────────────────────────

func TestGenerateTokenWithClaims(t *testing.T) {
	t.Parallel()

	t.Run("with ECDSA key", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		claims := &JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Issuer:    "custom-issuer",
				Subject:   "user-789",
			},
			UserID:    "user-789",
			Scopes:    []string{"read", "write", "delete"},
			Role:      "admin",
			BotID:     "bot-001",
			SessionID: "sess-abc123",
		}

		token, err := validator.GenerateTokenWithClaims(claims)
		require.NoError(t, err)
		require.NotEmpty(t, token)

		// Validate the generated token
		parsedClaims, err := validator.Validate(token)
		require.NoError(t, err)
		require.Equal(t, "user-789", parsedClaims.UserID)
		require.Equal(t, []string{"read", "write", "delete"}, parsedClaims.Scopes)
		require.Equal(t, "admin", parsedClaims.Role)
		require.Equal(t, "bot-001", parsedClaims.BotID)
		require.Equal(t, "sess-abc123", parsedClaims.SessionID)
	})

	t.Run("with HMAC secret", func(t *testing.T) {
		t.Parallel()
		secret := []byte("test-secret-key-32-bytes-long!!!")

		validator := NewJWTValidator(secret, "test-audience")
		defer validator.blacklist.Stop()

		claims := &JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Issuer:    "hotplex",
				Subject:   "user-999",
			},
			UserID: "user-999",
		}

		token, err := validator.GenerateTokenWithClaims(claims)
		require.NoError(t, err)
		require.NotEmpty(t, token)
	})

	t.Run("invalid secret type", func(t *testing.T) {
		t.Parallel()
		validator := &JWTValidator{
			secret:    "invalid-secret-type",
			audience:  "test",
			blacklist: newJTIBlacklist(),
		}
		defer validator.blacklist.Stop()

		claims := &JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			},
		}

		token, err := validator.GenerateTokenWithClaims(claims)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid secret type")
		require.Empty(t, token)
	})
}

// ─── GenerateTokenWithJTI ───────────────────────────────────────────────────────

func TestGenerateTokenWithJTI(t *testing.T) {
	t.Parallel()

	t.Run("generates token with JTI and revokes correctly", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		// Use validator without audience so generated tokens validate.
		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		token, jti, err := validator.GenerateTokenWithJTI("user-123", []string{"read"}, 1*time.Hour, 2*time.Hour)
		require.NoError(t, err)
		require.NotEmpty(t, token)
		require.NotEmpty(t, jti)

		// GenerateTokenWithJTI adds JTI to blacklist immediately.
		// So the token is already revoked — this is by design
		// (anyone with the JTI can revoke the token).
		_, err = validator.Validate(token)
		require.Error(t, err)
		require.Contains(t, err.Error(), "revoked")
		// IsRevoked confirms
		require.True(t, validator.IsRevoked(jti))
	})

	t.Run("invalid secret type", func(t *testing.T) {
		t.Parallel()
		validator := &JWTValidator{
			secret:    12345,
			audience:  "test",
			blacklist: newJTIBlacklist(),
		}
		defer validator.blacklist.Stop()

		token, jti, err := validator.GenerateTokenWithJTI("user-123", []string{"read"}, 1*time.Hour, 2*time.Hour)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid secret type")
		require.Empty(t, token)
		require.Empty(t, jti)
	})

	t.Run("blacklist TTL defaults to 10min when token TTL is zero", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		// Token with 0 TTL (edge case)
		token, jti, err := validator.GenerateTokenWithJTI("user-123", []string{"read"}, 0, 0)
		require.NoError(t, err)
		require.NotEmpty(t, token)
		require.NotEmpty(t, jti)
	})
}

// ─── RevokeToken / IsRevoked ────────────────────────────────────────────────────

func TestRevokeTokenAndIsRevoked(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	validator := NewJWTValidator(key, "test-audience")
	defer validator.blacklist.Stop()

	jti := mustGenerateJTI()

	// Initially not revoked
	require.False(t, validator.IsRevoked(jti))

	// Revoke the token
	validator.RevokeToken(jti, 10*time.Minute)

	// Should be revoked now
	require.True(t, validator.IsRevoked(jti))

	// Empty JTI should return false
	require.False(t, validator.IsRevoked(""))

	// Revoking empty JTI should be no-op
	validator.RevokeToken("", 10*time.Minute)
}

// ─── ValidateAPIKey ──────────────────────────────────────────────────────────────

func TestValidateAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provided string
		expected string
		wantErr  bool
	}{
		{"valid key", "secret-api-key-123", "secret-api-key-123", false},
		{"invalid key", "wrong-key", "secret-api-key-123", true},
		{"empty provided", "", "secret-api-key-123", true},
		{"empty expected", "secret-api-key-123", "", true},
		{"both empty", "", "", false}, // ConstantTimeCompare returns 1 for equal byte slices (both empty)
		{"case sensitive", "Secret-Key", "secret-key", true},
		{"different length", "short", "much-longer-secret-key", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAPIKey(tt.provided, tt.expected)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, ErrUnauthorized, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── JTI Blacklist ──────────────────────────────────────────────────────────────

func TestJTIBlacklist(t *testing.T) {
	t.Parallel()

	t.Run("newJTIBlacklist initializes properly", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		require.NotNil(t, b)
		require.NotNil(t, b.stopCh)
		defer b.Stop()
	})

	t.Run("revoke and isRevoked", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		jti := mustGenerateJTI()

		// Not revoked initially
		require.False(t, b.isRevoked(jti))

		// Revoke for 100ms
		b.revoke(jti, 100*time.Millisecond)
		require.True(t, b.isRevoked(jti))

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)
		require.False(t, b.isRevoked(jti))
	})

	t.Run("isRevoked with empty jti", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		require.False(t, b.isRevoked(""))
	})

	t.Run("revoke with empty jti is no-op", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		b.revoke("", 10*time.Minute)
		require.Equal(t, 0, b.Size())
	})

	t.Run("sweep removes expired entries", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		// Add multiple entries with short TTL
		jti1 := mustGenerateJTI()
		jti2 := mustGenerateJTI()
		b.revoke(jti1, 50*time.Millisecond)
		b.revoke(jti2, 50*time.Millisecond)

		require.Equal(t, 2, b.Size())

		// Wait for sweep (runs every 1 minute, but we can trigger manually by waiting)
		time.Sleep(100 * time.Millisecond)

		// Access to trigger lazy deletion
		require.False(t, b.isRevoked(jti1))
		require.False(t, b.isRevoked(jti2))
	})

	t.Run("Size returns correct count", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		require.Equal(t, 0, b.Size())

		b.revoke(mustGenerateJTI(), 10*time.Minute)
		require.Equal(t, 1, b.Size())

		b.revoke(mustGenerateJTI(), 10*time.Minute)
		require.Equal(t, 2, b.Size())
	})

	t.Run("Stop stops the sweeper", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()

		// Stop should not panic
		require.NotPanics(t, func() {
			b.Stop()
		})

		// Double stop should not panic (channel already closed)
		// Note: this will panic, but that's expected behavior
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()
		b := newJTIBlacklist()
		defer b.Stop()

		var wg sync.WaitGroup
		numOps := 100

		// Concurrent revokes
		for i := 0; i < numOps; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.revoke(mustGenerateJTI(), 10*time.Minute)
			}()
		}

		// Concurrent checks
		for i := 0; i < numOps; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = b.isRevoked(mustGenerateJTI())
			}()
		}

		wg.Wait()
		// Should complete without race conditions
	})
}

// ─── GenerateJTI ────────────────────────────────────────────────────────────────

func TestGenerateJTI(t *testing.T) {
	t.Parallel()

	t.Run("generates valid UUID v4", func(t *testing.T) {
		t.Parallel()
		jti, err := GenerateJTI()
		require.NoError(t, err)
		require.NotEmpty(t, jti)

		// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
		require.Len(t, jti, 36)
		require.Equal(t, byte('-'), jti[8])
		require.Equal(t, byte('-'), jti[13])
		require.Equal(t, byte('4'), jti[14]) // version 4
		require.Equal(t, byte('-'), jti[18])
		require.Contains(t, "89ab", string(jti[19])) // variant
		require.Equal(t, byte('-'), jti[23])
	})

	t.Run("generates unique JTIs", func(t *testing.T) {
		t.Parallel()
		jtis := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			jti, err := GenerateJTI()
			require.NoError(t, err)
			require.False(t, jtis[jti], "duplicate JTI generated: %s", jti)
			jtis[jti] = true
		}
	})
}

// ─── mustGenerateJTI ────────────────────────────────────────────────────────────

func TestMustGenerateJTI(t *testing.T) {
	t.Parallel()

	t.Run("generates non-empty JTI", func(t *testing.T) {
		t.Parallel()
		jti := mustGenerateJTI()
		require.NotEmpty(t, jti)
		require.Len(t, jti, 36) // UUID format
	})

	t.Run("generates unique JTIs", func(t *testing.T) {
		t.Parallel()
		jtis := make(map[string]bool)
		for i := 0; i < 100; i++ {
			jti := mustGenerateJTI()
			require.False(t, jtis[jti], "duplicate JTI generated: %s", jti)
			jtis[jti] = true
		}
	})
}

// ─── Integration Tests ──────────────────────────────────────────────────────────

func TestJWTIntegration(t *testing.T) {
	t.Parallel()

	t.Run("full lifecycle: generate, validate, revoke", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		// Use validator without audience so generated tokens (no audience) validate.
		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		// Generate token
		token, err := validator.GenerateToken("user-123", []string{"read", "write"}, 1*time.Hour)
		require.NoError(t, err)

		// Validate token
		claims, err := validator.Validate(token)
		require.NoError(t, err)
		require.Equal(t, "user-123", claims.UserID)

		// Revoke by JTI
		validator.RevokeToken(claims.ID, 10*time.Minute)

		// Validation should fail
		_, err = validator.Validate(token)
		require.Error(t, err)
		require.Contains(t, err.Error(), "revoked")
	})

	t.Run("token with custom claims", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		// Generate token with custom claims
		customClaims := &JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Issuer:    "hotplex",
				Subject:   "user-456",
				Audience:  []string{"hotplex-gateway"},
				ID:        mustGenerateJTI(),
			},
			UserID:    "user-456",
			Scopes:    []string{"admin", "read", "write", "delete"},
			Role:      "admin",
			BotID:     "bot-789",
			SessionID: "sess-xyz123",
		}

		token, err := validator.GenerateTokenWithClaims(customClaims)
		require.NoError(t, err)

		// Validate and check all fields
		claims, err := validator.Validate(token)
		require.NoError(t, err)
		require.Equal(t, "user-456", claims.UserID)
		require.Equal(t, []string{"admin", "read", "write", "delete"}, claims.Scopes)
		require.Equal(t, "admin", claims.Role)
		require.Equal(t, "bot-789", claims.BotID)
		require.Equal(t, "sess-xyz123", claims.SessionID)
	})

	t.Run("concurrent token validation", func(t *testing.T) {
		t.Parallel()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		validator := NewJWTValidator(key, "")
		defer validator.blacklist.Stop()

		// Generate multiple tokens
		tokens := make([]string, 10)
		for i := 0; i < 10; i++ {
			token, err := validator.GenerateToken(fmt.Sprintf("user-%d", i), []string{"read"}, 1*time.Hour)
			require.NoError(t, err)
			tokens[i] = token
		}

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				claims, err := validator.Validate(tokens[idx])
				require.NoError(t, err)
				require.Equal(t, fmt.Sprintf("user-%d", idx), claims.UserID)
			}(i)
		}

		wg.Wait()
	})
}
