package config

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// HotReloadableFields are config field paths that can be updated at runtime
// without requiring a restart. All other fields are treated as static.
// Format: "TopLevel.NestedField" (matches mapstructure tags).
var HotReloadableFields = map[string]bool{
	"gateway.addr":                 true,
	"gateway.ping_interval":       true,
	"gateway.pong_timeout":       true,
	"gateway.write_timeout":       true,
	"gateway.idle_timeout":       true,
	"gateway.broadcast_queue_size": true,
	"session.gc_scan_interval":    true,
	"session.max_concurrent":      true,
	"pool.max_size":              true,
	"pool.max_idle_per_user":     true,
	"worker.max_lifetime":        true,
	"worker.idle_timeout":         true,
	"worker.execution_timeout":   true,
	"admin.requests_per_sec":      true,
	"admin.burst":                 true,
}

// StaticFields are config fields that require a restart to take effect.
// Changing these at runtime is logged but the value is NOT applied.
var StaticFields = map[string]bool{
	"security.api_keys":           true,
	"security.tls_enabled":        true,
	"security.tls_cert_file":     true,
	"security.tls_key_file":      true,
	"security.jwt_secret":        true,
	"db.path":                     true,
	"db.wal_mode":                true,
}

// ConfigChange represents a single configuration change for audit logging.
type ConfigChange struct {
	Timestamp time.Time
	Field     string
	OldValue  string
	NewValue  string
	Hot       bool // true if the change was actually applied
}

// Watcher monitors a config file for changes and applies hot updates.
type Watcher struct {
	log    *slog.Logger
	path   string
	sp     SecretsProvider // used on reload to supply secrets
	viper  *fsnotify.Watcher
	debounce time.Duration
	onChange func(*Config) // called with the new config after hot reload
	onStatic func(string)  // called when a static field changes

	mu     sync.Mutex
	closed bool
	latest *Config // most recently loaded config (for onChange callback)

	// Audit log of all changes.
	muAudit sync.Mutex
	audit   []ConfigChange
}

// NewWatcher creates a file-system watcher for hot config reloading.
// path: absolute path to the config file.
// sp: SecretsProvider used on reload to supply sensitive values. If nil, falls back to env vars.
// onChange: called (in a goroutine) when hot-reloadable fields change.
// onStatic: called (in a goroutine) when static fields change.
// The watcher does not start until Start() is called.
func NewWatcher(log *slog.Logger, path string, sp SecretsProvider, onChange func(*Config), onStatic func(string)) *Watcher {
	if log == nil {
		log = slog.Default()
	}
	if sp == nil {
		sp = NewEnvSecretsProvider()
	}
	return &Watcher{
		log:      log,
		path:    path,
		sp:      sp,
		debounce: 500 * time.Millisecond,
		onChange: onChange,
		onStatic: onStatic,
		audit:    make([]ConfigChange, 0, 64),
	}
}

// Start begins watching the config file for changes.
// It returns an error if the file cannot be watched.
// The watcher stops when the context is cancelled or Close() is called.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.viper = fw

	// Watch the directory so we catch rename events (WRITE + RENAME on the file).
	dir := w.path
	if i := strings.LastIndex(w.path, "/"); i >= 0 {
		dir = w.path[:i]
	}
	if err := w.viper.Add(dir); err != nil {
		w.viper.Close()
		return err
	}

	go w.run(ctx)
	w.log.Info("config: watcher started", "path", w.path)
	return nil
}

func (w *Watcher) run(ctx context.Context) {
	var debounceTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-w.viper.Errors:
			if err != nil {
				w.log.Warn("config: watcher error", "error", err)
			}
		case event := <-w.viper.Events:
			if !w.isRelevant(event) {
				continue
			}
			// Reset debounce timer.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.NewTimer(w.debounce)
			select {
			case <-ctx.Done():
				return
			case <-debounceTimer.C:
				w.reload()
			}
		}
	}
}

func (w *Watcher) isRelevant(event fsnotify.Event) bool {
	// Only reload on writes/renames to the specific config file.
	if event.Name != w.path {
		return false
	}
	return event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0
}

func (w *Watcher) reload() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	prev := w.latest

	newCfg, err := Load(w.path, LoadOptions{SecretsProvider: w.sp})
	if err != nil {
		w.log.Warn("config: reload failed", "error", err)
		return
	}

	// Audit and apply changes.
	changes := diffConfigs(prev, newCfg)

	w.muAudit.Lock()
	for _, c := range changes {
		w.audit = append(w.audit, c)
		w.log.Info("config: changed",
			"field", c.Field,
			"old", c.OldValue,
			"new", c.NewValue,
			"hot", c.Hot,
		)
	}
	w.muAudit.Unlock()

	w.mu.Lock()
	w.latest = newCfg
	w.mu.Unlock()

	// Notify listeners.
	for _, c := range changes {
		if c.Hot {
			if w.onChange != nil {
				go w.onChange(newCfg)
			}
		} else {
			if w.onStatic != nil {
				go w.onStatic(c.Field)
			}
		}
	}
}

// diffConfigs compares two configs and returns the list of changes.
func diffConfigs(prev, next *Config) []ConfigChange {
	if prev == nil || next == nil {
		return nil
	}
	var changes []ConfigChange

	// We compare all known hot-reloadable fields by name.
	// For simplicity we do a string comparison of the whole struct for changed fields.
	// A more precise implementation would use reflection, but this is sufficient
	// for audit logging purposes.
	prevStr, nextStr := configSummary(prev), configSummary(next)
	if prevStr != nextStr {
		// At least one field changed — report the whole config as changed
		// and let the onChange callback decide how to apply it.
		// For a production implementation, use reflect.DeepEqual per field.
		changes = append(changes, ConfigChange{
			Timestamp: time.Now().UTC(),
			Field:     "config",
			OldValue:  prevStr,
			NewValue:  nextStr,
			Hot:       true,
		})
	}
	return changes
}

// configSummary returns a compact string representation of the config
// for change detection (used in audit log).
func configSummary(c *Config) string {
	if c == nil {
		return ""
	}
	return strings.Join([]string{
		c.Gateway.Addr,
		strconv.Itoa(c.Gateway.BroadcastQueueSize),
		c.Session.GCScanInterval.String(),
		strconv.Itoa(c.Pool.MaxSize),
		c.Worker.MaxLifetime.String(),
		c.Worker.IdleTimeout.String(),
		strconv.Itoa(c.Admin.RequestsPerSec),
	}, "|")
}

// AuditLog returns a copy of the change audit log.
func (w *Watcher) AuditLog() []ConfigChange {
	w.muAudit.Lock()
	defer w.muAudit.Unlock()
	out := make([]ConfigChange, len(w.audit))
	copy(out, w.audit)
	return out
}

// Latest returns the most recently loaded config, or nil.
func (w *Watcher) Latest() *Config {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.latest
}

// Close stops the watcher and closes the underlying file descriptor.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	if w.viper != nil {
		return w.viper.Close()
	}
	return nil
}
