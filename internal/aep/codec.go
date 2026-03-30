package aep

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"hotplex-worker/pkg/events"
)

// Encode writes an Envelope to w as a JSON string followed by a newline.
func Encode(w io.Writer, env *events.Envelope) error {
	// Always stamp the current version.
	env.Version = events.Version
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

// EncodeChunk is like Encode but for streaming deltas where you want to avoid
// re-allocating the encoder on each call. Caller must call w.Flush() when done.
func EncodeChunk(w io.Writer, env *events.Envelope) error {
	env.Version = events.Version
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("aep: marshal envelope: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// Decode reads a single newline-delimited JSON Envelope from r.
func Decode(r io.Reader) (*events.Envelope, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var env events.Envelope
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("aep: decode envelope: %w", err)
	}

	if err := Validate(&env); err != nil {
		return nil, fmt.Errorf("aep: validate envelope: %w", err)
	}

	return &env, nil
}

// DecodeLine decodes a single JSON-encoded line (no trailing newline required).
func DecodeLine(data []byte) (*events.Envelope, error) {
	var env events.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("aep: unmarshal envelope: %w", err)
	}
	if err := Validate(&env); err != nil {
		return nil, fmt.Errorf("aep: validate envelope: %w", err)
	}
	return &env, nil
}

// Validate checks that an Envelope has all required fields set correctly.
func Validate(env *events.Envelope) error {
	var errs []string

	if env.Version == "" {
		errs = append(errs, "version is required")
	} else if env.Version != events.Version {
		errs = append(errs, fmt.Sprintf("unsupported version: %q (want %q)", env.Version, events.Version))
	}

	if env.ID == "" {
		errs = append(errs, "id is required")
	}

	if env.SessionID == "" {
		errs = append(errs, "session_id is required")
	}

	if env.Seq <= 0 {
		errs = append(errs, "seq must be a positive integer")
	}

	if env.Timestamp <= 0 {
		errs = append(errs, "timestamp must be a positive unix-ms value")
	}

	if env.Event.Type == "" {
		errs = append(errs, "event.kind is required")
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// NewID generates a new event ID using the evt_ prefix.
func NewID() string {
	return "evt_" + uuid.NewString()
}

// NewSessionID generates a new session ID using the sess_ prefix.
func NewSessionID() string {
	return "sess_" + uuid.NewString()
}

// EncodeJSON encodes an envelope to JSON bytes (no trailing newline).
func EncodeJSON(env *events.Envelope) ([]byte, error) {
	env.Version = events.Version
	return json.Marshal(env)
}

// MustMarshal is like EncodeJSON but panics on error.
func MustMarshal(env *events.Envelope) []byte {
	data, err := EncodeJSON(env)
	if err != nil {
		panic(err)
	}
	return data
}

// IsSessionBusy checks whether the error data indicates a SESSION_BUSY condition.
func IsSessionBusy(env *events.Envelope) bool {
	if env.Event.Type != events.Error {
		return false
	}
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return false
	}
	code, _ := data["code"].(string)
	return code == string(events.ErrCodeSessionBusy)
}

// IsTerminalEvent returns true for events that signal a session is done.
func IsTerminalEvent(kind events.Kind) bool {
	return kind == events.Done || kind == events.Error
}

// ParseSessionID strips the "sess_" prefix if present.
func ParseSessionID(id string) string {
	return strings.TrimPrefix(id, "sess_")
}

// SeqKey returns the key used for deduplication (sessionID:eventID).
func SeqKey(sessionID, eventID string) string {
	return sessionID + ":" + eventID
}
