package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

// NewSendResponseFunc creates a SendResponse closure for registering pending interactions.
func NewSendResponseFunc(log *slog.Logger, bridge *Bridge, requestID, sessionID, ownerID string, conn PlatformConn) func(map[string]any) {
	return func(metadata map[string]any) {
		respCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		env := &events.Envelope{
			Version:   events.Version,
			ID:        requestID,
			SessionID: sessionID,
			Event: events.Event{
				Type: events.Input,
				Data: map[string]any{
					"content":  "",
					"metadata": metadata,
				},
			},
			OwnerID: ownerID,
		}
		if bridge != nil {
			if err := bridge.Handle(respCtx, env, conn); err != nil {
				log.Error("interaction: failed to send response",
					"request_id", requestID,
					"session_id", sessionID,
					"err", err)
			}
		} else {
			log.Error("interaction: bridge not available",
				"request_id", requestID,
				"session_id", sessionID)
		}
	}
}

// BuildPermissionResponse creates a permission_response metadata map.
func BuildPermissionResponse(requestID string, allowed bool, reason string) map[string]any {
	return map[string]any{
		"permission_response": map[string]any{
			"request_id": requestID,
			"allowed":    allowed,
			"reason":     reason,
		},
	}
}

// BuildQuestionResponse creates a question_response metadata map.
func BuildQuestionResponse(requestID, answer string) map[string]any {
	answers := map[string]string{}
	if answer != "" {
		answers["_"] = answer
	}
	return map[string]any{
		"question_response": map[string]any{
			"id":      requestID,
			"answers": answers,
		},
	}
}

// BuildElicitationResponse creates an elicitation_response metadata map.
func BuildElicitationResponse(requestID, action string) map[string]any {
	return map[string]any{
		"elicitation_response": map[string]any{
			"id":     requestID,
			"action": action,
		},
	}
}

const (
	// DefaultInteractionTimeout is the default timeout for user interactions.
	DefaultInteractionTimeout = 5 * time.Minute
)

// PendingInteraction represents an interaction request awaiting a user response.
type PendingInteraction struct {
	ID        string        // request ID from the worker
	SessionID string        // session ID
	OwnerID   string        // user ID of the interaction owner (for auth verification)
	Type      events.Kind   // PermissionRequest, QuestionRequest, ElicitationRequest
	CreatedAt time.Time     // when the request was created
	Timeout   time.Duration // timeout duration
	// SendResponse sends the user's response back through the bridge.
	// The metadata map contains the response data specific to the interaction type.
	SendResponse func(metadata map[string]any)
	// cancelCh is closed by CancelAll to abort the watchTimeout goroutine.
	cancelCh chan struct{}
}

// InteractionManager manages pending user interactions (permission requests,
// question requests, MCP elicitation requests) with timeout support.
type InteractionManager struct {
	mu      sync.RWMutex
	pending map[string]*PendingInteraction // keyed by request ID
	log     *slog.Logger
}

// NewInteractionManager creates a new InteractionManager.
func NewInteractionManager(log *slog.Logger) *InteractionManager {
	return &InteractionManager{
		pending: make(map[string]*PendingInteraction),
		log:     log,
	}
}

// Register adds a new pending interaction and starts its timeout timer.
// If an interaction with the same ID already exists, this is a no-op.
func (m *InteractionManager) Register(pi *PendingInteraction) {
	m.mu.Lock()

	// Dedup: avoid spawning multiple timeout goroutines for the same request ID.
	if _, exists := m.pending[pi.ID]; exists {
		m.mu.Unlock()
		m.log.Debug("interaction: duplicate register, ignoring", "request_id", pi.ID)
		return
	}

	pi.cancelCh = make(chan struct{})
	m.pending[pi.ID] = pi
	m.mu.Unlock()

	// Start timeout goroutine
	go m.watchTimeout(pi)
}

// Get retrieves a pending interaction by its request ID.
func (m *InteractionManager) Get(requestID string) (*PendingInteraction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pi, ok := m.pending[requestID]
	return pi, ok
}

// Complete removes a pending interaction after a response is received.
func (m *InteractionManager) Complete(requestID string) (*PendingInteraction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pi, ok := m.pending[requestID]
	if ok {
		delete(m.pending, requestID)
	}
	return pi, ok
}

