package opencodeserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func newTestCommander(t *testing.T, handler http.HandlerFunc) (*ServerCommander, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := &ServerCommander{
		client:    ts.Client(),
		baseURL:   ts.URL,
		sessionID: "sess-test-123",
	}
	return c, ts
}

func TestServerCommanderSendControlRequestRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subtype  string
		body     map[string]any
		wantPath string
	}{
		{"context_usage routes to GET messages", "get_context_usage", nil, "/session/sess-test-123/message"},
		{"mcp_status routes to GET tools", "mcp_status", nil, "/experimental/tool"},
		{"unsupported returns error", "unsupported_subtype", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotRequest := false
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				gotRequest = true
				if tt.wantPath != "" {
					require.Contains(t, r.URL.Path, tt.wantPath)
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode([]any{})
			})

			resp, err := c.SendControlRequest(context.Background(), tt.subtype, tt.body)
			if tt.wantPath == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, gotRequest)
			require.NotNil(t, resp)
		})
	}
}

func TestServerCommanderQueryContextUsage(t *testing.T) {
	t.Parallel()

	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/session/sess-test-123/message")
		messages := []any{
			map[string]any{"info": map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 100, "output": 200, "reasoning": 50, "cache": map[string]any{"read": 30, "write": 20}},
				"model":  map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4"},
			}},
			map[string]any{"info": map[string]any{"role": "user"}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(messages)
	})

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.Equal(t, 400, resp["totalTokens"])
	require.Equal(t, "anthropic/claude-sonnet-4", resp["model"])
	require.Len(t, resp["categories"], 5)
}

func TestServerCommanderQueryContextUsageMultipleMessages(t *testing.T) {
	t.Parallel()

	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		messages := []any{
			map[string]any{"info": map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 100, "output": 200, "reasoning": 50, "cache": map[string]any{"read": 10, "write": 5}},
				"model":  map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4"},
			}},
			map[string]any{"info": map[string]any{
				"role":   "assistant",
				"tokens": map[string]any{"input": 50, "output": 100, "reasoning": 25, "cache": map[string]any{"read": 5, "write": 3}},
				"model":  map[string]any{"providerID": "openai", "modelID": "gpt-4"},
			}},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(messages)
	})

	resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
	require.NoError(t, err)
	require.Equal(t, 100+200+50+10+5+50+100+25+5+3, resp["totalTokens"])
	require.Equal(t, "openai/gpt-4", resp["model"])
	categories := resp["categories"].([]map[string]any)
	require.Len(t, categories, 5)
	require.Equal(t, 150, categories[0]["tokens"])
}

func TestServerCommanderQueryContextUsageEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		messages       []any
		expectedTokens int
		expectedModel  string
	}{
		{
			name: "no assistant messages",
			messages: []any{map[string]any{"info": map[string]any{
				"role": "user", "tokens": map[string]any{"input": 100},
			}}},
			expectedTokens: 0, expectedModel: "",
		},
		{
			name: "nil tokens skips message entirely",
			messages: []any{map[string]any{"info": map[string]any{
				"role":  "assistant",
				"model": map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4"},
			}}},
			expectedTokens: 0, expectedModel: "",
		},
		{
			name: "empty tokens object",
			messages: []any{map[string]any{"info": map[string]any{
				"role": "assistant", "tokens": map[string]any{},
			}}},
			expectedTokens: 0, expectedModel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(tt.messages)
			})
			resp, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
			require.NoError(t, err)
			require.Equal(t, tt.expectedTokens, resp["totalTokens"])
			require.Equal(t, tt.expectedModel, resp["model"])
			require.Len(t, resp["categories"], 5)
		})
	}
}

func TestServerCommanderSetModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      map[string]any
		wantP     string
		wantM     string
		wantModel string
	}{
		{"provider/model format", map[string]any{"model": "anthropic/claude-sonnet-4"}, "anthropic", "claude-sonnet-4", "claude-sonnet-4"},
		{"bare model name", map[string]any{"model": "sonnet-4"}, "", "sonnet-4", "sonnet-4"},
		{"explicit fields", map[string]any{"providerID": "openai", "modelID": "gpt-4"}, "openai", "gpt-4", "gpt-4"},
		{"providerID + model field", map[string]any{"providerID": "anthropic", "model": "openai/gpt-4"}, "anthropic", "", ""},
		{"model field only", map[string]any{"model": "claude-sonnet-4"}, "", "claude-sonnet-4", "claude-sonnet-4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
			resp, err := c.SendControlRequest(context.Background(), "set_model", tt.body)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, c.PendingModel())
			require.Equal(t, tt.wantP, c.PendingModel().ProviderID)
			require.Equal(t, tt.wantM, c.PendingModel().ModelID)
			if tt.wantModel != "" {
				require.Equal(t, tt.wantModel, resp["model"])
			}
		})
	}
}

