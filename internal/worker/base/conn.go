package base

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Conn implements worker.SessionConn for stdin-based workers (claudecode, opencodecli).
type Conn struct {
	userID    string
	sessionID string
	stdin     *os.File
	recvCh    chan *events.Envelope
	log       *slog.Logger
	mu        sync.Mutex
	closed    bool
}

// Claude Code stream-json input message types.
type claudeUserMessage struct {
	Type    string        `json:"type"`
	Message claudeUserMsg `json:"message"`
}

type claudeUserMsg struct {
	Role    string              `json:"role"`
	Content []claudeTextContent `json:"content"`
}

type claudeTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewConn creates a new stdin-based session connection.
func NewConn(log *slog.Logger, stdin *os.File, userID, sessionID string) *Conn {
	if log == nil {
		log = slog.Default()
	}
	return &Conn{
		userID:    userID,
		sessionID: sessionID,
		stdin:     stdin,
		recvCh:    make(chan *events.Envelope, 256),
		log:       log,
	}
}

// Send delivers a message to the worker runtime via stdin using NDJSON encoding.
func (c *Conn) Send(ctx context.Context, msg *events.Envelope) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("base: connection closed")
	}
	c.mu.Unlock()

	// Write NDJSON to stdin.
	if err := aep.Encode(c.stdin, msg); err != nil {
		return fmt.Errorf("base: encode: %w", err)
	}

	return nil
}

// WriteAll loops syscall.Write until all data is written, handling partial
// writes and EAGAIN (non-blocking pipe on macOS). Go's stdlib File.Write does
// not retry EAGAIN for syscall-backed files, so we must use raw syscall.
func WriteAll(fd int, data []byte) error {
	n := 0
	for n < len(data) {
		nn, err := syscall.Write(fd, data[n:])
		if err != nil {
			if err == syscall.EAGAIN {
				runtime.Gosched()
				continue
			}
			return err
		}
		if nn == 0 {
			return fmt.Errorf("writeAll: zero write")
		}
		n += nn
	}
	return nil
}

func (c *Conn) SendUserMessage(ctx context.Context, content string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("base: connection closed")
	}
	c.mu.Unlock()

	msg := claudeUserMessage{
		Type: "user",
		Message: claudeUserMsg{
			Role: "user",
			Content: []claudeTextContent{
				{Type: "text", Text: content},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("base: marshal user message: %w", err)
	}
	data = append(data, '\n')

	err = WriteAll(int(c.stdin.Fd()), data)
	if err != nil {
		return fmt.Errorf("base: write user message: %w", err)
	}

	return nil
}

// Recv returns a channel that yields messages from the worker runtime.
func (c *Conn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

func (c *Conn) TrySend(env *events.Envelope) bool {
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}

// Close terminates the connection and releases resources.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	close(c.recvCh)

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	return nil
}

// UserID returns the user who owns this session.
func (c *Conn) UserID() string {
	return c.userID
}

// SessionID returns the session identifier.
func (c *Conn) SessionID() string {
	return c.sessionID
}

// SetSessionID updates the session identifier (for opencodecli's session ID extraction).
func (c *Conn) SetSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}
