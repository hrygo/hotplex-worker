package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/hrygo/hotplex/internal/cli/output"
)

// ANSI escape codes for TTY output.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiCyan  = "\033[36m"
	ansiDim   = "\033[2m"
	ansiGreen = "\033[32m"
	ansiRed   = "\033[31m"
)

//go:embed banner_art.txt
var bannerArt string

//go:generate go run ../../scripts/gen_banner.go -cols 80

// BuildInfo holds compile-time and runtime metadata.
type BuildInfo struct {
	Version   string
	BuildTime string
	GoVersion string
	OS        string
	Arch      string
}

func newBuildInfo() BuildInfo {
	return BuildInfo{
		Version:   versionString(),
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// RuntimeStatus holds component state for the status panel.
type RuntimeStatus struct {
	GatewayAddr  string
	AdminAddr    string
	WebChatAddr  string
	DBPath       string
	PoolMax      int
	PoolIdle     int
	Adapters     []AdapterStatus
	RetryEnabled bool
	RetryMax     int
	RetryDelay   string
}

// AdapterStatus reports a single messaging adapter's state.
type AdapterStatus struct {
	Name    string
	Started bool
}

// writeAll writes strings to w, ignoring errors (banner output is best-effort).
func writeAll(w io.Writer, lines ...string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

func printStartupBanner(out *os.File, info BuildInfo, s RuntimeStatus, configPath string) {
	tty := output.IsTTY(out)

	bold := func(s string) string {
		if tty {
			return ansiBold + s + ansiReset
		}
		return s
	}
	cyan := func(s string) string {
		if tty {
			return ansiCyan + s + ansiReset
		}
		return s
	}
	dim := func(s string) string {
		if tty {
			return ansiDim + s + ansiReset
		}
		return s
	}
	green := func(v string) string {
		if tty {
			return ansiGreen + v + ansiReset
		}
		return v
	}
	red := func(v string) string {
		if tty {
			return ansiRed + v + ansiReset
		}
		return v
	}

	pad := func(label, value string) string {
		return fmt.Sprintf("  %-11s%s", bold(label), value)
	}

	var lines []string

	lines = append(lines, "", cyan(bannerArt), "")

	// Build info
	lines = append(lines,
		pad("Version", cyan(info.Version)),
		pad("Build", info.BuildTime),
		pad("Go", fmt.Sprintf("%s · %s/%s", info.GoVersion, info.OS, info.Arch)),
	)
	if configPath != "" {
		lines = append(lines, pad("Config", configPath))
	}

	// Separator between build info and runtime status
	lines = append(lines, dim("  ─────────────────────────────────────"), "")

	// Runtime status
	lines = append(lines, pad("Gateway", "http://"+s.GatewayAddr))
	if s.AdminAddr != "" {
		lines = append(lines, pad("Admin", "http://"+s.AdminAddr))
	}
	if s.WebChatAddr != "" {
		lines = append(lines, pad("WebChat", "http://"+s.WebChatAddr))
	}
	lines = append(lines, pad("Database", s.DBPath+" (SQLite)"))
	lines = append(lines, pad("Pool", fmt.Sprintf("%d sessions / %d idle per user", s.PoolMax, s.PoolIdle)))

	if len(s.Adapters) > 0 {
		var parts []string
		for _, a := range s.Adapters {
			if a.Started {
				parts = append(parts, fmt.Sprintf("%s %s", a.Name, green("✓")))
			} else {
				parts = append(parts, red(fmt.Sprintf("%s ✗", a.Name)))
			}
		}
		lines = append(lines, pad("Adapters", strings.Join(parts, "  ")))
	}

	if s.RetryEnabled {
		lines = append(lines, pad("LLM Retry", green(fmt.Sprintf("✓ %d retries, %s base delay", s.RetryMax, s.RetryDelay))))
	}

	lines = append(lines, "")
	writeAll(out, lines...)
}
