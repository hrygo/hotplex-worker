package base

import "context"

// MetadataHandler dispatches control responses extracted from Input metadata.
// Both ClaudeCode (stdin control) and OCS (HTTP POST) adapters implement this
// to unify the metadata type-switch pattern.
type MetadataHandler interface {
	HandlePermissionResponse(ctx context.Context, reqID string, allowed bool, reason string) error
	HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error
	HandleElicitationResponse(ctx context.Context, reqID string, action string, content map[string]any) error
}

// DispatchMetadata checks metadata for control response keys and dispatches
// to the handler. Returns (true, nil) if handled, (false, nil) if no match,
// or (true, err) on dispatch failure.
func DispatchMetadata(ctx context.Context, metadata map[string]any, h MetadataHandler) (bool, error) {
	if metadata == nil {
		return false, nil
	}
	if permResp, ok := metadata["permission_response"].(map[string]any); ok {
		reqID, _ := permResp["request_id"].(string)
		allowed, _ := permResp["allowed"].(bool)
		reason, _ := permResp["reason"].(string)
		return true, h.HandlePermissionResponse(ctx, reqID, allowed, reason)
	}
	if qResp, ok := metadata["question_response"].(map[string]any); ok {
		reqID, _ := qResp["id"].(string)
		answers, _ := qResp["answers"].(map[string]string)
		return true, h.HandleQuestionResponse(ctx, reqID, answers)
	}
	if eResp, ok := metadata["elicitation_response"].(map[string]any); ok {
		reqID, _ := eResp["id"].(string)
		action, _ := eResp["action"].(string)
		content, _ := eResp["content"].(map[string]any)
		return true, h.HandleElicitationResponse(ctx, reqID, action, content)
	}
	return false, nil
}
