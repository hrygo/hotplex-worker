package phrases

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load reads PHRASES.md from all levels with cascade-append:
//
//  1. code defaults (hardcoded via Defaults())
//  2. dir/PHRASES.md (global)
//  3. dir/{platform}/PHRASES.md
//  4. dir/{platform}/{botID}/PHRASES.md
//
// Each level's entries are appended to the pool, never replaced.
// Missing directory or file is not an error — skips gracefully.
func Load(dir, platform, botID string) (*Phrases, error) {
	p := Defaults()

	paths := []string{
		filepath.Join(dir, "PHRASES.md"),
		filepath.Join(dir, platform, "PHRASES.md"),
	}

	if botID != "" {
		if filepath.Base(botID) != botID {
			return nil, fmt.Errorf("phrases: invalid botID %q: path traversal detected", botID)
		}
		paths = append(paths, filepath.Join(dir, platform, botID, "PHRASES.md"))
	}

	for _, pth := range paths {
		data, err := os.ReadFile(pth)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("phrases: read %s: %w", pth, err)
		}
		parsed := parseMarkdown(string(data))
		merge(p.entries, parsed)
	}

	return p, nil
}

// merge appends src entries into dst for all keys.
func merge(dst, src map[string][]string) {
	for k, vals := range src {
		dst[k] = append(dst[k], vals...)
	}
}
