package checkers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

// agentConfigSuffixChecker detects deprecated platform-suffix files
// (e.g., SOUL.slack.md) in the agent-configs directory and suggests
// migration to the new directory-based layout.
type agentConfigSuffixChecker struct {
	dir string // override for testing; defaults to config.HotplexHome()/agent-configs
}

func (c agentConfigSuffixChecker) Name() string     { return "agent.suffix_deprecated" }
func (c agentConfigSuffixChecker) Category() string { return "agent_config" }

func (c agentConfigSuffixChecker) scanDir() string {
	if c.dir != "" {
		return c.dir
	}
	return filepath.Join(config.HotplexHome(), "agent-configs")
}

func (c agentConfigSuffixChecker) Check(_ context.Context) cli.Diagnostic {
	dir := c.scanDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusWarn,
				Message:  "Agent config directory does not exist",
				FixHint:  fmt.Sprintf("Create it: mkdir -p %s", dir),
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot read agent config directory: " + err.Error(),
		}
	}

	platforms := []string{"slack", "feishu", "webchat"}
	var deprecated []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for _, p := range platforms {
			suffix := "." + p + ".md"
			if strings.HasSuffix(name, suffix) {
				deprecated = append(deprecated, name)
			}
		}
	}

	if len(deprecated) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "No deprecated platform-suffix files found",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("Deprecated suffix files: %s", strings.Join(deprecated, ", ")),
		FixHint: fmt.Sprintf("Move to directory layout:\n  mkdir -p %s/slack && mv %s %s/slack/SOUL.md",
			dir, filepath.Join(dir, deprecated[0]), dir),
	}
}

func init() {
	cli.DefaultRegistry.Register(agentConfigSuffixChecker{})
}
