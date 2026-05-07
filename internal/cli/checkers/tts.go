package checkers

import (
	"context"
	"os/exec"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type ttsEnvironmentChecker struct{}

func (c ttsEnvironmentChecker) Name() string     { return "tts.runtime" }
func (c ttsEnvironmentChecker) Category() string { return "tts" }

func (c ttsEnvironmentChecker) Check(ctx context.Context) cli.Diagnostic {
	needsFFmpeg := ttsRequirements()
	if !needsFFmpeg {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "TTS not configured or no external dependencies needed",
		}
	}

	ffPath, err := exec.LookPath("ffmpeg")
	if err == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "TTS environment ready (ffmpeg: " + ffPath + ")",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "TTS requires ffmpeg for MP3→Opus conversion, but ffmpeg not found in PATH",
		FixHint:  installHint("ffmpeg"),
	}
}

// ttsRequirements determines whether TTS needs ffmpeg based on config.
// Both Slack and Feishu TTS pipelines use Edge TTS → MP3 → ffmpeg Opus conversion.
func ttsRequirements() bool {
	if configPath == "" {
		return false
	}
	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return false
	}
	if cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSConfig.Enabled {
		return true
	}
	if cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSConfig.Enabled {
		return true
	}
	return false
}

func init() {
	cli.DefaultRegistry.Register(ttsEnvironmentChecker{})
}
