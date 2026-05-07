package onboard

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/hrygo/hotplex/configs"
)

// ConfigTemplateOptions carries wizard inputs that drive config generation.
type ConfigTemplateOptions struct {
	SlackEnabled        bool
	SlackDMPolicy       string
	SlackGroupPolicy    string
	SlackRequireMention *bool
	SlackAllowFrom      []string

	FeishuEnabled        bool
	FeishuDMPolicy       string
	FeishuGroupPolicy    string
	FeishuRequireMention *bool
	FeishuAllowFrom      []string

	WorkerType         string
	KeptPlatforms      map[string]bool
	ExistingConfigPath string
}

// DefaultConfigYAML returns the embedded config.yaml as-is (no mutations).
func DefaultConfigYAML() string {
	return string(configs.EmbeddedConfigYAML)
}

// BuildConfigYAML returns a config.yaml with wizard mutations applied.
// When no mutations are needed (both platforms disabled with defaults), the
// embedded file is returned verbatim to preserve comments and formatting.
// Otherwise, the file is parsed into a YAML AST, mutated, and re-serialised
// (note: yaml.Marshal drops comments from the original).
func BuildConfigYAML(opts ConfigTemplateOptions) (string, error) {
	if !needsMutation(opts) {
		return string(configs.EmbeddedConfigYAML), nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal(configs.EmbeddedConfigYAML, &root); err != nil {
		panic("embedded config.yaml parse error: " + err.Error())
	}

	// Preserve existing platform blocks when the user chose to keep them.
	if opts.ExistingConfigPath != "" && len(opts.KeptPlatforms) > 0 {
		data, err := os.ReadFile(opts.ExistingConfigPath)
		if err != nil {
			return "", fmt.Errorf("read existing config %q for block preservation: %w", opts.ExistingConfigPath, err)
		}
		var existing yaml.Node
		if err := yaml.Unmarshal(data, &existing); err != nil {
			return "", fmt.Errorf("parse existing config %q: %w", opts.ExistingConfigPath, err)
		}
		for platform := range opts.KeptPlatforms {
			replaceBlock(&root, &existing, "messaging", platform)
		}
	}

	// Mutate platform settings for non-kept platforms.
	msg := lookupPath(&root, "messaging")
	if msg != nil {
		if !opts.KeptPlatforms["slack"] {
			if n := lookupKey(msg, "slack"); n != nil {
				applyPlatformOpts(n, opts.SlackEnabled, opts.SlackDMPolicy, opts.SlackGroupPolicy, opts.SlackRequireMention, opts.SlackAllowFrom)
			}
		}
		if !opts.KeptPlatforms["feishu"] {
			if n := lookupKey(msg, "feishu"); n != nil {
				applyPlatformOpts(n, opts.FeishuEnabled, opts.FeishuDMPolicy, opts.FeishuGroupPolicy, opts.FeishuRequireMention, opts.FeishuAllowFrom)
			}
		}
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return "", fmt.Errorf("marshal config after mutations (slack=%v feishu=%v kept=%v): %w",
			opts.SlackEnabled, opts.FeishuEnabled, opts.KeptPlatforms, err)
	}
	return string(out), nil
}

func applyPlatformOpts(n *yaml.Node, enabled bool, dmPolicy, groupPolicy string, requireMention *bool, allowFrom []string) {
	setBool(n, "enabled", enabled)
	setScalar(n, "dm_policy", defaultStr(dmPolicy, "allowlist"))
	setScalar(n, "group_policy", defaultStr(groupPolicy, "allowlist"))
	setBool(n, "require_mention", defaultBool(requireMention, true))
	setStringList(n, "allow_from", allowFrom)
}

// needsMutation returns true when the wizard options differ from the embedded defaults.
func needsMutation(opts ConfigTemplateOptions) bool {
	if opts.SlackEnabled || opts.FeishuEnabled {
		return true
	}
	if len(opts.KeptPlatforms) > 0 {
		return true
	}
	if len(opts.SlackAllowFrom) > 0 || len(opts.FeishuAllowFrom) > 0 {
		return true
	}
	if opts.SlackDMPolicy != "" || opts.SlackGroupPolicy != "" {
		return true
	}
	if opts.FeishuDMPolicy != "" || opts.FeishuGroupPolicy != "" {
		return true
	}
	if opts.SlackRequireMention != nil || opts.FeishuRequireMention != nil {
		return true
	}
	return false
}

func defaultStr(s, def string) string { //nolint:unparam // def varies if more usage added
	if s == "" {
		return def
	}
	return s
}

func defaultBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// GenerateSecret produces a 32-byte cryptographic secret encoded as base64.
func GenerateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(b)
}
