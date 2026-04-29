//go:build darwin || linux

package security

import "strings"

func pathEqual(a, b string) bool { return a == b }

func pathHasPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

func isRootPath(path string) bool { return path == "/" }
