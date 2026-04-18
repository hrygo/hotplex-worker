package session

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed sql/*.sql
var sqlFS embed.FS

// queries holds all SQL queries loaded at package initialization.
var queries = loadQueries()

// loadQueries reads all .sql files from the embedded fs and returns
// a map of query name → SQL text. The query name is derived from the
// filename without the .sql extension and without its package prefix
// (e.g. "store.get_session.sql" → "get_session").
func loadQueries() map[string]string {
	entries, err := fs.ReadDir(sqlFS, "sql")
	if err != nil {
		panic("session: read sql fs: " + err.Error())
	}

	queries := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		data, err := sqlFS.ReadFile("sql/" + name)
		if err != nil {
			panic("session: read sql file " + name + ": " + err.Error())
		}
		// Derive key from filename: strip directory and .sql extension.
		// E.g. "sql/store.get_session.sql" → "get_session"
		key := strings.TrimSuffix(name, ".sql")

		// Strip SQL comment headers and normalize whitespace.
		text := strings.TrimSpace(stripComments(string(data)))
		if text != "" {
			queries[key] = text
		}
	}
	return queries
}

// stripComments removes single-line (--) SQL comments from text.
func stripComments(sql string) string {
	var result strings.Builder
	for _, line := range strings.SplitAfter(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			result.WriteString(line)
		}
	}
	return result.String()
}
