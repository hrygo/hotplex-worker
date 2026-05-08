package checkers

import (
	"context"
	"os"
	"os/exec"
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
		} else {
			fails = append(fails, "python3")
		}
	}

	if deps.MossModelDir != "" {
		if info, err := os.Stat(deps.MossModelDir); err == nil && info.IsDir() {
			msgs = append(msgs, "moss model dir: "+deps.MossModelDir)
		} else {
			fails = append(fails, "moss model dir ("+deps.MossModelDir+")")
		}
	}

	if len(fails) > 0 {
		hints := make([]string, len(fails))
		for i, pkg := range fails {
			hints[i] = installHint(pkg)
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

	// Edge TTS → MP3 → ffmpeg → Opus (used by both Slack and Feishu).
	slackEdge := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSConfig.Enabled
	feishuEdge := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSConfig.Enabled
	if slackEdge || feishuEdge {
		deps.FFmpeg = true
	}

	// MOSS-TTS-Nano sidecar requires python3 + model dir.
	slackMoss := cfg.Messaging.Slack.Enabled && cfg.Messaging.Slack.TTSConfig.Enabled && mossProvider(cfg.Messaging.Slack.TTSProvider)
	feishuMoss := cfg.Messaging.Feishu.Enabled && cfg.Messaging.Feishu.TTSConfig.Enabled && mossProvider(cfg.Messaging.Feishu.TTSProvider)
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

func joinStrings(ss []string) string {
	return strings.Join(ss, ", ")
}

func init() {
	cli.DefaultRegistry.Register(ttsEnvironmentChecker{})
}
