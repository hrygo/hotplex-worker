// Package aep implements the AEP v1 protocol codec for HotPlex Worker Gateway.
// This package contains the core protocol encoding/decoding logic shared by both
// the gateway server and client implementations.
package aep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/pkg/events"
)

// jsLineTerminators holds the UTF-8 encoding of U+2028 and U+2029.
// These are valid JSON, but invalid JavaScript string literals — NDJSON consumers
// that parse lines as JS string literals will silently truncate at these codepoints.
var jsLineTerminators = [...]byte{0xE2, 0x80, 0xA8, 0xE2, 0x80, 0xA9}

// Encode writes an Envelope to w as a newline-delimited JSON record.
// NDJSON-safe: U+2028 and U+2029 are escaped to prevent JS parsers truncating.
func Encode(w io.Writer, env *events.Envelope) error {
	env.Version = events.Version
	if env.Timestamp == 0 {
		env.Timestamp = nowMillis()
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("aep: marshal envelope: %w", err)
	}
	data = escapeJSTerminators(data)
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// EncodeChunk is like Encode but for streaming deltas where you want to avoid
// re-allocating the encoder on each call. Caller must call w.Flush() when done.
func EncodeChunk(w io.Writer, env *events.Envelope) error {
	env.Version = events.Version
	if env.Timestamp == 0 {
		env.Timestamp = nowMillis()
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("aep: marshal envelope: %w", err)
	}
	data = escapeJSTerminators(data)
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// NDJSONSpec: https://datatracker.ietf.org/doc/html/rfc7464
// JS JSON.parse accepts U+2028/U+2029 as valid JSON, but they are NOT valid
// inside JavaScript string literals. A NDJSON consumer that evaluates lines as JS
// (e.g. JSON.parse via eval) will silently truncate at these codepoints.
//
// escapeJSTerminators converts any raw U+2028/U+2029 bytes already present in
// the JSON output to the \u2028 / \u2029 escape sequences.
//
// NOTE: json.Marshal already emits \u2028 / \u2029 when marshaling Go strings
// containing those codepoints. This function catches the edge case where the
// raw UTF-8 bytes somehow survive (e.g. in map[string]any with raw []byte values).
func escapeJSTerminators(data []byte) []byte {
	// Fast path: no terminators present
	if !bytes.Contains(data, jsLineTerminators[:3]) &&
		!bytes.Contains(data, jsLineTerminators[3:]) {
		return data
	}
	result := make([]byte, 0, len(data)+32)
	for i := 0; i < len(data); {
		if i+3 <= len(data) {
			b0, b1, b2 := data[i], data[i+1], data[i+2]
			if b0 == 0xE2 && b1 == 0x80 {
				if b2 == 0xA8 {
					result = append(result, '\\', 'u', '2', '0', '2', '8')
					i += 3
					continue
				}
				if b2 == 0xA9 {
					result = append(result, '\\', 'u', '2', '0', '2', '9')
					i += 3
					continue
				}
			}
		}
		result = append(result, data[i])
		i++
	}
	return result
}

// EscapeJSTerminators is the exported version of escapeJSTerminators.
func EscapeJSTerminators(data []byte) []byte {
	return escapeJSTerminators(data)
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

// ValidateMinimal validates only the fields required for client->server messages.
// This is less strict than Validate, suitable for incoming client messages.
func ValidateMinimal(env *events.Envelope) error {
	if env.Version != "" && env.Version != events.Version {
		return fmt.Errorf("unsupported version: %q", env.Version)
	}
	if env.Event.Type == "" {
		return fmt.Errorf("event type is required")
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
// NDJSON-safe: U+2028 and U+2029 are escaped.
func EncodeJSON(env *events.Envelope) ([]byte, error) {
	env.Version = events.Version
	if env.Timestamp == 0 {
		env.Timestamp = nowMillis()
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("aep: marshal envelope: %w", err)
	}
	return escapeJSTerminators(data), nil
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

// NewEnvelope creates a new Envelope with version, ID, seq, and timestamp set.
func NewEnvelope(id, sessionID string, seq int64, kind events.Kind, data any) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        id,
		Seq:       seq,
		SessionID: sessionID,
		Timestamp: nowMillis(),
		Event: events.Event{
			Type: kind,
			Data: data,
		},
	}
}

// NewInputEnvelope creates a new input envelope.
func NewInputEnvelope(sessionID, content string) *events.Envelope {
	return NewEnvelope(NewID(), sessionID, 0, events.Input, map[string]any{
		"content": content,
	})
}

// NewPingEnvelope creates a new ping envelope.
func NewPingEnvelope(sessionID string) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       0,
		Priority:  events.PriorityControl,
		SessionID: sessionID,
		Timestamp: nowMillis(),
		Event: events.Event{
			Type: events.Ping,
			Data: struct{}{},
		},
	}
}

// nowMillis returns the current time in milliseconds.
func nowMillis() int64 {
	return nowFunc()
}

// nowFunc allows overriding for testing. Defaults to real wall-clock time.
var nowFunc = func() int64 {
	return time.Now().UnixMilli()
}
