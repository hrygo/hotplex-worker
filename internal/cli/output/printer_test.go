package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestPrintDiagnostic(t *testing.T) {
	tests := []struct {
		name     string
		d        cli.Diagnostic
		verbose  bool
		contains []string
	}{
		{
			name: "pass without verbose",
			d: cli.Diagnostic{
				Name:     "test.pass",
				Category: "test",
				Status:   cli.StatusPass,
				Message:  "Test passed",
			},
			verbose:  false,
			contains: []string{"✓", "test", "Test passed"},
		},
		{
			name: "warn with detail verbose",
			d: cli.Diagnostic{
				Name:     "test.warn",
				Category: "test",
				Status:   cli.StatusWarn,
				Message:  "Test warning",
				Detail:   "Some detail",
			},
			verbose:  true,
			contains: []string{"⚠", "test", "Test warning", "Detail:", "Some detail"},
		},
		{
			name: "fail with fix hint",
			d: cli.Diagnostic{
				Name:     "test.fail",
				Category: "test",
				Status:   cli.StatusFail,
				Message:  "Test failed",
				FixHint:  "Run fix command",
			},
			verbose:  false,
			contains: []string{"✗", "test", "Test failed", "→", "Run fix command"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintDiagnostic(&buf, tt.d, tt.verbose)
			output := buf.String()
			for _, s := range tt.contains {
				if !strings.Contains(output, s) {
					t.Errorf("output missing %q: %s", s, output)
				}
			}
		})
	}
}

func TestPrintSummary(t *testing.T) {
	tests := []struct {
		name     string
		pass     int
		warn     int
		fail     int
		fixable  int
		contains []string
	}{
		{
			name:     "all pass",
			pass:     3,
			warn:     0,
			fail:     0,
			fixable:  0,
			contains: []string{"3 checks: 3 passed, 0 warnings, 0 failed"},
		},
		{
			name:     "mixed with fixable",
			pass:     5,
			warn:     2,
			fail:     1,
			fixable:  3,
			contains: []string{"8 checks: 5 passed, 2 warnings, 1 failed (3 fixable)"},
		},
		{
			name:     "all fail",
			pass:     0,
			warn:     0,
			fail:     4,
			fixable:  0,
			contains: []string{"4 checks: 0 passed, 0 warnings, 4 failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintSummary(&buf, tt.pass, tt.warn, tt.fail, tt.fixable)
			output := buf.String()
			for _, s := range tt.contains {
				if !strings.Contains(output, s) {
					t.Errorf("output missing %q: %s", s, output)
				}
			}
		})
	}
}

func TestIsTTY(t *testing.T) {
	var buf bytes.Buffer
	if IsTTY(&buf) {
		t.Error("bytes.Buffer should not be a TTY")
	}
}
