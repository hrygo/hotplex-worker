package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		xff  string
		addr string
		want string
	}{
		{"direct", "", "1.2.3.4:8080", "1.2.3.4"},
		{"xff single", "10.0.0.1", "1.2.3.4:8080", "10.0.0.1"},
		{"xff multiple", "10.0.0.1, 10.0.0.2", "1.2.3.4:8080", "10.0.0.1"},
		{"xff trimmed", " 10.0.0.1 ", "1.2.3.4:8080", "10.0.0.1"},
		{"no port", "", "1.2.3.4", "1.2.3.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.addr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			got := clientIP(r)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		query  string
		want   string
		wantOK bool
	}{
		{"bearer token", "Bearer abc123", "", "abc123", true},
		{"bearer lowercase", "bearer abc123", "", "", false},
		{"query param", "", "access_token=xyz789", "xyz789", true},
		{"header priority", "Bearer from_header", "access_token=from_query", "from_header", true},
		{"empty", "", "", "", false},
		{"no bearer prefix", "Basic abc123", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			if tt.query != "" {
				r.URL.RawQuery = tt.query
			}
			got := extractBearerToken(r)
			if tt.wantOK {
				require.Equal(t, tt.want, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestIPAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		addr  string
		cidrs []string
		want  bool
	}{
		{"empty cidrs allows all", "1.2.3.4", nil, true},
		{"match single", "10.0.0.5", []string{"10.0.0.0/8"}, true},
		{"match multiple", "192.168.1.5", []string{"10.0.0.0/8", "192.168.0.0/16"}, true},
		{"no match", "8.8.8.8", []string{"10.0.0.0/8"}, false},
		{"invalid addr", "not-an-ip", []string{"10.0.0.0/8"}, false},
		{"invalid cidr skipped", "10.0.0.1", []string{"bad-cidr", "10.0.0.0/24"}, true},
		{"ipv6", "::1", []string{"::1/128"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ipAllowed(tt.addr, tt.cidrs)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetScopes(t *testing.T) {
	t.Parallel()
	t.Run("no scopes", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		require.Nil(t, getScopes(r))
	})

	t.Run("with scopes", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := contextWithScopes(r, []string{ScopeSessionRead, ScopeStatsRead})
		r = r.WithContext(ctx)
		require.Equal(t, []string{ScopeSessionRead, ScopeStatsRead}, getScopes(r))
	})
}

func TestHasScope(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := contextWithScopes(r, []string{ScopeSessionRead, ScopeStatsRead})
	r = r.WithContext(ctx)

	require.True(t, hasScope(r, ScopeSessionRead))
	require.True(t, hasScope(r, ScopeStatsRead))
	require.False(t, hasScope(r, ScopeSessionWrite))
}

// contextWithScopes injects scope values into the request context.
func contextWithScopes(r *http.Request, scopes []string) context.Context {
	return context.WithValue(r.Context(), scopeContextKey{}, scopes)
}
