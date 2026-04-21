package client

import (
	"log/slog"
	"time"
)

// Option configures a Client. See New for usage.
type Option func(*Client) error

// URL sets the WebSocket gateway URL (e.g. "ws://localhost:8888").
func URL(rawurl string) Option {
	return func(c *Client) error {
		c.url = rawurl
		return nil
	}
}

// WorkerType sets the worker type (e.g. "claude_code", "opencode_server").
func WorkerType(t string) Option {
	return func(c *Client) error {
		c.workerType = t
		return nil
	}
}

// AuthToken sets the JWT auth token for gateway authentication.
func AuthToken(token string) Option {
	return func(c *Client) error {
		c.authToken = token
		return nil
	}
}

// APIKey sets the gateway API key.
func APIKey(key string) Option {
	return func(c *Client) error {
		c.apiKey = key
		return nil
	}
}

// PingInterval sets the heartbeat ping interval (default 54s).
func PingInterval(d time.Duration) Option {
	return func(c *Client) error {
		c.pingInterval = d
		return nil
	}
}

// ClientSessionID enables deterministic session IDs for client-managed sessions.
// Use when you need stable session identifiers across reconnections.
func ClientSessionID(id string) Option {
	return func(c *Client) error {
		c.clientSessionID = id
		return nil
	}
}

// AutoReconnect enables automatic reconnection with exponential backoff.
func AutoReconnect(enabled bool) Option {
	return func(c *Client) error {
		c.autoReconnect = enabled
		return nil
	}
}

// Logger sets the slog.Logger for the client.
func Logger(logger *slog.Logger) Option {
	return func(c *Client) error {
		if logger != nil {
			c.logger = logger
		}
		return nil
	}
}

// Metadata adds optional metadata to the init handshake.
func Metadata(metadata map[string]any) Option {
	return func(c *Client) error {
		c.metadata = metadata
		return nil
	}
}
