package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hotplex/hotplex-worker/internal/config"
)

// ensureDBDir creates the parent directory of dbPath if it does not exist.
// This is a simple wrapper around os.MkdirAll which is idempotent and fast
// for existing directories (typically one stat syscall). We intentionally
// don't cache results to support multiple database paths in tests and future
// multi-tenancy scenarios.
func ensureDBDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("session store: create db dir: %w", err)
		}
	}
	return nil
}

const (
	// StoreTypeSQLite is the default message store type.
	StoreTypeSQLite = "sqlite"
)

// MessageStoreBuilder creates a MessageStore from a configuration.
// Implementations are registered via RegisterMessageStore.
type MessageStoreBuilder func(ctx context.Context, cfg *config.Config) (MessageStore, error)

// messageStoreRegistry maps store type names to their builders.
var messageStoreRegistry = make(map[string]MessageStoreBuilder)

// RegisterMessageStore registers a MessageStore builder under the given type name.
// This follows the same pattern as worker.Builder (internal/worker/registry.go).
// Panics if a builder is already registered under that name.
func RegisterMessageStore(name string, builder MessageStoreBuilder) {
	if _, ok := messageStoreRegistry[name]; ok {
		panic(fmt.Sprintf("session: MessageStore builder %q already registered", name))
	}
	messageStoreRegistry[name] = builder
}

// NewMessageStore creates a MessageStore using the builder registered for
// the type specified in cfg.Session.EventStoreType (default: "sqlite").
// Returns ErrMessageStoreTypeUnknown if no builder is registered for the type.
func NewMessageStore(ctx context.Context, cfg *config.Config) (MessageStore, error) {
	storeType := cfg.Session.EventStoreType
	if storeType == "" {
		storeType = StoreTypeSQLite // default
	}
	builder, ok := messageStoreRegistry[storeType]
	if !ok {
		return nil, fmt.Errorf("%w: %q (known: %v)", ErrMessageStoreTypeUnknown, storeType, knownStoreTypes())
	}
	return builder(ctx, cfg)
}

// ErrMessageStoreTypeUnknown is returned by NewMessageStore when the configured
// event store type has no registered builder.
var ErrMessageStoreTypeUnknown = fmt.Errorf("session: unknown message store type")

func knownStoreTypes() []string {
	types := make([]string, 0, len(messageStoreRegistry))
	for t := range messageStoreRegistry {
		types = append(types, t)
	}
	return types
}

// init registers the built-in sqlite MessageStore builder.
func init() {
	RegisterMessageStore(StoreTypeSQLite, func(ctx context.Context, cfg *config.Config) (MessageStore, error) {
		return NewSQLiteMessageStore(ctx, cfg)
	})
}
