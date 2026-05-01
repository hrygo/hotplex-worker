//go:build darwin || linux

package security

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func pathEqual(a, b string) bool { return a == b }

func pathHasPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

func isRootPath(path string) bool { return path == "/" }

// getCurrentUser returns the current username.
// Prioritizes $USER environment variable for performance, falls back to os/user.Current.
func getCurrentUser() string {
	// Fast path: environment variable
	if username := os.Getenv("USER"); username != "" {
		return username
	}

	// Fallback: system call
	if u, err := user.Current(); err == nil {
		return u.Username
	}

	return ""
}

// matchesUserHomePattern checks if path matches /home/<username>/* or /Users/<username>/* pattern.
// Returns (matches, username).
func matchesUserHomePattern(path string) (bool, string) {
	// Linux: /home/<username>/*
	if strings.HasPrefix(path, "/home/") {
		rest := strings.TrimPrefix(path, "/home/")
		parts := strings.SplitN(rest, string(filepath.Separator), 2)
		if len(parts) >= 1 && parts[0] != "" {
			return true, parts[0]
		}
	}

	// macOS: /Users/<username>/*
	if strings.HasPrefix(path, "/Users/") {
		rest := strings.TrimPrefix(path, "/Users/")
		parts := strings.SplitN(rest, string(filepath.Separator), 2)
		if len(parts) >= 1 && parts[0] != "" {
			return true, parts[0]
		}
	}

	return false, ""
}

// matchesUsrLocalPattern checks if path matches /usr/local/<username>/* pattern.
// Returns (matches, username).
func matchesUsrLocalPattern(path string) (bool, string) {
	if strings.HasPrefix(path, "/usr/local/") {
		rest := strings.TrimPrefix(path, "/usr/local/")
		parts := strings.SplitN(rest, string(filepath.Separator), 2)
		if len(parts) >= 1 && parts[0] != "" {
			return true, parts[0]
		}
	}
	return false, ""
}

// isOwnedByCurrentUser checks if the path exists and is owned by the current user.
// Returns (isOwned, error). If path doesn't exist, returns (false, nil).
func isOwnedByCurrentUser(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Path doesn't exist yet - can't verify ownership
			return false, nil
		}
		return false, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, nil
	}

	currentUser := getCurrentUser()
	if currentUser == "" {
		return false, nil
	}

	// Convert UID to username
	uid := strconv.Itoa(int(stat.Uid))
	fileOwner, err := user.LookupId(uid)
	if err != nil {
		return false, nil
	}

	return fileOwner.Username == currentUser, nil
}

// isUserAccessibleDirectory performs intelligent analysis to determine if a path
// under /home or /usr/local should be accessible to the current user.
//
// Rules:
//  1. /home/<current_user>/* → allowed
//  2. /Users/<current_user>/* → allowed (macOS)
//  3. /usr/local/<current_user>/* → allowed
//  4. If path exists, check actual file ownership
//  5. Otherwise → deny
func isUserAccessibleDirectory(path string) bool {
	currentUser := getCurrentUser()
	if currentUser == "" {
		return false
	}

	// Check /home/<username> pattern (Linux)
	if matches, owner := matchesUserHomePattern(path); matches {
		return owner == currentUser
	}

	// Check /usr/local/<username> pattern
	if matches, owner := matchesUsrLocalPattern(path); matches {
		return owner == currentUser
	}

	// Fallback: check actual file ownership if path exists
	if owned, err := isOwnedByCurrentUser(path); err == nil && owned {
		return true
	}

	return false
}
