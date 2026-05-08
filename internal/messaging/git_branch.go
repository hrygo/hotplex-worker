package messaging

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// GitBranchOf returns the current git branch name for the given directory.
// Best-effort: 2s timeout, errors silently ignored. Results are cached per
// directory with a 30-minute TTL.
func GitBranchOf(dir string) string {
	if dir == "" {
		return ""
	}
	if !checkGitAvailable() {
		return ""
	}

	now := time.Now()
	gitBranchMu.RLock()
	if e, ok := gitBranchCache[dir]; ok && now.Before(e.expiry) {
		branch := e.branch
		gitBranchMu.RUnlock()
		return branch
	}
	gitBranchMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	branch := ""
	if err == nil {
		branch = strings.TrimSpace(string(out))
		if branch == "HEAD" {
			shortCmd := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD")
			shortCmd.Dir = dir
			if shortOut, shortErr := shortCmd.Output(); shortErr == nil {
				branch = strings.TrimSpace(string(shortOut))
			}
		}
	}

	gitBranchMu.Lock()
	gitBranchCache[dir] = gitBranchEntry{branch: branch, expiry: now.Add(gitBranchTTL)}
	gitBranchMu.Unlock()
	return branch
}

var (
	gitAvailable bool
	gitOnce      sync.Once

	gitBranchMu    sync.RWMutex
	gitBranchCache = map[string]gitBranchEntry{}
)

const gitBranchTTL = 30 * time.Minute

type gitBranchEntry struct {
	branch string
	expiry time.Time
}

func checkGitAvailable() bool {
	gitOnce.Do(func() {
		_, err := exec.LookPath("git")
		gitAvailable = err == nil
	})
	return gitAvailable
}
