package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestNewJSONReport(t *testing.T) {
	diags := []cli.Diagnostic{
		{
			Name:     "test.pass1",
			Category: "test",
			Status:   cli.StatusPass,
			Message:  "Pass 1",
		},
		{
			Name:     "test.pass2",
			Category: "test",
			Status:   cli.StatusPass,
			Message:  "Pass 2",
		},
		{
			Name:     "test.warn",
			Category: "test",
			Status:   cli.StatusWarn,
			Message:  "Warning",
			Detail:   "Some detail",
		},
		{
			Name:     "test.fail1",
			Category: "test",
			Status:   cli.StatusFail,
			Message:  "Fail 1",
			FixHint:  "Fix it",
		},
		{
			Name:     "test.fail2",
			Category: "test",
			Status:   cli.StatusFail,
			Message:  "Fail 2",
		},
	}

	report := NewJSONReport("0.1.0", diags)

	if report.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", report.Version, "0.1.0")
	}

	_, err := time.Parse(time.RFC3339, report.Timestamp)
	if err != nil {
		t.Errorf("Timestamp %q is not RFC3339: %v", report.Timestamp, err)
	}

	if report.Summary.Pass != 2 {
		t.Errorf("Summary.Pass = %d, want %d", report.Summary.Pass, 2)
	}
	if report.Summary.Warn != 1 {
		t.Errorf("Summary.Warn = %d, want %d", report.Summary.Warn, 1)
	}
	if report.Summary.Fail != 2 {
		t.Errorf("Summary.Fail = %d, want %d", report.Summary.Fail, 2)
	}

	if len(report.Diagnostics) != 5 {
		t.Errorf("len(Diagnostics) = %d, want %d", len(report.Diagnostics), 5)
	}
}

func TestWriteJSON(t *testing.T) {
	diags := []cli.Diagnostic{
		{
			Name:     "test.simple",
			Category: "test",
			Status:   cli.StatusPass,
			Message:  "Simple test",
		},
	}

	report := NewJSONReport("0.1.0", diags)

	var buf bytes.Buffer
	err := WriteJSON(&buf, report)
	if err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"version": "0.1.0"`) {
		t.Errorf("output missing version: %s", output)
	}
	if !strings.Contains(output, `"pass": 1`) {
		t.Errorf("output missing pass count: %s", output)
	}
	if !strings.Contains(output, `"category": "test"`) {
		t.Errorf("output missing category: %s", output)
	}

	var decoded JSONReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if decoded.Version != "0.1.0" {
		t.Errorf("decoded.Version = %q, want %q", decoded.Version, "0.1.0")
	}
	if decoded.Summary.Pass != 1 {
		t.Errorf("decoded.Summary.Pass = %d, want %d", decoded.Summary.Pass, 1)
	}
	if len(decoded.Diagnostics) != 1 {
		t.Errorf("len(decoded.Diagnostics) = %d, want %d", len(decoded.Diagnostics), 1)
	}
}

func TestJSONReportStructure(t *testing.T) {
	diags := []cli.Diagnostic{
		{
			Name:     "test.allfields",
			Category: "test",
			Status:   cli.StatusWarn,
			Message:  "All fields",
			Detail:   "Extra detail",
			FixHint:  "Fix suggestion",
		},
	}

	report := NewJSONReport("0.2.0", diags)

	var buf bytes.Buffer
	if err := WriteJSON(&buf, report); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	// Verify the JSON structure matches the spec
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if v, ok := result["version"].(string); !ok || v != "0.2.0" {
		t.Errorf("missing or invalid version field")
	}
	if _, ok := result["timestamp"].(string); !ok {
		t.Errorf("missing timestamp string field")
	}
	if summary, ok := result["summary"].(map[string]interface{}); ok {
		if _, ok := summary["pass"].(float64); !ok {
			t.Errorf("missing pass number in summary")
		}
		if _, ok := summary["warn"].(float64); !ok {
			t.Errorf("missing warn number in summary")
		}
		if _, ok := summary["fail"].(float64); !ok {
			t.Errorf("missing fail number in summary")
		}
	} else {
		t.Errorf("missing summary object")
	}
	if diagnostics, ok := result["diagnostics"].([]interface{}); ok {
		if len(diagnostics) != 1 {
			t.Errorf("diagnostics length = %d, want 1", len(diagnostics))
		}
	} else {
		t.Errorf("missing diagnostics array")
	}
}