func TestServerCommanderCompact(t *testing.T) {
	t.Parallel()
	called := false
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		require.Contains(t, r.URL.Path, "/session/sess-test-123/summarize")
		called = true
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(true)
	})
	require.NoError(t, c.Compact(context.Background(), nil))
	require.True(t, called)
}

func TestServerCommanderCompactWithPendingModel(t *testing.T) {
	t.Parallel()
	var capturedBody map[string]any
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(true)
	})
	c.SendControlRequest(context.Background(), "set_model", map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4"})
	require.NoError(t, c.Compact(context.Background(), nil))
	require.Equal(t, "anthropic", capturedBody["providerID"])
	require.Equal(t, "claude-sonnet-4", capturedBody["modelID"])
	require.Equal(t, false, capturedBody["auto"])
}

func TestServerCommanderClear(t *testing.T) {
	t.Parallel()
	deleteCalled := false
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalled = true
			require.Contains(t, r.URL.Path, "/session/sess-test-123")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/session" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-sess-456"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	require.NoError(t, c.Clear(context.Background()))
	require.True(t, deleteCalled)
	require.Equal(t, "new-sess-456", c.SessionID())
}

func TestServerCommanderClearWithErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		deleteFails bool
		createFails bool
		expectError bool
	}{
		{"delete fails", true, false, true},
		{"create fails", false, true, true},
		{"both succeed", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deleteCalled, createCalled := false, false
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deleteCalled = true
					w.WriteHeader(map[bool]int{true: http.StatusInternalServerError, false: http.StatusOK}[tt.deleteFails])
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/session" {
					createCalled = true
					if tt.createFails {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-sess-456"})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			})
			err := c.Clear(context.Background())
			if tt.expectError {
				require.Error(t, err)
				if tt.deleteFails {
					require.Contains(t, err.Error(), "opencode clear (delete)")
				}
				if tt.createFails {
					require.Contains(t, err.Error(), "opencode clear (create)")
				}
			} else {
				require.NoError(t, err)
				require.True(t, deleteCalled)
				require.True(t, createCalled)
				require.Equal(t, "new-sess-456", c.SessionID())
			}
		})
	}
}

func TestServerCommanderSessionIDAfterClear(t *testing.T) {
	t.Parallel()
	callCount := 0
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/session" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "generated-session-" + fmt.Sprint(callCount)})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	require.Equal(t, "sess-test-123", c.SessionID())
	require.NoError(t, c.Clear(context.Background()))
	require.Equal(t, "generated-session-2", c.SessionID())
}

func TestServerCommanderRewind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		targetID    string
		wantMsgID   any
		wantNoMsgID bool
		checkReq    func(*testing.T, *http.Request)
	}{
		{
			name: "with target ID", targetID: "msg-target-456", wantMsgID: "msg-target-456",
			checkReq: func(t *testing.T, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Contains(t, r.URL.Path, "/session/sess-test-123/revert")
			},
		},
		{
			name: "without target ID resolves last assistant message", targetID: "", wantMsgID: "msg-asst-999",
			checkReq: func(t *testing.T, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Contains(t, r.URL.Path, "/session/sess-test-123/revert")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var capturedBody map[string]any
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]any{
						map[string]any{"id": "msg-user-1", "info": map[string]any{"role": "user"}},
						map[string]any{"id": "msg-asst-999", "info": map[string]any{"role": "assistant"}},
					})
					return
				}
				tt.checkReq(t, r)
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedBody)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(nil)
			})
			require.NoError(t, c.Rewind(context.Background(), tt.targetID))
			if tt.wantNoMsgID {
				return
			}
			require.Equal(t, tt.wantMsgID, capturedBody["messageID"])
		})
	}
}

func TestServerCommanderSetPermissionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        string
		checkBody   func(*testing.T, map[string]any)
		wantSuccess bool
	}{
		{
			name: "bypassPermissions", mode: "bypassPermissions",
			checkBody:   nil,
			wantSuccess: true,
		},
		{
			name: "unknown mode", mode: "unknownMode",
			checkBody: func(t *testing.T, reqBody map[string]any) {
				require.Equal(t, []any{}, reqBody["permission"])
			},
			wantSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPatch, r.Method)
				called = true
				if tt.checkBody != nil {
					body, _ := io.ReadAll(r.Body)
					var reqBody map[string]any
					json.Unmarshal(body, &reqBody)
					tt.checkBody(t, reqBody)
				}
				w.WriteHeader(http.StatusOK)
			})
			resp, err := c.SendControlRequest(context.Background(), "set_permission_mode", map[string]any{"mode": tt.mode})
			require.NoError(t, err)
			require.True(t, called)
			require.Equal(t, tt.mode, resp["mode"])
			require.Equal(t, tt.wantSuccess, resp["success"])
		})
	}
}