// Len returns the number of pending interactions.
func (m *InteractionManager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// GetAll returns a snapshot of all pending interactions.
// The returned slice is ordered by creation time (most recent first).
func (m *InteractionManager) GetAll() []*PendingInteraction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*PendingInteraction, 0, len(m.pending))
	for _, pi := range m.pending {
		result = append(result, pi)
	}
	slices.SortFunc(result, func(a, b *PendingInteraction) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return result
}

// GetBySession returns pending interactions for a specific session, ordered by
// creation time (most recent first). Returns nil if sessionID is empty or no
// interactions match.
func (m *InteractionManager) GetBySession(sessionID string) []*PendingInteraction {
	if sessionID == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*PendingInteraction
	for _, pi := range m.pending {
		if pi.SessionID == sessionID {
			result = append(result, pi)
		}
	}
	slices.SortFunc(result, func(a, b *PendingInteraction) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return result
}

// watchTimeout waits for the interaction timeout and auto-denies.
func (m *InteractionManager) watchTimeout(pi *PendingInteraction) {
	timer := time.NewTimer(pi.Timeout)
	defer timer.Stop()

	select {
	case <-pi.cancelCh:
		// Cancelled by CancelAll — interaction already removed from map.
		return
	case <-timer.C:
	}

	// Try to complete (remove) the interaction; if already gone, user responded.
	if _, ok := m.Complete(pi.ID); !ok {
		return
	}

	m.log.Info("interaction: timeout, auto-denying",
		"request_id", pi.ID,
		"type", pi.Type,
		"session_id", pi.SessionID)

	// Recover from panics in SendResponse — platform connection may be closed.
	func() {
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("interaction: panic in SendResponse during timeout",
					"request_id", pi.ID,
					"type", pi.Type,
					"session_id", pi.SessionID,
					"panic", r)
			}
		}()

		// Send auto-deny/reject response based on type
		switch pi.Type {
		case events.PermissionRequest:
			pi.SendResponse(BuildPermissionResponse(pi.ID, false, "interaction timed out"))
		case events.QuestionRequest:
			pi.SendResponse(BuildQuestionResponse(pi.ID, ""))
		case events.ElicitationRequest:
			pi.SendResponse(BuildElicitationResponse(pi.ID, "cancel"))
		}
	}()
}

// CancelAll removes all pending interactions for a given session.
// Called when a session ends (GC/Reset/Close). Nil-safe: no-op if m is nil.
func (m *InteractionManager) CancelAll(sessionID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, pi := range m.pending {
		if pi.SessionID == sessionID {
			delete(m.pending, id)
			close(pi.cancelCh)
			m.log.Debug("interaction: cancelled", "request_id", id, "session_id", sessionID)
		}
	}
}

// ExtractPermissionData extracts PermissionRequestData from an AEP envelope.
func ExtractPermissionData(env *events.Envelope) (*events.PermissionRequestData, error) {
	switch d := env.Event.Data.(type) {
	case events.PermissionRequestData:
		return &d, nil
	case map[string]any:
		id, _ := d["id"].(string)
		toolName, _ := d["tool_name"].(string)
		desc, _ := d["description"].(string)
		var args []string
		if a, ok := d["args"].([]any); ok {
			for _, v := range a {
				if s, ok := v.(string); ok {
					args = append(args, s)
				}
			}
		}
		return &events.PermissionRequestData{
			ID:          id,
			ToolName:    toolName,
			Description: desc,
			Args:        args,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected permission data type: %T", env.Event.Data)
	}
}

// ExtractQuestionData extracts QuestionRequestData from an AEP envelope.
func ExtractQuestionData(env *events.Envelope) (*events.QuestionRequestData, error) {
	d, ok := events.DecodeAs[events.QuestionRequestData](env.Event.Data)
	if !ok {
		return nil, fmt.Errorf("unexpected question data type: %T", env.Event.Data)
	}
	return &d, nil
}

// ExtractElicitationData extracts ElicitationRequestData from an AEP envelope.
func ExtractElicitationData(env *events.Envelope) (*events.ElicitationRequestData, error) {
	d, ok := events.DecodeAs[events.ElicitationRequestData](env.Event.Data)
	if !ok {
		return nil, fmt.Errorf("unexpected elicitation data type: %T", env.Event.Data)
	}
	return &d, nil
}
