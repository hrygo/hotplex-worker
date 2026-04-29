package proc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrInvalidKey is returned when a PID file key fails validation.
	ErrInvalidKey = errors.New("pidfile: invalid key")

	globalTracker atomic.Pointer[Tracker]
)

// InitTracker creates a new Tracker, sets it as the global instance, and returns it.
func InitTracker(dir string, log *slog.Logger) *Tracker {
	t := NewTracker(dir, log)
	globalTracker.Store(t)
	return t
}

// GlobalTracker returns the global Tracker instance, or nil if not initialized.
func GlobalTracker() *Tracker {
	return globalTracker.Load()
}

// Tracker manages PID files for worker processes, supporting orphan detection and cleanup.
type Tracker struct {
	dir string
	log *slog.Logger

	// fileMu serializes file I/O to prevent concurrent writes to the same file.
	fileMu sync.Mutex

	// activeMu protects the active map. Read-heavy: allows concurrent Write/Remove
	// while CleanupOrphans runs in the background.
	activeMu sync.RWMutex
	active   map[string]bool
}

// CleanupResult holds the outcome of cleaning up a single orphan process.
type CleanupResult struct {
	Key    string
	PGID   int
	Killed bool
	Err    error
}

// NewTracker creates a new Tracker. Nil logger defaults to slog.Default().
func NewTracker(dir string, log *slog.Logger) *Tracker {
	if log == nil {
		log = slog.Default()
	}
	return &Tracker{
		dir:    dir,
		log:    log,
		active: make(map[string]bool),
	}
}

// EnsureDir creates the PID directory if it doesn't exist.
func (t *Tracker) EnsureDir() error {
	if t == nil {
		return fmt.Errorf("pidfile: tracker is nil")
	}
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return fmt.Errorf("pidfile: mkdir %s: %w", t.dir, err)
	}
	return nil
}

func validateKey(key string) error {
	if key == "" || key == "." || strings.Contains(key, "/") {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return nil
}

// pidPath returns the full path for a PID file key.
func (t *Tracker) pidPath(key string) string {
	return filepath.Join(t.dir, key+".pid")
}

// Write creates a PID file for the given key with the specified PGID.
// Uses atomic write (temp file + rename) to prevent partial reads.
func (t *Tracker) Write(key string, pgid int) error {
	if err := validateKey(key); err != nil {
		return err
	}

	t.fileMu.Lock()
	defer t.fileMu.Unlock()

	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return fmt.Errorf("pidfile: mkdir %s: %w", t.dir, err)
	}

	path := t.pidPath(key)
	tmpPath := path + ".tmp"
	content := strconv.Itoa(pgid) + "\n"

	if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("pidfile: write temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("pidfile: rename %s -> %s: %w", tmpPath, path, err)
	}

	t.activeMu.Lock()
	t.active[key] = true
	t.activeMu.Unlock()

	t.log.Debug("pidfile: written", "key", key, "pgid", pgid, "path", path)
	return nil
}

// Remove deletes a PID file and removes it from the active set.
// Best-effort: logs warnings on error but always returns nil.
func (t *Tracker) Remove(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	t.fileMu.Lock()
	defer t.fileMu.Unlock()

	path := t.pidPath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.log.Warn("pidfile: remove failed", "key", key, "path", path, "err", err)
	}

	t.activeMu.Lock()
	delete(t.active, key)
	t.activeMu.Unlock()

	return nil
}

// RemoveAll deletes all active PID files and clears the active set.
// PID files are removed in parallel to minimize shutdown latency.
// Safe to call even if other goroutines are calling Write/Remove concurrently.
func (t *Tracker) RemoveAll() {
	t.activeMu.RLock()
	keys := make([]string, 0, len(t.active))
	for k := range t.active {
		keys = append(keys, k)
	}
	t.activeMu.RUnlock()

	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			_ = t.Remove(k) // Remove is idempotent
		}(key)
	}
	wg.Wait()
}