func TestServerCommanderUpdateSessionID(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	require.Equal(t, "sess-test-123", c.SessionID())
	c.UpdateSessionID("new-session-id")
	require.Equal(t, "new-session-id", c.SessionID())
}

func TestServerCommanderMCPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolsResp   any
		checkServer func(*testing.T, []map[string]any)
	}{
		{
			name:      "with tools",
			toolsResp: []map[string]any{{"name": "filesystem"}, {"name": "web_search"}},
			checkServer: func(t *testing.T, servers []map[string]any) {
				require.Len(t, servers, 2)
				require.Equal(t, "filesystem", servers[0]["name"])
				require.Equal(t, "connected", servers[0]["status"])
				require.Equal(t, "web_search", servers[1]["name"])
				require.Equal(t, "connected", servers[1]["status"])
			},
		},
		{
			name:        "empty tools",
			toolsResp:   []any{},
			checkServer: func(t *testing.T, servers []map[string]any) { require.Empty(t, servers) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Contains(t, r.URL.Path, "/experimental/tool")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(tt.toolsResp)
			})
			resp, err := c.SendControlRequest(context.Background(), "mcp_status", nil)
			require.NoError(t, err)
			tt.checkServer(t, resp["servers"].([]map[string]any))
		})
	}
}

func TestServerCommanderDoGet(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/test-path")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "value"})
	})
	var result map[string]any
	require.NoError(t, c.doGet(context.Background(), "/test-path", &result))
	require.Equal(t, "value", result["key"])
}

func TestServerCommanderDoPostWithBody(t *testing.T) {
	t.Parallel()
	var capturedBody map[string]any
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	var result map[string]any
	require.NoError(t, c.doPost(context.Background(), "/test-endpoint", map[string]any{"key": "val"}, &result))
	require.Equal(t, "val", capturedBody["key"])
	require.Equal(t, true, result["ok"])
}

func TestServerCommanderDoPatch(t *testing.T) {
	t.Parallel()
	var capturedBody map[string]any
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, c.doPatch(context.Background(), "/session/sess-test-123", map[string]any{"field": "value"}))
	require.Equal(t, "value", capturedBody["field"])
}

func TestServerCommanderDoRequestNilResult(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json, but that's ok when result is nil"))
	})
	require.NoError(t, c.doDelete(context.Background(), "/session/sess-test-123"))
}

func TestServerCommanderPendingModelNil(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	require.Nil(t, c.PendingModel())
}

// Consolidated: all HTTP error path tests
func TestServerCommanderHTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		call          func(*ServerCommander) error
		statusCode    int
		body          string
		errorContains string
	}{
		{
			name: "QueryContextUsage 503",
			call: func(c *ServerCommander) error {
				_, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
				return err
			},
			statusCode: http.StatusServiceUnavailable, body: "service unavailable",
			errorContains: "opencode context query",
		},
		{
			name: "SetPermissionMode 500",
			call: func(c *ServerCommander) error {
				_, err := c.SendControlRequest(context.Background(), "set_permission_mode", map[string]any{"mode": "bypassPermissions"})
				return err
			},
			statusCode: http.StatusInternalServerError, body: "server error",
			errorContains: "opencode set permission",
		},
		{
			name:       "Compact 500",
			call:       func(c *ServerCommander) error { return c.Compact(context.Background(), nil) },
			statusCode: http.StatusInternalServerError, body: "compact failed",
			errorContains: "opencode compact",
		},
		{
			name:       "Rewind 400",
			call:       func(c *ServerCommander) error { return c.Rewind(context.Background(), "bad-target") },
			statusCode: http.StatusBadRequest, body: "invalid target",
			errorContains: "opencode rewind",
		},
		{
			name: "DoPatch 403",
			call: func(c *ServerCommander) error {
				return c.doPatch(context.Background(), "/session/sess-test-123", map[string]any{"field": "value"})
			},
			statusCode: http.StatusForbidden, body: "forbidden",
			errorContains: "HTTP 403",
		},
		{
			name: "MCPStatus 500",
			call: func(c *ServerCommander) error {
				_, err := c.SendControlRequest(context.Background(), "mcp_status", nil)
				return err
			},
			statusCode: http.StatusInternalServerError, body: "internal server error",
			errorContains: "opencode mcp status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})
			err := tt.call(c)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestServerCommanderDoRequestErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		handler       http.HandlerFunc
		body          any
		expectError   bool
		errorContains string
	}{
		{
			name: "HTTP 400",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad request"))
			},
			expectError: true, errorContains: "HTTP 400",
		},
		{
			name: "HTTP 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("server error"))
			},
			expectError: true, errorContains: "HTTP 500",
		},
		{
			name: "JSON decode error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			},
			expectError: true, errorContains: "invalid character",
		},
		{
			name: "successful request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"result": "success"}`))
			},
			body:        map[string]any{"key": "value"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, tt.handler)
			var result map[string]any
			err := c.doPost(context.Background(), "/test", tt.body, &result)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, "success", result["result"])
			}
		})
	}
}

func TestServerCommanderDoRequestStatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"301 redirect", 301}, {"302 found", 302}, {"401 unauthorized", 401},
		{"403 forbidden", 403}, {"404 not found", 404}, {"429 too many requests", 429},
		{"502 bad gateway", 502}, {"503 service unavailable", 503},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte("error body"))
			})
			var result map[string]any
			err := c.doGet(context.Background(), "/test", &result)
			require.Error(t, err)
			require.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", tt.statusCode))
		})
	}
}

func TestServerCommanderDoRequestLargeResponseBody(t *testing.T) {
	t.Parallel()
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(string(make([]byte, 500))))
	})
	err := c.doGet(context.Background(), "/test", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func TestServerCommanderDoRequestWithJsonMarshalError(t *testing.T) {
	t.Parallel()
	type unmarshalable struct {
		Channel chan int
	}
	c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	var result map[string]any
	err := c.doPost(context.Background(), "/test", unmarshalable{Channel: make(chan int)}, &result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "json: unsupported type")
}

// Consolidated: context cancellation tests (was 5 separate functions)
func TestServerCommanderContextCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		call          func(*ServerCommander, context.Context) error
		errorContains string
	}{
		{
			name: "SendControlRequest get_context_usage",
			call: func(c *ServerCommander, ctx context.Context) error {
				_, err := c.SendControlRequest(ctx, "get_context_usage", nil)
				return err
			},
			errorContains: "opencode context query",
		},
		{
			name: "SendControlRequest set_permission_mode",
			call: func(c *ServerCommander, ctx context.Context) error {
				_, err := c.SendControlRequest(ctx, "set_permission_mode", map[string]any{"mode": "bypassPermissions"})
				return err
			},
			errorContains: "opencode set permission",
		},
		{
			name:          "Compact",
			call:          func(c *ServerCommander, ctx context.Context) error { return c.Compact(ctx, nil) },
			errorContains: "opencode compact",
		},
		{
			name:          "Rewind",
			call:          func(c *ServerCommander, ctx context.Context) error { return c.Rewind(ctx, "msg-123") },
			errorContains: "opencode rewind",
		},
		{
			name:          "Clear (delete)",
			call:          func(c *ServerCommander, ctx context.Context) error { return c.Clear(ctx) },
			errorContains: "opencode clear (delete)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called when context is cancelled")
				w.WriteHeader(http.StatusOK)
			})
			err := tt.call(c, ctx)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

// Consolidated: JSON decode error tests (was 5 separate functions)
func TestServerCommanderJSONDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		call          func(*ServerCommander, *testing.T) error
		errorContains string
	}{
		{
			name: "Compact", call: func(c *ServerCommander, t *testing.T) error { return c.Compact(context.Background(), nil) },
			errorContains: "opencode compact",
		},
		{
			name: "Rewind", call: func(c *ServerCommander, t *testing.T) error { return c.Rewind(context.Background(), "msg-123") },
			errorContains: "opencode rewind",
		},
		{
			name: "Clear create",
			call: func(c *ServerCommander, t *testing.T) error {
				return c.Clear(context.Background())
			},
			errorContains: "opencode clear (create)",
		},
		{
			name: "QueryContextUsage",
			call: func(c *ServerCommander, t *testing.T) error {
				_, err := c.SendControlRequest(context.Background(), "get_context_usage", nil)
				return err
			},
			errorContains: "opencode context query",
		},
		{
			name: "MCPStatus",
			call: func(c *ServerCommander, t *testing.T) error {
				_, err := c.SendControlRequest(context.Background(), "mcp_status", nil)
				return err
			},
			errorContains: "opencode mcp status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestCommander(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("not valid json"))
			})
			err := tt.call(c, t)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

