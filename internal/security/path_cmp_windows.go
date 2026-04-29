//go:build windows

package security

import "strings"

func pathEqual(a, b string) bool { return strings.EqualFold(a, b) }

func pathHasPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}

func isRootPath(path string) bool {
	return len(path) == 3 && path[1] == ':' && path[2] == '\\'
}
