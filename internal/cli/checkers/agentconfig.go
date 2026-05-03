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
	cli.DefaultRegistry.Register(agentConfigDirChecker{})
	cli.DefaultRegistry.Register(agentConfigGlobalFilesChecker{})
}

// agentConfigDirChecker validates the agent-configs directory structure,
// ensuring platform subdirectories contain only recognized config files.
type agentConfigDirChecker struct {
	dir string // override for testing; defaults to config.HotplexHome()/agent-configs
}

func (c agentConfigDirChecker) Name() string     { return "agent.directory_structure" }
func (c agentConfigDirChecker) Category() string { return "agent_config" }

func (c agentConfigDirChecker) scanDir() string {
	if c.dir != "" {
		return c.dir
	}
	return filepath.Join(config.HotplexHome(), "agent-configs")
}

var validConfigFiles = map[string]bool{
	"SOUL.md": true, "AGENTS.md": true, "SKILLS.md": true,
	"USER.md": true, "MEMORY.md": true,
}

// ignoredFiles are non-config files allowed in any directory without warning.
var ignoredFiles = map[string]bool{
	".gitkeep": true, "README.md": true, ".DS_Store": true,
}

func (c agentConfigDirChecker) Check(_ context.Context) cli.Diagnostic {
	dir := c.scanDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot read agent config directory: " + err.Error(),
		}
	}

	var warnings []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Platform directory (slack/, feishu/, webchat/, etc.)
		platformDir := filepath.Join(dir, e.Name())
		c.checkSubdir(platformDir, e.Name(), &warnings)
	}

	if len(warnings) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Agent config directory structure is valid",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("Unrecognized files in agent config: %s", strings.Join(warnings, ", ")),
		FixHint:  "Remove or relocate non-config .md files from platform subdirectories. Valid names: SOUL.md, AGENTS.md, SKILLS.md, USER.md, MEMORY.md",
	}
}

func (c agentConfigDirChecker) checkSubdir(platformDir, platformName string, warnings *[]string) {
	entries, err := os.ReadDir(platformDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			// Bot-level subdirectory — validate its contents too
			botDir := filepath.Join(platformDir, e.Name())
			c.checkSubdir(botDir, platformName+"/"+e.Name(), warnings)
			continue
		}
		name := e.Name()
		if validConfigFiles[name] || ignoredFiles[name] {
			continue
		}
		// Non-.md files or unrecognized .md files in platform/bot directories
		if strings.HasSuffix(name, ".md") {
			*warnings = append(*warnings, filepath.Join(platformName, name))
		}
	}
}

// agentConfigGlobalFilesChecker detects config files at the global level
// that lack a per-bot directory, meaning they are shared across all bots.
type agentConfigGlobalFilesChecker struct {
	dir string
}

func (c agentConfigGlobalFilesChecker) Name() string     { return "agent.global_files" }
func (c agentConfigGlobalFilesChecker) Category() string { return "agent_config" }

func (c agentConfigGlobalFilesChecker) scanDir() string {
	if c.dir != "" {
		return c.dir
	}
	return filepath.Join(config.HotplexHome(), "agent-configs")
}

func (c agentConfigGlobalFilesChecker) Check(_ context.Context) cli.Diagnostic {
	dir := c.scanDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "Agent config directory not yet created"}
		}
		return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusWarn, Message: "Cannot read: " + err.Error()}
	}

	var global []string
	for _, e := range entries {
		if !e.IsDir() && validConfigFiles[e.Name()] {
			global = append(global, e.Name())
		}
	}
	if len(global) == 0 {
		return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "No global agent-config files (using per-bot configs)"}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("Global config files apply to all bots: %s", strings.Join(global, ", ")),
		Detail:   dir,
		FixHint:  fmt.Sprintf("Move to per-bot directory for isolation:\n  mkdir -p %s/slack/<BOT_ID>\n  mv %s %s/slack/<BOT_ID>/", dir, filepath.Join(dir, global[0]), dir),
	}
}
