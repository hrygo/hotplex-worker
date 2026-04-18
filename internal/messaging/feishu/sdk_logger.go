package feishu

import (
	"context"
	"fmt"
	"log/slog"
)

// SlogLogger implements larkcore.Logger, wrapping slog.Logger.
// This ensures all Feishu SDK logs use the same JSON format and level
// as the application logs.
type SlogLogger struct{ *slog.Logger }

func (s SlogLogger) Debug(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelDebug, fmt.Sprint(args...))
}
func (s SlogLogger) Info(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelInfo, fmt.Sprint(args...))
}
func (s SlogLogger) Warn(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelWarn, fmt.Sprint(args...))
}
func (s SlogLogger) Error(_ context.Context, args ...any) {
	s.Logger.Log(context.Background(), slog.LevelError, fmt.Sprint(args...))
}
