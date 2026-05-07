package checkers

import (
	"context"
	"os/exec"
	"runtime"
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

	if deps.OnnxRuntime {
		if p, err := exec.LookPath("onnxruntime"); err == nil {
			msgs = append(msgs, "onnxruntime: "+p)
		} else if found := findOnnxRuntimeLib(); found != "" {
			msgs = append(msgs, "onnxruntime: "+found)
		} else {
			fails = append(fails, "onnxruntime")
		}
	}

	if deps.EspeakNG {
		if p, err := exec.LookPath("espeak-ng"); err == nil {
			msgs = append(msgs, "espeak-ng: "+p)
		} else {
			fails = append(fails, "espeak-ng")
		}
	}

	if len(fails) > 0 {
		hints := make([]string, len(fails))
		for i, pkg := range fails {
			hints[i] = installHint(ttsPkgName(pkg))
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
	FFmpeg      bool
	OnnxRuntime bool
	EspeakNG    bool
}

func (d ttsDeps) any() bool {
	return d.FFmpeg || d.OnnxRuntime || d.EspeakNG
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

	// Edge TTS → MP3 → ffmpeg → Opus (used by both Slack and Feishu).
	slackEdge := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSConfig.Enabled
	feishuEdge := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSConfig.Enabled
	if slackEdge || feishuEdge {
		deps.FFmpeg = true
	}

	// Kokoro ONNX fallback requires onnxruntime + espeak-ng.
	slackKokoro := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSConfig.Enabled && kokoroProvider(cfg.Messaging.Slack.TTSProvider)
	feishuKokoro := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSConfig.Enabled && kokoroProvider(cfg.Messaging.Feishu.TTSProvider)
	if slackKokoro || feishuKokoro {
		deps.OnnxRuntime = true
		deps.EspeakNG = true
	}

	return deps
}

func kokoroProvider(provider string) bool {
	return provider == "kokoro" || provider == "edge+kokoro"
}

func ttsPkgName(pkg string) string {
	if runtime.GOOS == "linux" && pkg == "onnxruntime" {
		return "libonnxruntime-dev"
	}
	return pkg
}

func findOnnxRuntimeLib() string {
	candidates := []string{}
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/usr/local/lib/libonnxruntime.dylib", "/opt/homebrew/lib/libonnxruntime.dylib"}
	case "linux":
		candidates = []string{"/usr/lib/x86_64-linux-gnu/libonnxruntime.so", "/usr/local/lib/libonnxruntime.so"}
	case "windows":
		candidates = []string{`C:\Program Files\onnxruntime\lib\onnxruntime.dll`}
	}
	for _, p := range candidates {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

func joinStrings(ss []string) string {
	return strings.Join(ss, ", ")
}

func init() {
	cli.DefaultRegistry.Register(ttsEnvironmentChecker{})
}
