// Package demo provides shared utilities for the client SDK examples.
package demo

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// EnvOr returns the environment variable value or the fallback.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// FieldStr extracts a string field from map[string]any data.
func FieldStr(data any, key string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	v := m[key]
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// FieldFloat64 extracts a float64 field from map[string]any data.
func FieldFloat64(data any, key string) float64 {
	m, ok := data.(map[string]any)
	if !ok {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return f
}

// Truncate shortens s to max runes, appending "..." if truncated.
func Truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}

// Banner prints a section banner to stdout.
func Banner(title string) {
	w := len(title) + 4
	if w < 50 {
		w = 50
	}
	fmt.Println()
	fmt.Println(strings.Repeat("=", w))
	fmt.Printf("  %s\n", title)
	fmt.Println(strings.Repeat("=", w))
}
