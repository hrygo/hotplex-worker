package worker

import "context"

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
