package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
)

type JSONReport struct {
	Version     string           `json:"version"`
	Timestamp   string           `json:"timestamp"`
	Summary     JSONSummary      `json:"summary"`
	Diagnostics []cli.Diagnostic `json:"diagnostics"`
}

type JSONSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

func NewJSONReport(version string, diags []cli.Diagnostic) JSONReport {
	var pass, warn, fail int
	for _, d := range diags {
		switch d.Status {
		case cli.StatusPass:
			pass++
		case cli.StatusWarn:
			warn++
		case cli.StatusFail:
			fail++
		}
	}

	return JSONReport{
		Version:   version,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary: JSONSummary{
			Pass: pass,
			Warn: warn,
			Fail: fail,
		},
		Diagnostics: diags,
	}
}

func WriteJSON(out io.Writer, report JSONReport) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
