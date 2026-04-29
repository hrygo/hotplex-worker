package slack

import "github.com/hrygo/hotplex/internal/messaging"

// IsAbortCommand checks if text is an abort trigger.
// Delegates to the shared messaging.IsAbortCommand.
func IsAbortCommand(text string) bool { return messaging.IsAbortCommand(text) }