// CleanupOrphans scans all PID files older than minAge, detects orphan processes,
// and kills them. Concurrency is capped at maxConcurrent (default 3).
// Files created after cleanup starts (or younger than minAge) are skipped to avoid
// killing newly started workers that happened to reuse a filename or PID.
// Write/Remove operations are never blocked by cleanup.
// Safe to call as a goroutine — does not block the caller.
func (t *Tracker) CleanupOrphans(ctx context.Context, maxConcurrent int, minAge time.Duration) []CleanupResult {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	// Negative minAge disables the age filter entirely (used in tests).
	skipAgeCheck := minAge < 0
	if minAge <= 0 {
		minAge = 5 * time.Second
	}
	sem := make(chan struct{}, maxConcurrent)
	cutoff := time.Now().Add(-minAge).Unix()

	// Glob under lock but release immediately — file I/O happens outside the critical section.
	t.fileMu.Lock()
	pattern := filepath.Join(t.dir, "*.pid")
	matches, err := filepath.Glob(pattern)
	t.fileMu.Unlock()

	if err != nil {
		t.log.Warn("pidfile: glob failed", "pattern", pattern, "err", err)
		return nil
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []CleanupResult
	)

	for _, match := range matches {
		// Skip files younger than minAge — they are from the current run.
		if !skipAgeCheck {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.ModTime().Unix() > cutoff {
				t.log.Debug("pidfile: skipping recent file during cleanup", "path", match, "mtime", info.ModTime())
				continue
			}
		}

		// Non-blocking semaphore acquire: skip if context already cancelled.
		select {
		case <-ctx.Done():
			t.log.Info("pidfile: cleanup cancelled", "processed", len(results), "remaining", len(matches)-len(results))
			goto done
		case sem <- struct{}{}:
			// acquired
		}

		wg.Add(1)
		go func(match string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			// Use background context so cancellation doesn't interrupt
			// the graceful shutdown timer in cleanupSingle.
			result := t.cleanupSingle(context.Background(), match)
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(match)
	}

done:
	wg.Wait()
	return results
}

func (t *Tracker) cleanupSingle(ctx context.Context, match string) CleanupResult {
	result := CleanupResult{}

	fileName := filepath.Base(match)
	key := strings.TrimSuffix(fileName, ".pid")
	result.Key = key

	// Read file content.
	content, err := os.ReadFile(match)
	if err != nil {
		if os.IsNotExist(err) {
			return result
		}
		t.log.Warn("pidfile: read failed", "path", match, "err", err)
		result.Err = fmt.Errorf("pidfile: read %s: %w", match, err)
		return result
	}

	pgid, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
	if parseErr != nil {
		t.log.Warn("pidfile: invalid content, removing", "path", match, "content", string(content))
		_ = os.Remove(match)
		result.Err = fmt.Errorf("pidfile: invalid content: %q", string(content))
		return result
	}
	result.PGID = pgid

	// Check process existence (PID recycling defense).
	if err := IsProcessAlive(pgid); err != nil {
		if IsProcessNotExist(err) {
			t.log.Info("pidfile: stale PID file, process not found", "pgid", pgid)
			_ = os.Remove(match)
			return result
		}
		t.log.Warn("pidfile: process check error", "pgid", pgid, "err", err)
		result.Err = fmt.Errorf("pidfile: check process %d: %w", pgid, err)
		_ = os.Remove(match)
		return result
	}

	// Verify PGID is still the process group leader (PID recycled).
	if err := IsProcessGroupAlive(pgid); err != nil {
		if IsProcessNotExist(err) {
			t.log.Warn("pidfile: PID recycled, not a process group leader", "pgid", pgid)
			return result
		}
		t.log.Warn("pidfile: group check error", "pgid", pgid, "err", err)
		result.Err = fmt.Errorf("pidfile: check pgid %d: %w", pgid, err)
		_ = os.Remove(match)
		return result
	}

	t.log.Warn("pidfile: confirmed orphan, killing", "pgid", pgid)

	// Graceful terminate.
	if err := GracefulTerminate(pgid); err != nil {
		t.log.Warn("pidfile: graceful terminate failed", "pgid", pgid, "err", err)
	}

	// Wait for graceful shutdown.
	graceTimer := time.NewTimer(5 * time.Second)
	defer graceTimer.Stop()

	select {
	case <-graceTimer.C:
		if err := ForceKill(pgid); err != nil {
			t.log.Warn("pidfile: force kill failed", "pgid", pgid, "err", err)
		}
		result.Killed = true
	case <-ctx.Done():
		result.Killed = false
		result.Err = ctx.Err()
		return result
	}

	if err := os.Remove(match); err != nil {
		t.log.Warn("pidfile: remove after kill failed", "path", match, "err", err)
	}

	// Clean up active map entry if it exists.
	t.activeMu.Lock()
	delete(t.active, key)
	t.activeMu.Unlock()

	return result
}
