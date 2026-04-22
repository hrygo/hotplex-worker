package opencodeserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ServerCommander implements ControlRequester + WorkerCommander for OpenCode Server.
// Routes worker commands to OpenCode's HTTP REST API.
type ServerCommander struct {
	client       *http.Client
	baseURL      string
	sessionID    string
	pendingModel *ModelRef
}

// ModelRef stores model selection for subsequent message requests.
type ModelRef struct {
	ProviderID string
	ModelID    string
}

// SendControlRequest implements ControlRequester interface.
func (c *ServerCommander) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	switch subtype {
	case "get_context_usage":
		return c.queryContextUsage(ctx)
	case "set_model":
		return c.setModel(ctx, body)
	case "set_permission_mode":
		return c.setPermissionMode(ctx, body)
	case "mcp_status":
		return c.queryMCPStatus(ctx)
	default:
		return nil, fmt.Errorf("opencode server: unsupported control request: %s", subtype)
	}
}

// Compact implements WorkerCommander — POST /session/{id}/summarize.
func (c *ServerCommander) Compact(ctx context.Context, args map[string]any) error {
	reqBody := map[string]any{"auto": false}
	if c.pendingModel != nil {
		reqBody["providerID"] = c.pendingModel.ProviderID
		reqBody["modelID"] = c.pendingModel.ModelID
	}
	var result bool
	if err := c.doPost(ctx, "/session/"+c.sessionID+"/summarize", reqBody, &result); err != nil {
		return fmt.Errorf("opencode compact: %w", err)
	}
	return nil
}

// Clear implements WorkerCommander — delete session + create new.
func (c *ServerCommander) Clear(ctx context.Context) error {
	if err := c.doDelete(ctx, "/session/"+c.sessionID); err != nil {
		return fmt.Errorf("opencode clear (delete): %w", err)
	}
	var newSession struct {
		ID string `json:"id"`
	}
	if err := c.doPost(ctx, "/session", map[string]any{}, &newSession); err != nil {
		return fmt.Errorf("opencode clear (create): %w", err)
	}
	c.sessionID = newSession.ID
	return nil
}

// Rewind implements WorkerCommander — POST /session/{id}/revert.
func (c *ServerCommander) Rewind(ctx context.Context, targetID string) error {
	reqBody := map[string]any{}
	if targetID != "" {
		reqBody["messageID"] = targetID
	}
	var result any
	if err := c.doPost(ctx, "/session/"+c.sessionID+"/revert", reqBody, &result); err != nil {
		return fmt.Errorf("opencode rewind: %w", err)
	}
	return nil
}

// SessionID returns the current session ID (may change after Clear).
func (c *ServerCommander) SessionID() string { return c.sessionID }

// PendingModel returns stored model for injection into message requests.
func (c *ServerCommander) PendingModel() *ModelRef { return c.pendingModel }

// UpdateSessionID updates the internal session ID (called after Clear creates a new session).
func (c *ServerCommander) UpdateSessionID(id string) { c.sessionID = id }

func (c *ServerCommander) queryContextUsage(ctx context.Context) (map[string]any, error) {
	var messages []openCodeMessage
	if err := c.doGet(ctx, "/session/"+c.sessionID+"/message?limit=100", &messages); err != nil {
		return nil, fmt.Errorf("opencode context query: %w", err)
	}
	var totalInput, totalOutput, totalReasoning, totalCacheRead, totalCacheWrite int
	var model string
	for _, msg := range messages {
		if msg.Info.Role != "assistant" || msg.Info.Tokens == nil {
			continue
		}
		totalInput += msg.Info.Tokens.Input
		totalOutput += msg.Info.Tokens.Output
		totalReasoning += msg.Info.Tokens.Reasoning
		totalCacheRead += msg.Info.Tokens.Cache.Read
		totalCacheWrite += msg.Info.Tokens.Cache.Write
		if msg.Info.Model != nil {
			model = msg.Info.Model.ProviderID + "/" + msg.Info.Model.ModelID
		}
	}
	totalTokens := totalInput + totalOutput + totalReasoning + totalCacheRead + totalCacheWrite
	return map[string]any{
		"totalTokens": totalTokens,
		"maxTokens":   0,
		"percentage":  0,
		"model":       model,
		"categories": []map[string]any{
			{"name": "Input tokens", "tokens": totalInput},
			{"name": "Output tokens", "tokens": totalOutput},
			{"name": "Reasoning tokens", "tokens": totalReasoning},
			{"name": "Cache read", "tokens": totalCacheRead},
			{"name": "Cache write", "tokens": totalCacheWrite},
		},
	}, nil
}

func (c *ServerCommander) setModel(_ context.Context, body map[string]any) (map[string]any, error) {
	providerID, _ := body["providerID"].(string)
	modelID, _ := body["modelID"].(string)
	if model, ok := body["model"].(string); ok && providerID == "" {
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			providerID, modelID = parts[0], parts[1]
		} else {
			modelID = model
		}
	}
	c.pendingModel = &ModelRef{ProviderID: providerID, ModelID: modelID}
	return map[string]any{"success": true, "model": modelID}, nil
}

func (c *ServerCommander) setPermissionMode(ctx context.Context, body map[string]any) (map[string]any, error) {
	mode, _ := body["mode"].(string)
	var rules []map[string]any
	switch mode {
	case "bypassPermissions":
		rules = []map[string]any{{"permission": "*", "action": "allow", "pattern": "*"}}
	default:
		rules = []map[string]any{}
	}
	if err := c.doPatch(ctx, "/session/"+c.sessionID, map[string]any{"permission": rules}); err != nil {
		return nil, fmt.Errorf("opencode set permission: %w", err)
	}
	return map[string]any{"success": true, "mode": mode}, nil
}

func (c *ServerCommander) queryMCPStatus(ctx context.Context) (map[string]any, error) {
	var tools []struct {
		Name string `json:"name"`
	}
	if err := c.doGet(ctx, "/experimental/tool", &tools); err != nil {
		return nil, fmt.Errorf("opencode mcp status: %w", err)
	}
	servers := make([]map[string]any, len(tools))
	for i, t := range tools {
		servers[i] = map[string]any{"name": t.Name, "status": "connected"}
	}
	return map[string]any{"servers": servers}, nil
}

// HTTP helpers

func (c *ServerCommander) doGet(ctx context.Context, path string, result any) error {
	return c.doRequest(ctx, http.MethodGet, path, nil, result)
}

func (c *ServerCommander) doPost(ctx context.Context, path string, body, result any) error {
	return c.doRequest(ctx, http.MethodPost, path, body, result)
}

func (c *ServerCommander) doPatch(ctx context.Context, path string, body any) error {
	return c.doRequest(ctx, http.MethodPatch, path, body, nil)
}

func (c *ServerCommander) doDelete(ctx context.Context, path string) error {
	return c.doRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (c *ServerCommander) doRequest(ctx context.Context, method, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("opencode API %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Compile-time interface checks.
var (
	_ ControlRequester = (*ServerCommander)(nil)
	_ WorkerCommander  = (*ServerCommander)(nil)
)

type openCodeMessage struct {
	Info struct {
		Role   string `json:"role"`
		Tokens *struct {
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
			Cache     struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
		Model *struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"model"`
	} `json:"info"`
}

// ControlRequester is implemented by workers that support structured control queries.
type ControlRequester interface {
	SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
}

// WorkerCommander is implemented by workers that support worker-level commands
// beyond the basic Input() passthrough.
type WorkerCommander interface {
	Compact(ctx context.Context, args map[string]any) error
	Clear(ctx context.Context) error
	Rewind(ctx context.Context, targetID string) error
}