// Consolidated: worker session ID tests
func TestWorkerSessionID(t *testing.T) {
	t.Parallel()

	t.Run("no conn get", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", New().GetWorkerSessionID())
	})
	t.Run("no conn set", func(t *testing.T) {
		t.Parallel()
		w := New()
		w.SetWorkerSessionID("sess-abc")
		require.Equal(t, "sess-abc", w.GetWorkerSessionID())
	})
	t.Run("with conn get", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
		t.Cleanup(ts.Close)
		w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil), client: ts.Client(), httpAddr: ts.URL}
		w.initHTTPConn("user-1", "sess-from-conn", "")
		require.Equal(t, "sess-from-conn", w.GetWorkerSessionID())
	})
	t.Run("with conn set", func(t *testing.T) {
		t.Parallel()
		w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
		w.initHTTPConn("user-1", "sess-original", "")
		require.Equal(t, "sess-original", w.GetWorkerSessionID())
		w.SetWorkerSessionID("sess-updated")
		require.Equal(t, "sess-updated", w.GetWorkerSessionID())
	})
}

func TestWorkerInPlaceReset(t *testing.T) {
	t.Parallel()
	require.True(t, New().InPlaceReset())
}

func TestWorkerDelegatesCmdNil(t *testing.T) {
	t.Parallel()
	w := New()
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { _, err := w.SendControlRequest(ctx, "get_context_usage", nil); return err },
		func() error { return w.Compact(ctx, nil) },
		func() error { return w.Clear(ctx) },
		func() error { return w.Rewind(ctx, "msg-1") },
	} {
		err := call()
		require.Error(t, err)
		require.Contains(t, err.Error(), "commander not initialized")
	}
}

func TestWorkerResetContextNotStarted(t *testing.T) {
	t.Parallel()
	err := New().ResetContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "worker not started")
}

func TestWorkerResetContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		body          string
		expectError   bool
		errorContains string
	}{
		{
			name: "success", statusCode: http.StatusOK, body: "",
			expectError: false,
		},
		{
			name: "500 error", statusCode: http.StatusInternalServerError, body: "reset failed",
			expectError: true, errorContains: "reset: status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Contains(t, r.URL.Path, "/session/sess-xyz/reset")
				w.WriteHeader(tt.statusCode)
				if tt.body != "" {
					w.Write([]byte(tt.body))
				}
			}))
			t.Cleanup(ts.Close)
			w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil), client: ts.Client(), httpAddr: ts.URL}
			w.initHTTPConn("user-1", "sess-xyz", "")
			err := w.ResetContext(context.Background())
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAnswersToArrays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   map[string]string
		wantLen int
	}{
		{"two entries", map[string]string{"q1": "answer1", "q2": "answer2"}, 2},
		{"empty", map[string]string{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := answersToArrays(tt.input)
			require.Len(t, result, tt.wantLen)
			if tt.wantLen == 2 {
				require.ElementsMatch(t, [][]string{{"answer1"}, {"answer2"}}, result)
			}
		})
	}
}

// Consolidated: conn tests
func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("UserID", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "user-42", (&conn{userID: "user-42"}).UserID())
	})
	t.Run("SessionID", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "sess-99", (&conn{sessionID: "sess-99"}).SessionID())
	})
	t.Run("Recv", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, (&conn{recvCh: make(chan *events.Envelope, 1)}).Recv())
	})
	t.Run("Close idempotent", func(t *testing.T) {
		t.Parallel()
		c := &conn{recvCh: make(chan *events.Envelope, 1)}
		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
	})
	t.Run("Send after close", func(t *testing.T) {
		t.Parallel()
		c := &conn{recvCh: make(chan *events.Envelope, 1)}
		c.Close()
		err := c.Send(context.Background(), &events.Envelope{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "connection closed")
	})
}

func TestWorkerCapabilities(t *testing.T) {
	t.Parallel()
	w := New()
	require.Equal(t, worker.TypeOpenCodeSrv, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Equal(t, "", w.SessionStoreDir())
	require.Equal(t, 0, w.MaxTurns())
	require.Equal(t, []string{"text", "code"}, w.Modalities())
}

// Consolidated: worker not-started tests
func TestWorkerNotStarted(t *testing.T) {
	t.Parallel()
	w := New()

	t.Run("Input", func(t *testing.T) {
		t.Parallel()
		err := w.Input(context.Background(), "hello", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "worker not started")
	})
	t.Run("Conn", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, w.Conn())
	})
	t.Run("Health", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", w.Health().SessionID)
	})
	t.Run("LastIO", func(t *testing.T) {
		t.Parallel()
		require.True(t, w.LastIO().IsZero())
	})
}
