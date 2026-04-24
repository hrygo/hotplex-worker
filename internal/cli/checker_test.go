package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockChecker is a minimal Checker stub for registry tests.
type mockChecker struct {
	name     string
	category string
	status   Status
}

func (m *mockChecker) Name() string     { return m.name }
func (m *mockChecker) Category() string { return m.category }
func (m *mockChecker) Check(_ context.Context) Diagnostic {
	return Diagnostic{
		Name:     m.name,
		Category: m.category,
		Status:   m.status,
		Message:  "mock diagnostic",
	}
}

func TestRegistry_Register(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		checkers       []Checker
		wantNames      []string
		wantCategories []string
	}{
		{
			name: "single checker",
			checkers: []Checker{
				&mockChecker{name: "disk", category: "storage", status: StatusPass},
			},
			wantNames:      []string{"disk"},
			wantCategories: []string{"storage"},
		},
		{
			name: "sorted by category then name",
			checkers: []Checker{
				&mockChecker{name: "memory", category: "runtime", status: StatusPass},
				&mockChecker{name: "cpu", category: "runtime", status: StatusWarn},
				&mockChecker{name: "disk", category: "storage", status: StatusFail},
				&mockChecker{name: "network", category: "network", status: StatusPass},
			},
			wantNames:      []string{"network", "cpu", "memory", "disk"},
			wantCategories: []string{"network", "runtime", "runtime", "storage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := &CheckerRegistry{}
			for _, c := range tt.checkers {
				reg.Register(c)
			}

			all := reg.All()
			require.Len(t, all, len(tt.wantNames))

			var gotNames []string
			var gotCategories []string
			for _, c := range all {
				gotNames = append(gotNames, c.Name())
				gotCategories = append(gotCategories, c.Category())
			}
			require.Equal(t, tt.wantNames, gotNames)
			require.Equal(t, tt.wantCategories, gotCategories)
		})
	}
}

func TestRegistry_ByCategory(t *testing.T) {
	t.Parallel()

	reg := &CheckerRegistry{}
	reg.Register(&mockChecker{name: "cpu", category: "runtime", status: StatusPass})
	reg.Register(&mockChecker{name: "memory", category: "runtime", status: StatusWarn})
	reg.Register(&mockChecker{name: "disk", category: "storage", status: StatusFail})
	reg.Register(&mockChecker{name: "latency", category: "network", status: StatusPass})

	tests := []struct {
		name     string
		category string
		want     []string
	}{
		{name: "runtime category returns two checkers", category: "runtime", want: []string{"cpu", "memory"}},
		{name: "storage category returns one checker", category: "storage", want: []string{"disk"}},
		{name: "network category returns one checker", category: "network", want: []string{"latency"}},
		{name: "unknown category returns empty", category: "nonexistent", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := reg.ByCategory(tt.category)
			var got []string
			for _, c := range result {
				got = append(got, c.Name())
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRegistry_Empty(t *testing.T) {
	t.Parallel()

	reg := &CheckerRegistry{}

	all := reg.All()
	require.Empty(t, all)

	byCat := reg.ByCategory("any")
	require.Empty(t, byCat)
}

func TestDiagnostic_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		diag Diagnostic
	}{
		{
			name: "all fields populated",
			diag: Diagnostic{
				Name:     "disk-usage",
				Category: "storage",
				Status:   StatusWarn,
				Message:  "disk usage above 80%",
				Detail:   "/dev/sda1 at 87%",
				FixHint:  "clean up old logs",
				FixFunc:  func() error { return nil },
			},
		},
		{
			name: "minimal fields",
			diag: Diagnostic{
				Name:     "health",
				Category: "system",
				Status:   StatusPass,
				Message:  "ok",
			},
		},
		{
			name: "fail status with fix func",
			diag: Diagnostic{
				Name:     "config-missing",
				Category: "config",
				Status:   StatusFail,
				Message:  "config file not found",
				FixHint:  "run: cp configs/env.example .env",
				FixFunc:  func() error { return nil },
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := tt.diag
			require.Equal(t, d.Name, tt.diag.Name)
			require.Equal(t, d.Category, tt.diag.Category)
			require.Equal(t, d.Status, tt.diag.Status)
			require.Equal(t, d.Message, tt.diag.Message)
			require.Equal(t, d.Detail, tt.diag.Detail)
			require.Equal(t, d.FixHint, tt.diag.FixHint)

			if tt.diag.FixFunc != nil {
				require.NotNil(t, d.FixFunc)
				// Exercise FixFunc to confirm it is callable.
				err := d.FixFunc()
				require.NoError(t, err)
			}
		})
	}
}
