package checkers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type ttsEnvironmentChecker struct{}

func (c ttsEnvironmentChecker) Name() string     { return "tts.runtime" }
func (c ttsEnvironmentChecker) Category() string { return "tts" }

func (c ttsEnvironmentChecker) Check(ctx context.Context) cli.Diagnostic {
	deps := ttsRequirements()
	if !deps.any() {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "TTS not configured or no external dependencies needed",
		}
	}

	var msgs []string
	var fails []string

	if deps.FFmpeg {
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			msgs = append(msgs, "ffmpeg: "+p)
		} else {
			fails = append(fails, "ffmpeg")
		}
	}

	if deps.Python3 {
		if p, err := exec.LookPath("python3"); err == nil {
			msgs = append(msgs, "python3: "+p)

			// MOSS Python package validation (only when python3 is available).
			if deps.MossModelDir != "" {
				if ok, detail := checkMossPythonPackages(ctx, p); ok {
					msgs = append(msgs, "moss python deps")
				} else {
					fails = append(fails, "moss python packages ("+detail+")")
				}
			}
		} else {
			fails = append(fails, "python3")
		}
	}

	if deps.MossModelDir != "" {
		if info, err := os.Stat(deps.MossModelDir); err == nil && info.IsDir() {
			msgs = append(msgs, "moss model dir: "+deps.MossModelDir)

			// Validate entry script exists inside model directory.
			appPath := filepath.Join(deps.MossModelDir, "app_onnx.py")
			if _, err := os.Stat(appPath); err != nil {
				fails = append(fails, "moss entry script ("+appPath+")")
			}
		} else {
			fails = append(fails, "moss model dir ("+deps.MossModelDir+")")
		}
	}

	if len(fails) > 0 {
		hints := make([]string, len(fails))
		for i, pkg := range fails {
			hints[i] = ttsInstallHint(pkg)
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "TTS missing dependencies: " + joinStrings(fails),
			FixHint:  joinStrings(hints),
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "TTS environment ready (" + joinStrings(msgs) + ")",
	}
}

// ttsDeps describes which external tools the TTS pipeline requires.
type ttsDeps struct {
	FFmpeg       bool
	Python3      bool
	MossModelDir string
}

func (d ttsDeps) any() bool {
	return d.FFmpeg || d.Python3 || d.MossModelDir != ""
}

// ttsRequirements determines which TTS dependencies are needed based on config.
func ttsRequirements() ttsDeps {
	if configPath == "" {
		return ttsDeps{}
	}
	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return ttsDeps{}
	}

	var deps ttsDeps

	// Edge TTS → MP3 → ffmpeg → platform format (Feishu: Opus, Slack: MP3).
	slackEdge := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSEnabled
	feishuEdge := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSEnabled
	if slackEdge || feishuEdge {
		deps.FFmpeg = true
	}

	// MOSS-TTS-Nano sidecar requires python3 + model dir.
	slackMoss := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSEnabled && mossProvider(cfg.Messaging.Slack.TTSProvider)
	feishuMoss := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSEnabled && mossProvider(cfg.Messaging.Feishu.TTSProvider)
	if slackMoss || feishuMoss {
		deps.Python3 = true
		deps.FFmpeg = true
		dir := cfg.Messaging.Feishu.MossModelDir
		if slackMoss && cfg.Messaging.Slack.MossModelDir != "" {
			dir = cfg.Messaging.Slack.MossModelDir
		}
		deps.MossModelDir = dir
	}

	return deps
}

func mossProvider(provider string) bool {
	return provider == "moss" || provider == "edge+moss"
}

// checkMossPythonPackages verifies MOSS-TTS-Nano Python dependencies are importable.
func checkMossPythonPackages(ctx context.Context, pyPath string) (bool, string) {
	cmd := exec.CommandContext(ctx, pyPath, "-c",
		"import numpy; import sentencepiece; import onnxruntime; import fastapi; import uvicorn; print('ok')")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out))
	}
	return true, ""
}

// ttsInstallHint returns a platform-specific install hint for a TTS dependency.
// For MOSS-specific items (model dir, entry script, python packages), it returns
// actionable guidance instead of the generic "brew install" pattern.
func ttsInstallHint(pkg string) string {
	switch {
	case strings.HasPrefix(pkg, "moss model dir"):
		return "mkdir -p ~/.hotplex/models/moss-tts-nano && download MOSS scripts + model (see docs)"
	case strings.HasPrefix(pkg, "moss entry script"):
		return "download MOSS-TTS-Nano Python scripts into the model dir (see references/tts.md step 2)"
	case strings.HasPrefix(pkg, "moss python packages"):
		return "pip3 install numpy sentencepiece onnxruntime fastapi uvicorn python-multipart soundfile huggingface_hub"
	default:
		return installHint(pkg)
	}
}

func joinStrings(ss []string) string {
	return strings.Join(ss, ", ")
}

func init() {
	cli.DefaultRegistry.Register(ttsEnvironmentChecker{})
}
