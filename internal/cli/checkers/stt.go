package checkers

import (
	"context"
	"os/exec"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
)

type sttRuntimeChecker struct{}

func (c sttRuntimeChecker) Name() string     { return "stt.runtime" }
func (c sttRuntimeChecker) Category() string { return "stt" }
func (c sttRuntimeChecker) Check(ctx context.Context) cli.Diagnostic {
	// Try to find python3
	pyPath, err := exec.LookPath("python3")
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "python3 not found in PATH",
			FixHint:  "Install Python 3.9+ and ensure 'python3' command is available.",
		}
	}

	// Check for necessary libraries
	cmd := exec.CommandContext(ctx, pyPath, "-c", "import funasr_onnx; import onnxruntime; print('ok')")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "STT dependencies missing: " + strings.TrimSpace(string(output)),
			FixHint:  "Run: pip install funasr-onnx onnxruntime",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "STT runtime (python3 + dependencies) is ready",
	}
}

func init() {
	cli.DefaultRegistry.Register(sttRuntimeChecker{})
}
