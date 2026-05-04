package checkers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type sttEnvironmentChecker struct{}

func (c sttEnvironmentChecker) Name() string     { return "stt.runtime" }
func (c sttEnvironmentChecker) Category() string { return "stt" }

func (c sttEnvironmentChecker) Check(ctx context.Context) cli.Diagnostic {
	needsPython, needsFFmpeg := sttRequirements()
	if !needsPython && !needsFFmpeg {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "STT not configured",
		}
	}

	var passed, failed []string
	var hints []string

	if needsPython {
		pyPath, err := exec.LookPath("python3")
		if err == nil {
			passed = append(passed, "python3: "+pyPath)
			if pkgOk, detail := checkPythonPackages(ctx, pyPath); pkgOk {
				passed = append(passed, "funasr-onnx + onnxruntime")
			} else {
				failed = append(failed, "Python STT packages missing: "+detail)
				hints = append(hints, "pip3 install funasr-onnx onnxruntime")
			}
		} else {
			failed = append(failed, "python3 not found in PATH")
			hints = append(hints, installHint("python3"))
		}

		scriptPath := filepath.Join(config.HotplexHome(), "scripts", "stt_server.py")
		if _, err := os.Stat(scriptPath); err != nil {
			passed = append(passed, "STT scripts will be deployed on first gateway start")
		} else {
			passed = append(passed, "STT scripts installed")
		}
	}

	if needsFFmpeg {
		ffPath, err := exec.LookPath("ffmpeg")
		if err == nil {
			passed = append(passed, "ffmpeg: "+ffPath)
		} else {
			failed = append(failed, "ffmpeg not found in PATH")
			hints = append(hints, installHint("ffmpeg"))
		}
	}

	status := cli.StatusPass
	if len(failed) > 0 {
		status = cli.StatusFail
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   status,
		Message:  sttSummary(passed, failed),
		Detail:   strings.Join(passed, "; "),
		FixHint:  strings.Join(hints, "\n"),
	}
}

// sttRequirements determines which STT dependencies are needed based on config.
func sttRequirements() (needsPython, needsFFmpeg bool) {
	if configPath == "" {
		return false, false
	}
	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return false, false
	}
	if cfg.Messaging.Slack.Enabled {
		py, ff := providerDeps(cfg.Messaging.Slack.Provider)
		needsPython = needsPython || py
		needsFFmpeg = needsFFmpeg || ff
	}
	if cfg.Messaging.Feishu.Enabled {
		py, ff := providerDeps(cfg.Messaging.Feishu.Provider)
		needsPython = needsPython || py
		needsFFmpeg = needsFFmpeg || ff
	}
	return
}

func providerDeps(provider string) (python, ffmpeg bool) {
	switch provider {
	case config.STTProviderLocal:
		return true, false
	case config.STTProviderFeishu:
		return false, true
	case config.STTProviderFeishuLocal:
		return true, true
	default:
		return false, false
	}
}

func checkPythonPackages(ctx context.Context, pyPath string) (bool, string) {
	cmd := exec.CommandContext(ctx, pyPath, "-c", "import funasr_onnx; import onnxruntime; print('ok')")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out))
	}
	return true, ""
}

func sttSummary(passed, failed []string) string {
	if len(failed) == 0 {
		return fmt.Sprintf("STT environment ready (%d checks passed)", len(passed))
	}
	return fmt.Sprintf("STT environment incomplete: %s", strings.Join(failed, "; "))
}

func installHint(pkg string) string {
	if runtime.GOOS == "darwin" {
		return "brew install " + pkg
	}
	return "apt install " + pkg
}

func init() {
	cli.DefaultRegistry.Register(sttEnvironmentChecker{})
}
