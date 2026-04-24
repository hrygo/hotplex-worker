// Package cli provides the CLI self-service infrastructure for HotPlex Worker Gateway.
package cli

import (
	"context"
	"sort"
	"sync"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Diagnostic struct {
	Name     string       `json:"name"`
	Category string       `json:"category"`
	Status   Status       `json:"status"`
	Message  string       `json:"message"`
	Detail   string       `json:"detail,omitempty"`
	FixHint  string       `json:"fix_hint,omitempty"`
	FixFunc  func() error `json:"-"`
}

// Checker is the interface that each diagnostic check implements.
type Checker interface {
	Name() string
	Category() string
	Check(ctx context.Context) Diagnostic
}

type CheckerRegistry struct {
	mu       sync.RWMutex
	checkers []Checker
}

var DefaultRegistry = &CheckerRegistry{}

func (r *CheckerRegistry) Register(c Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers = append(r.checkers, c)
}

func (r *CheckerRegistry) All() []Checker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Checker, len(r.checkers))
	copy(result, r.checkers)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category() != result[j].Category() {
			return result[i].Category() < result[j].Category()
		}
		return result[i].Name() < result[j].Name()
	})
	return result
}

func (r *CheckerRegistry) ByCategory(cat string) []Checker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Checker
	for _, c := range r.checkers {
		if c.Category() == cat {
			result = append(result, c)
		}
	}
	return result
}
