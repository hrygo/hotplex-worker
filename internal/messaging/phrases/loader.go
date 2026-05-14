package phrases

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load reads PHRASES.md from all levels with cascade-append:
//
//  1. dir/PHRASES.md (global, weight 2)
//  2. dir/{platform}/PHRASES.md (platform, weight 3)
//  3. dir/{platform}/{botID}/PHRASES.md (bot, weight 4)
//
// Each level's entries are appended to the pool, never replaced.
// Higher-level entries have higher selection weight in Random().
// Code defaults (weight 1) are only included as fallback when no
// external configuration exists for a given category.
// Missing directory or file is not an error — skips gracefully.
func Load(dir, platform, botID string) (*Phrases, error) {
	type loadLevel struct {
		path   string
		weight int
	}

	levels := []loadLevel{
		{filepath.Join(dir, "PHRASES.md"), WeightGlobal},
		{filepath.Join(dir, platform, "PHRASES.md"), WeightPlatform},
	}

	if botID != "" {
		if filepath.Base(botID) != botID {
			return nil, fmt.Errorf("phrases: invalid botID %q: path traversal detected", botID)
		}
		levels = append(levels, loadLevel{
			path:   filepath.Join(dir, platform, botID, "PHRASES.md"),
			weight: WeightBot,
		})
	}

	// Collect external entries by category.
	external := make(map[string][]entry)
	for _, lvl := range levels {
		data, err := os.ReadFile(lvl.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("phrases: read %s: %w", lvl.path, err)
		}
		parsed := parseMarkdown(string(data))
		for k, vals := range parsed {
			for _, v := range vals {
				external[k] = append(external[k], entry{text: v, weight: lvl.weight})
			}
		}
	}

	// Build final entries: external overrides defaults per-category.
	// If a category has any external entries, defaults are excluded.
	defaults := Defaults()
	merged := make(map[string][]entry)
	for cat, defEntries := range defaults.entries {
		if ext, ok := external[cat]; ok && len(ext) > 0 {
			merged[cat] = ext
		} else {
			merged[cat] = defEntries
		}
	}
	// Add external categories not present in defaults.
	for cat, ext := range external {
		if _, ok := merged[cat]; !ok {
			merged[cat] = ext
		}
	}

	return &Phrases{entries: merged}, nil
}
