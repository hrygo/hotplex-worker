package client

import (
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

// WorkerType sets the worker type (e.g. "claude_code", "opencode_cli").
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
