package checkers

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
)

type goVersionChecker struct{}

func (c goVersionChecker) Name() string     { return "environment.go_version" }
func (c goVersionChecker) Category() string { return "environment" }
func (c goVersionChecker) Check(ctx context.Context) cli.Diagnostic {
	ver := runtime.Version()
	if strings.HasPrefix(ver, "devel") {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Go development version detected: " + ver,
			Detail:   "Development versions may have unstable APIs",
		}
	}

	minor := parseGoMinor(ver)
	if minor >= 26 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Go version " + ver + " meets requirement (>= go1.26)",
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "Go version " + ver + " does not meet requirement (>= go1.26)",
		FixHint:  "Install Go 1.26 or later from https://go.dev/dl/",
	}
}

func parseGoMinor(ver string) int {
	for _, prefix := range []string{"go1.", "go"} {
		if strings.HasPrefix(ver, prefix) {
			numStr := strings.TrimPrefix(ver, prefix)
			if dotIdx := strings.Index(numStr, "."); dotIdx > 0 {
				numStr = numStr[:dotIdx]
			}
			if n, err := strconv.Atoi(numStr); err == nil {
				return n
			}
			break
		}
	}
	return 0
}

type osArchChecker struct{}

func (c osArchChecker) Name() string     { return "environment.os_arch" }
func (c osArchChecker) Category() string { return "environment" }
func (c osArchChecker) Check(ctx context.Context) cli.Diagnostic {
	supportedOS := runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows"
	supportedArch := runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	if supportedOS && supportedArch {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Platform " + runtime.GOOS + "/" + runtime.GOARCH + " is supported",
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "Platform " + runtime.GOOS + "/" + runtime.GOARCH + " is not supported",
		Detail:   "Supported platforms: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64",
		FixHint:  "Use a supported platform or build from source",
	}
}

type buildToolsChecker struct{}

func (c buildToolsChecker) Name() string     { return "environment.build_tools" }
func (c buildToolsChecker) Category() string { return "environment" }
func (c buildToolsChecker) Check(ctx context.Context) cli.Diagnostic {
	golangciLint, errLint := exec.LookPath("golangci-lint")
	goimports, errImp := exec.LookPath("goimports")

	if errLint == nil && errImp == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Build tools found: golangci-lint (" + golangciLint + "), goimports (" + goimports + ")",
		}
	}

	var missing []string
	if errLint != nil {
		missing = append(missing, "golangci-lint")
	}
	if errImp != nil {
		missing = append(missing, "goimports")
	}

	if len(missing) == 2 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Build tools not found: golangci-lint, goimports",
			FixHint:  "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && go install golang.org/x/tools/cmd/goimports@latest",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  "Build tools partially missing: " + strings.Join(missing, ", "),
		FixHint:  "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && go install golang.org/x/tools/cmd/goimports@latest",
	}
}

func init() {
	cli.DefaultRegistry.Register(goVersionChecker{})
	cli.DefaultRegistry.Register(osArchChecker{})
	cli.DefaultRegistry.Register(buildToolsChecker{})
}
