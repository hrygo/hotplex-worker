package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

// sensitiveParams lists URL query keys that must be redacted in logs.
var sensitiveParams = map[string]bool{
	"access_key": true,
	"ticket":     true,
}

// redactURL replaces sensitive query parameters with "***" in URLs.
// Non-URL strings are returned unchanged.
func redactURL(s string) string {
	if !strings.HasPrefix(s, "ws://") && !strings.HasPrefix(s, "wss://") && !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	q := u.Query()
	changed := false
	for k := range q {
		if sensitiveParams[k] {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
		return u.String()
	}
	return s
}

// SlogLogger implements larkcore.Logger, wrapping slog.Logger.
// This ensures all Feishu SDK logs use the same JSON format and level
// as the application logs, with sensitive URL params redacted.
type SlogLogger struct{ *slog.Logger }

func (s SlogLogger) Debug(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelDebug, redactURL(fmt.Sprint(args...)))
}
func (s SlogLogger) Info(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelInfo, redactURL(fmt.Sprint(args...)))
}
func (s SlogLogger) Warn(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelWarn, redactURL(fmt.Sprint(args...)))
}
func (s SlogLogger) Error(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelError, redactURL(fmt.Sprint(args...)))
}
