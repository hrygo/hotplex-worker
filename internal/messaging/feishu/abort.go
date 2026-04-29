package feishu

import "github.com/hrygo/hotplex/internal/messaging"

// IsAbortCommand checks if the message text is an abort command.
// Delegates to the shared messaging.IsAbortCommand.
func IsAbortCommand(text string) bool { return messaging.IsAbortCommand(text) }
