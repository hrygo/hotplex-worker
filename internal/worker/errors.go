package worker

// WorkerErrorKind classifies worker errors for gateway-level handling.
type WorkerErrorKind int

const (
	// ErrKindUnavailable indicates the worker process is dead or its I/O is closed.
	ErrKindUnavailable WorkerErrorKind = iota
	// ErrKindSessionInUse indicates session files are locked by another process
	// (e.g. leftover from a crashed session).
	ErrKindSessionInUse
)

// WorkerError is a typed error carrying a classification kind.
// Gateway uses errors.As to match on Kind instead of string-matching error messages.
type WorkerError struct {
	Kind    WorkerErrorKind
	Message string
	Cause   error
}

func (e *WorkerError) Error() string { return e.Message }
func (e *WorkerError) Unwrap() error { return e.Cause }
