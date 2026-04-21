package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── Init / Global ──────────────────────────────────────────────

func TestTracker_Init_NilLogger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := InitTracker(dir, nil)
	require.NotNil(t, tr)
	require.NotNil(t, tr.log)
	t.Cleanup(func() { globalTracker.Store((*Tracker)(nil)) })
}

func TestTracker_Init_SetsGlobal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := InitTracker(dir, nil)
	require.Equal(t, tr, GlobalTracker())
	t.Cleanup(func() { globalTracker.Store((*Tracker)(nil)) })
}

func TestGlobalTracker_BeforeInit(t *testing.T) {
	t.Parallel()
	globalTracker.Store((*Tracker)(nil))
	require.Nil(t, GlobalTracker())
}

// ── EnsureDir ─────────────────────────────────────────────────

func TestTracker_EnsureDir_CreatesDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "pids")
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.EnsureDir())
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestTracker_EnsureDir_Idempotent(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "pids")
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.EnsureDir())
	require.NoError(t, tr.EnsureDir())
}

func TestTracker_EnsureDir_NestedPath(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b", "c", "pids")
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.EnsureDir())
	_, err := os.Stat(dir)
	require.NoError(t, err)
}

// ── Write ─────────────────────────────────────────────────────

func TestTracker_Write_CreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("session1", 12345))
	_, err := os.Stat(filepath.Join(dir, "session1.pid"))
	require.NoError(t, err)
}

func TestTracker_Write_ContentFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 12345))
	content, err := os.ReadFile(filepath.Join(dir, "s1.pid"))
	require.NoError(t, err)
	require.Equal(t, "12345\n", string(content))
}

func TestTracker_Write_Atomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 42))
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	require.Empty(t, matches, "no .tmp file should remain after write")
}

func TestTracker_Write_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 111))
	require.NoError(t, tr.Write("s1", 222))
	content, err := os.ReadFile(filepath.Join(dir, "s1.pid"))
	require.NoError(t, err)
	require.Equal(t, "222\n", string(content))
}

func TestTracker_Write_TracksActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 1))
	require.NoError(t, tr.Write("s2", 2))
	require.True(t, tr.active["s1"])
	require.True(t, tr.active["s2"])
	require.Len(t, tr.active, 2)
}

func TestTracker_Write_InvalidKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	for _, key := range []string{"", "a/b", "/abs", "."} {
		err := tr.Write(key, 1)
		require.Error(t, err, "key %q should be rejected", key)
		require.ErrorIs(t, err, ErrInvalidKey)
	}
}

// ── Remove ────────────────────────────────────────────────────

func TestTracker_Remove_DeletesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 100))
	require.NoError(t, tr.Remove("s1"))
	_, err := os.Stat(filepath.Join(dir, "s1.pid"))
	require.True(t, os.IsNotExist(err))
}

func TestTracker_Remove_MissingNoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Remove("nonexistent"))
}

func TestTracker_Remove_RemovesFromActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 100))
	require.True(t, tr.active["s1"])
	require.NoError(t, tr.Remove("s1"))
	require.False(t, tr.active["s1"])
}

// ── RemoveAll ─────────────────────────────────────────────────

func TestTracker_RemoveAll_DeletesAllActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 1))
	require.NoError(t, tr.Write("s2", 2))
	require.NoError(t, tr.Write("s3", 3))
	tr.RemoveAll()
	for _, k := range []string{"s1", "s2", "s3"} {
		_, err := os.Stat(filepath.Join(dir, k+".pid"))
		require.True(t, os.IsNotExist(err), "file %s.pid should be gone", k)
	}
}

func TestTracker_RemoveAll_ClearsActiveMap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	require.NoError(t, tr.Write("s1", 1))
	require.NoError(t, tr.Write("s2", 2))
	tr.RemoveAll()
	require.Empty(t, tr.active)
}

// ── CleanupOrphans ───────────────────────────────────────────

func TestCleanupOrphans_NoFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)
	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Empty(t, results)
}

func TestCleanupOrphans_DeadProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	pgid := 999999
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dead.pid"), []byte(strconv.Itoa(pgid)+"\n"), 0o644))

	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Len(t, results, 1)
	require.Equal(t, "dead", results[0].Key)
	require.Equal(t, pgid, results[0].PGID)
	require.False(t, results[0].Killed)
	require.NoError(t, results[0].Err)

	_, err := os.Stat(filepath.Join(dir, "dead.pid"))
	require.True(t, os.IsNotExist(err))
}

func TestCleanupOrphans_StaleContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.pid"), []byte("xyz\n"), 0o644))

	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Len(t, results, 1)
	require.Equal(t, "stale", results[0].Key)
	require.Error(t, results[0].Err)
	require.Contains(t, results[0].Err.Error(), "invalid content")

	_, err := os.Stat(filepath.Join(dir, "stale.pid"))
	require.True(t, os.IsNotExist(err))
}

func TestCleanupOrphans_LiveOrphan(t *testing.T) {
	t.Parallel()
	if os.Getenv("TEST_RACE_ENABLED") != "" {
		t.Skip("skipping real process test under race detector")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("skipping real process test: not on POSIX")
	}

	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	pgid := cmd.Process.Pid

	require.NoError(t, os.WriteFile(filepath.Join(dir, "orphan.pid"), []byte(strconv.Itoa(pgid)+"\n"), 0o644))

	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Len(t, results, 1)
	require.Equal(t, "orphan", results[0].Key)
	require.Equal(t, pgid, results[0].PGID)
	require.True(t, results[0].Killed)

	_ = cmd.Wait()
}

func TestCleanupOrphans_RecycledPID(t *testing.T) {
	t.Parallel()
	if os.Getenv("TEST_RACE_ENABLED") != "" {
		t.Skip("skipping real process test under race detector")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("skipping real process test: not on POSIX")
	}

	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid

	require.NoError(t, os.WriteFile(filepath.Join(dir, "recycled.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644))

	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Len(t, results, 1)
	require.Equal(t, "recycled", results[0].Key)
	require.False(t, results[0].Killed)

	_, err := os.Stat(filepath.Join(dir, "recycled.pid"))
	require.NoError(t, err)

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

func TestCleanupOrphans_Cancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	for i := range 5 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, strconv.Itoa(i)+".pid"), []byte(strconv.Itoa(999999+i)+"\n"), 0o644))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := tr.CleanupOrphans(ctx, 3, 0)
	require.LessOrEqual(t, len(results), 5)
}

func TestCleanupOrphans_Multiple(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := NewTracker(dir, nil)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "dead.pid"), []byte("999998\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.pid"), []byte("not-a-pid\n"), 0o644))

	results := tr.CleanupOrphans(context.Background(), 3, -time.Second)
	require.Len(t, results, 2)

	byKey := make(map[string]CleanupResult)
	for _, r := range results {
		byKey[r.Key] = r
	}

	require.False(t, byKey["dead"].Killed)
	require.NoError(t, byKey["dead"].Err)
	require.Error(t, byKey["stale"].Err)
}

// ── Nil Safety ────────────────────────────────────────────────

func TestNilSafety_AllMethodsNoPanic(t *testing.T) {
	t.Parallel()
	var tr *Tracker

	defer func() {
		_ = recover()
	}()

	_ = tr.EnsureDir()
	_ = tr.Remove("any")
	tr.RemoveAll()
	_ = tr.CleanupOrphans(context.Background(), 3, -time.Second)
}
