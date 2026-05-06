package onboard

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/cli/checkers"
	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/service"
)

type messagingPlatformConfig struct {
	enabled        bool
	kept           bool
	dmPolicy       string
	groupPolicy    string
	requireMention bool
	allowFrom      []string
	credentials    map[string]string
}

type WizardOptions struct {
	ConfigPath        string
	NonInteractive    bool
	Force             bool
	EnableSlack       bool
	EnableFeishu      bool
	SlackAllowFrom    []string
	SlackDMPolicy     string
	SlackGroupPolicy  string
	FeishuAllowFrom   []string
	FeishuDMPolicy    string
	FeishuGroupPolicy string
	InstallService    bool
	ServiceLevel      string
	Version           string // injected from build ldflags via onboard.go
}

// ExistingConfig holds detected existing configuration state.
type ExistingConfig struct {
	ConfigExists  bool
	EnvExists     bool
	SlackEnabled  bool
	FeishuEnabled bool
	SlackCreds    bool
	FeishuCreds   bool
	ConfigPath    string
	EnvPath       string
}

func (ec *ExistingConfig) HasAny() bool      { return ec.ConfigExists || ec.EnvExists }
func (ec *ExistingConfig) SlackReady() bool  { return ec.SlackEnabled && ec.SlackCreds }
func (ec *ExistingConfig) FeishuReady() bool { return ec.FeishuEnabled && ec.FeishuCreds }
func (ec *ExistingConfig) PlatformConfigured(platform string) bool {
	switch strings.ToLower(platform) {
	case "slack":
		return ec.SlackEnabled || ec.SlackCreds
	case "feishu":
		return ec.FeishuEnabled || ec.FeishuCreds
	}
	return false
}

func detectExistingConfig(configPath, envPath string) *ExistingConfig {
	ec := &ExistingConfig{ConfigPath: configPath, EnvPath: envPath}

	if data, err := os.ReadFile(configPath); err == nil {
		ec.ConfigExists = true
		content := string(data)
		ec.SlackEnabled = isPlatformEnabled(content, "slack")
		ec.FeishuEnabled = isPlatformEnabled(content, "feishu")
	}

	if data, err := os.ReadFile(envPath); err == nil {
		ec.EnvExists = true
		content := string(data)
		ec.SlackCreds = hasEnvValue(content, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN")
		ec.FeishuCreds = hasEnvValue(content, "HOTPLEX_MESSAGING_FEISHU_APP_ID")
	}

	return ec
}

func isPlatformEnabled(yamlContent, platform string) bool {
	markers := []string{
		platform + ":\n  enabled: true",
		platform + ":\n    enabled: true",
	}
	for _, m := range markers {
		if strings.Contains(yamlContent, m) {
			return true
		}
	}
	return false
}

func hasEnvValue(content, key string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		prefix := key + "="
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			return true
		}
	}
	return false
}

type WizardResult struct {
	ConfigPath     string
	EnvPath        string
	Steps          []StepResult
	Action         string   // "keep" or "reconfigure"
	AgentConfigNew []string // files created by this run
}

type StepResult struct {
	Name   string
	Status string
	Detail string
}

// wizardContext carries shared state between wizard steps.
type wizardContext struct {
	opts     WizardOptions
	existing *ExistingConfig
	envPath  string
	reader   *bufio.Reader // nil in non-interactive mode

	// Step outputs — pre-fillable from existing config when step is skipped
	jwtSecret     string
	adminToken    string
	workerType    string
	slackCfg      messagingPlatformConfig
	feishuCfg     messagingPlatformConfig
	configCreated bool
}

// runMode determines how the wizard handles existing configuration.
type runMode int

const (
	modeFullConfig  runMode = iota // fresh or forced configuration
	modeKeep                       // keep existing config
	modeSelectSteps                // incrementally reconfigure selected steps
)

// selectableStep represents a user-selectable reconfiguration step.
type selectableStep struct {
	name     string
	label    string // display label for selection UI
	selected bool
	run      func(wctx *wizardContext) StepResult
	prefill  func(wctx *wizardContext) // populate outputs from existing config
}

func Run(ctx context.Context, opts WizardOptions) (*WizardResult, error) {
	result := &WizardResult{
		ConfigPath: opts.ConfigPath,
		EnvPath:    filepath.Join(filepath.Dir(opts.ConfigPath), ".env"),
	}

	displayBanner(opts.Version)

	// Step 1: Environment pre-check (always runs, fatal on failure)
	result.add(stepEnvPreCheck())
	if result.hasFail() {
		return result, fmt.Errorf("environment pre-check failed, resolve errors above before continuing")
	}

	// Step 2: Detect existing config and determine run mode
	existing := detectExistingConfig(opts.ConfigPath, result.EnvPath)
	mode := modeFullConfig
	if !opts.Force && existing.HasAny() {
		if opts.NonInteractive {
			mode = modeKeep
		} else {
			displayExistingConfig(existing)
			mode = promptRunMode()
		}
	}

	// Fast return for "keep" mode
	if mode == modeKeep {
		result.Action = "keep"
		detail := "kept existing configuration"
		if opts.NonInteractive {
			detail += " (non-interactive)"
		}
		result.add(StepResult{Name: "onboard", Status: "pass", Detail: detail})
		s, created := stepAgentConfig()
		result.add(s)
		result.AgentConfigNew = created
		result.add(stepVerify(opts.ConfigPath))
		return result, nil
	}

	if mode == modeFullConfig {
		opts.Force = true
		fmt.Fprintln(os.Stderr, "  → Reconfiguring...")
	}

	// Step 3: Build wizard context
	wctx := &wizardContext{
		opts:     opts,
		existing: existing,
		envPath:  result.EnvPath,
	}
	if !opts.NonInteractive {
		wctx.reader = bufio.NewReader(os.Stdin)
	}

	// Step 4: Define configurable steps
	steps := wctx.buildSteps()

	// Step 5: Pre-fill all from existing config
	for i := range steps {
		steps[i].prefill(wctx)
	}

	// Step 6: If select steps mode, prompt user to choose
	if mode == modeSelectSteps {
		choices := promptStepSelection(wctx.reader, steps)
		for i := range steps {
			steps[i].selected = choices[i]
		}
	}

	// Step 7: Run selected configurable steps with progress
	totalSteps := len(steps) + 3 // +3 mandatory: config_gen, write_config, agent_config+verify
	current := 1
	for _, step := range steps {
		if !step.selected {
			result.add(StepResult{Name: step.name, Status: "skip", Detail: "unchanged"})
			continue
		}
		displayStepProgress(current, totalSteps, step.label)
		result.add(step.run(wctx))
		current++
	}

	// Step 8: Mandatory steps (always run)
	if !opts.NonInteractive {
		wctx.displayPreview()
	}
	displayStepProgress(current, totalSteps, "Generate Configuration")
	s5, configCreated := stepConfigGen(wctx.opts, wctx.buildTemplateOpts())
	wctx.configCreated = configCreated
	result.add(s5)
	if s5.Status == "fail" {
		return result, fmt.Errorf("config generation failed: %s", s5.Detail)
	}
	current++

	displayStepProgress(current, totalSteps, "Write Environment")
	s6 := stepWriteConfig(wctx.envPath, wctx.jwtSecret, wctx.adminToken, wctx.slackCfg, wctx.feishuCfg, configCreated, wctx.opts)
	result.add(s6)
	if s6.Status == "fail" {
		return result, fmt.Errorf("config write failed: %s", s6.Detail)
	}
	current++

	s, created := stepAgentConfig()
	result.add(s)
	result.AgentConfigNew = created

	result.add(stepSTTCheck(opts.ConfigPath, wctx.reader))

	displayStepProgress(current, totalSteps, "Verify")
	result.add(stepVerify(opts.ConfigPath))

	result.Action = "reconfigure"
	return result, nil
}

func toPtr[T any](v T) *T { return &v }

func (r *WizardResult) add(s StepResult) { r.Steps = append(r.Steps, s) }

func (r *WizardResult) hasFail() bool {
	for _, s := range r.Steps {
		if s.Status == "fail" {
			return true
		}
	}
	return false
}

func messagingDetail(slack, feishu bool) string {
	switch {
	case slack && feishu:
		return "slack+feishu"
	case slack:
		return "slack"
	case feishu:
		return "feishu"
	default:
		return "non-interactive"
	}
}

func buildPlatformNonInteractive(enabled bool, dmPolicy, groupPolicy string, allowFrom []string) messagingPlatformConfig {
	return messagingPlatformConfig{
		enabled:        enabled,
		dmPolicy:       defaultStr(dmPolicy, "allowlist"),
		groupPolicy:    defaultStr(groupPolicy, "allowlist"),
		requireMention: true,
		allowFrom:      allowFrom,
		credentials:    map[string]string{},
	}
}

// ─── Display helpers ─────────────────────────────────────────────────────────

func displayBanner(version string) {
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(os.Stderr, "\n  %s\n", output.Bold("HotPlex Worker Gateway — Setup Wizard"))
	fmt.Fprintf(os.Stderr, "  %s\n", output.Dim("AI Coding Agent Gateway  "+version))
	fmt.Fprintln(os.Stderr, "  "+strings.Repeat("─", 45))
	fmt.Fprintln(os.Stderr, "")
}

func displayExistingConfig(ec *ExistingConfig) {
	fmt.Fprintln(os.Stderr, output.NoteBox("Existing Configuration Detected", ""))
	if ec.ConfigExists {
		fmt.Fprintf(os.Stderr, "    Config: %s\n", output.Green(ec.ConfigPath))
		if ec.SlackEnabled {
			var status string
			if ec.SlackCreds {
				status = output.StatusSymbol("pass") + " " + output.Green("configured")
			} else {
				status = output.StatusSymbol("warn") + " " + output.Yellow("missing token in .env")
			}
			fmt.Fprintf(os.Stderr, "    Slack:  enabled (%s)\n", status)
		}
		if ec.FeishuEnabled {
			var status string
			if ec.FeishuCreds {
				status = output.StatusSymbol("pass") + " " + output.Green("configured")
			} else {
				status = output.StatusSymbol("warn") + " " + output.Yellow("missing credentials in .env")
			}
			fmt.Fprintf(os.Stderr, "    Feishu: enabled (%s)\n", status)
		}
		if !ec.SlackEnabled && !ec.FeishuEnabled {
			fmt.Fprintln(os.Stderr, "    Platforms: none enabled")
		}
	}
	if ec.EnvExists && !ec.ConfigExists {
		fmt.Fprintf(os.Stderr, "    Env file: %s\n", output.Yellow(ec.EnvPath+" (config file missing)"))
	}
	fmt.Fprintln(os.Stderr, "")
}

// ─── Run mode selection ──────────────────────────────────────────────────

func promptRunMode() runMode {
	fmt.Fprintln(os.Stderr, "  What would you like to do?")
	fmt.Fprintln(os.Stderr, "    1) Keep all — skip to verify")
	fmt.Fprintln(os.Stderr, "    2) Reconfigure everything — full reset")
	fmt.Fprintln(os.Stderr, "    3) Select steps — choose what to change")
	fmt.Fprintf(os.Stderr, "\n  Select [1]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	switch line {
	case "2":
		return modeFullConfig
	case "3":
		return modeSelectSteps
	default:
		return modeKeep
	}
}

func promptStepSelection(reader *bufio.Reader, steps []selectableStep) []bool {
	fmt.Fprint(os.Stderr, output.SectionHeader("Select Steps to Reconfigure"))
	choices := make([]bool, len(steps))
	for i, step := range steps {
		choices[i] = promptYesNo(reader, step.label)
	}
	return choices
}

// ─── Step progress indicator ────────────────────────────────────────────

func displayStepProgress(current, total int, label string) {
	fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", current, total, output.Bold(label))
}

// ─── wizardContext methods ───────────────────────────────────────────────

func (wctx *wizardContext) buildSteps() []selectableStep {
	return []selectableStep{
		{
			name: "required_config", label: "Secrets & Worker Type", selected: true,
			run: func(wctx *wizardContext) StepResult { return wctx.runRequiredConfig() },
			prefill: func(wctx *wizardContext) {
				wctx.jwtSecret = readExistingEnvValue(wctx.envPath, "HOTPLEX_JWT_SECRET")
				wctx.adminToken = readExistingEnvValue(wctx.envPath, "HOTPLEX_ADMIN_TOKEN_1")
				wctx.workerType = readExistingConfigValue(wctx.opts.ConfigPath, "worker.type")
				if wctx.workerType == "" {
					wctx.workerType = "claude_code"
				}
			},
		},
		{
			name: "worker_dep", label: "Worker Dependency", selected: true,
			run:     func(wctx *wizardContext) StepResult { return wctx.runWorkerDep() },
			prefill: func(wctx *wizardContext) {},
		},
		{
			name: "messaging", label: "Messaging Platform", selected: true,
			run: func(wctx *wizardContext) StepResult { return wctx.runMessaging() },
			prefill: func(wctx *wizardContext) {
				wctx.slackCfg = prefillPlatformConfig("slack", wctx.existing, wctx.envPath, []string{
					"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN",
					"HOTPLEX_MESSAGING_SLACK_APP_TOKEN",
				})
				wctx.feishuCfg = prefillPlatformConfig("feishu", wctx.existing, wctx.envPath, []string{
					"HOTPLEX_MESSAGING_FEISHU_APP_ID",
					"HOTPLEX_MESSAGING_FEISHU_APP_SECRET",
				})
			},
		},
		{
			name: "service_install", label: "System Service", selected: true,
			run:     func(wctx *wizardContext) StepResult { return stepServiceInstall(wctx.reader, wctx.opts) },
			prefill: func(wctx *wizardContext) {},
		},
	}
}

func (wctx *wizardContext) runRequiredConfig() StepResult {
	if wctx.opts.NonInteractive {
		wctx.jwtSecret = GenerateSecret()
		wctx.adminToken = GenerateSecret()
		wctx.workerType = "claude_code"
		return StepResult{Name: "required_config", Status: "pass", Detail: "auto-generated secrets, worker=claude_code"}
	}
	fmt.Fprint(os.Stderr, output.SectionHeader("Required Configuration"))
	wctx.jwtSecret = prompt(wctx.reader, "JWT secret (enter to auto-generate)")
	if wctx.jwtSecret == "" {
		wctx.jwtSecret = GenerateSecret()
		fmt.Fprintln(os.Stderr, "  → Generated JWT secret")
	}
	wctx.adminToken = prompt(wctx.reader, "Admin token (enter to auto-generate)")
	if wctx.adminToken == "" {
		wctx.adminToken = GenerateSecret()
		fmt.Fprintln(os.Stderr, "  → Generated admin token")
	}
	wctx.workerType = promptChoice(wctx.reader, "Worker type", []string{"claude_code", "opencode_server"})
	return StepResult{Name: "required_config", Status: "pass", Detail: "worker=" + wctx.workerType}
}

func (wctx *wizardContext) runWorkerDep() StepResult {
	return stepWorkerDep(wctx.workerType)
}

func (wctx *wizardContext) runMessaging() StepResult {
	if wctx.opts.NonInteractive {
		wctx.slackCfg = buildPlatformNonInteractive(wctx.opts.EnableSlack, wctx.opts.SlackDMPolicy, wctx.opts.SlackGroupPolicy, wctx.opts.SlackAllowFrom)
		wctx.feishuCfg = buildPlatformNonInteractive(wctx.opts.EnableFeishu, wctx.opts.FeishuDMPolicy, wctx.opts.FeishuGroupPolicy, wctx.opts.FeishuAllowFrom)
		return StepResult{Name: "messaging", Status: "pass", Detail: messagingDetail(wctx.slackCfg.enabled, wctx.feishuCfg.enabled)}
	}
	slackCfg, feishuCfg, _ := stepMessaging(wctx.reader, wctx.opts, wctx.existing)
	wctx.slackCfg = slackCfg
	wctx.feishuCfg = feishuCfg
	return StepResult{Name: "messaging", Status: "pass", Detail: messagingDetail(slackCfg.enabled, feishuCfg.enabled)}
}

func (wctx *wizardContext) buildTemplateOpts() ConfigTemplateOptions {
	tplOpts := ConfigTemplateOptions{
		WorkerType:           wctx.workerType,
		SlackEnabled:         wctx.slackCfg.enabled,
		SlackDMPolicy:        wctx.slackCfg.dmPolicy,
		SlackGroupPolicy:     wctx.slackCfg.groupPolicy,
		SlackRequireMention:  toPtr(wctx.slackCfg.requireMention),
		SlackAllowFrom:       wctx.slackCfg.allowFrom,
		FeishuEnabled:        wctx.feishuCfg.enabled,
		FeishuDMPolicy:       wctx.feishuCfg.dmPolicy,
		FeishuGroupPolicy:    wctx.feishuCfg.groupPolicy,
		FeishuRequireMention: toPtr(wctx.feishuCfg.requireMention),
		FeishuAllowFrom:      wctx.feishuCfg.allowFrom,
	}
	if wctx.slackCfg.kept || wctx.feishuCfg.kept {
		tplOpts.KeptPlatforms = map[string]bool{
			"slack":  wctx.slackCfg.kept,
			"feishu": wctx.feishuCfg.kept,
		}
		tplOpts.ExistingConfigPath = wctx.opts.ConfigPath
	}
	return tplOpts
}

func (wctx *wizardContext) displayPreview() {
	fmt.Fprint(os.Stderr, output.SectionHeader("Configuration Preview"))
	fmt.Fprintf(os.Stderr, "    %-14s %s\n", "Worker:", wctx.workerType)

	slackStatus := "disabled"
	if wctx.slackCfg.enabled {
		slackStatus = fmt.Sprintf("enabled, dm=%s, group=%s, mention=%t",
			wctx.slackCfg.dmPolicy, wctx.slackCfg.groupPolicy, wctx.slackCfg.requireMention)
	}
	fmt.Fprintf(os.Stderr, "    %-14s %s\n", "Slack:", slackStatus)

	feishuStatus := "disabled"
	if wctx.feishuCfg.enabled {
		feishuStatus = fmt.Sprintf("enabled, dm=%s, group=%s, mention=%t",
			wctx.feishuCfg.dmPolicy, wctx.feishuCfg.groupPolicy, wctx.feishuCfg.requireMention)
	}
	fmt.Fprintf(os.Stderr, "    %-14s %s\n", "Feishu:", feishuStatus)

	fmt.Fprintf(os.Stderr, "    %-14s %s\n", "Config:", wctx.opts.ConfigPath)
	fmt.Fprintf(os.Stderr, "    %-14s %s\n", "Secrets:", wctx.envPath)
	fmt.Fprintln(os.Stderr)

	if !promptYesNo(wctx.reader, "Write configuration files?") {
		fmt.Fprintln(os.Stderr, "  → Skipping config write. Use --force to reconfigure later.")
	}
}

// ─── Existing config pre-fill helpers ────────────────────────────────────

func readExistingEnvValue(envPath, key string) string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		prefix := key + "="
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			return line[len(prefix):]
		}
	}
	return ""
}

func readExistingConfigValue(configPath, field string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(field, ".")
	if len(parts) != 2 {
		return ""
	}
	parentKey := parts[0] + ":"
	childKey := parts[1] + ":"
	inParent := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inParent {
			if trimmed == parentKey {
				inParent = true
			}
			continue
		}
		if strings.HasPrefix(line, "  "+childKey) {
			val := strings.TrimPrefix(trimmed, childKey)
			val = strings.TrimSpace(val)
			val = strings.Trim(val, "\"")
			return val
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") && trimmed != "" {
			break
		}
	}
	return ""
}

func prefillPlatformConfig(platform string, existing *ExistingConfig, envPath string, credKeys []string) messagingPlatformConfig {
	enabled := existing.PlatformConfigured(platform)
	cfg := messagingPlatformConfig{
		enabled:     enabled,
		kept:        enabled,
		credentials: readExistingEnvCredentials(envPath, credKeys),
	}
	if enabled {
		cfg.dmPolicy = "allowlist"
		cfg.groupPolicy = "allowlist"
		cfg.requireMention = true
	}
	return cfg
}

// ─── Step 1: Environment pre-check ──────────────────────────────────────────

func stepEnvPreCheck() StepResult {
	ver := runtime.Version()
	goOK := false
	if s, ok := strings.CutPrefix(ver, "go"); ok {
		parts := strings.Split(s, ".")
		if len(parts) >= 2 {
			if minor, err := strconv.Atoi(parts[1]); err == nil {
				goOK = minor >= 26
			}
		}
	}
	if !goOK {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("Go version %s does not meet requirement (>= go1.26)", ver)}
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("OS %s is not supported (need darwin, linux, or windows)", runtime.GOOS)}
	}

	freeMB, err := checkers.GetDiskFreeMB(".")
	if err != nil {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: "cannot check disk space: " + err.Error()}
	}

	if freeMB < 100 {
		return StepResult{Name: "env_precheck", Status: "fail", Detail: fmt.Sprintf("insufficient disk space: %d MB (need >= 100 MB)", freeMB)}
	}

	return StepResult{Name: "env_precheck", Status: "pass", Detail: fmt.Sprintf("Go %s, %s/%s, %d MB free", ver, runtime.GOOS, runtime.GOARCH, freeMB)}
}

// ─── Step 3: Worker dependency check ────────────────────────────────────────

func stepWorkerDep(workerType string) StepResult {
	binaries := map[string]string{
		"claude_code":     "claude",
		"opencode_server": "opencode",
	}
	bin, ok := binaries[workerType]
	if !ok {
		return StepResult{Name: "worker_dep", Status: "skip", Detail: "worker type " + workerType + " has no binary dependency"}
	}

	// Level 1: binary in PATH
	p, err := exec.LookPath(bin)
	if err != nil {
		return StepResult{Name: "worker_dep", Status: "warn", Detail: bin + " binary not found in PATH — install before running serve"}
	}
	detail := bin + " found: " + p

	// Level 2: --version functional check
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	out, verr := cmd.Output()
	if verr != nil {
		detail += " (version check timed out or failed)"
		return StepResult{Name: "worker_dep", Status: "pass", Detail: detail}
	}
	ver := strings.TrimSpace(string(out))
	if ver != "" {
		detail += " (" + ver + ")"
	}

	if workerType == "opencode_server" {
		detail += " — singleton mode: shared process across sessions"
	}
	return StepResult{Name: "worker_dep", Status: "pass", Detail: detail}
}

// ─── Step 4: Messaging platform ─────────────────────────────────────────────

func stepMessaging(reader *bufio.Reader, _ WizardOptions, existing *ExistingConfig) (slackCfg, feishuCfg messagingPlatformConfig, result StepResult) {
	fmt.Fprint(os.Stderr, output.SectionHeader("Messaging Platform (optional)"))

	slackCfg = collectPlatformOrKeep(reader, "Slack", existing, map[string]string{
		"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": "Slack Bot Token (xoxb-...)",
		"HOTPLEX_MESSAGING_SLACK_APP_TOKEN": "Slack App Token (xapp-...)",
	})

	feishuCfg = collectPlatformOrKeep(reader, "Feishu", existing, map[string]string{
		"HOTPLEX_MESSAGING_FEISHU_APP_ID":     "Feishu App ID",
		"HOTPLEX_MESSAGING_FEISHU_APP_SECRET": "Feishu App Secret",
	})

	return slackCfg, feishuCfg, StepResult{Name: "messaging", Status: "pass", Detail: messagingDetail(slackCfg.enabled, feishuCfg.enabled)}
}

func collectPlatformOrKeep(reader *bufio.Reader, platformName string, existing *ExistingConfig, credPrompts map[string]string) messagingPlatformConfig {
	if existing != nil && existing.PlatformConfigured(platformName) {
		if promptKeepPlatform(reader, platformName) {
			return messagingPlatformConfig{enabled: true, kept: true, credentials: map[string]string{}}
		}
	}
	return collectPlatformConfig(reader, platformName, credPrompts)
}

func promptKeepPlatform(reader *bufio.Reader, platform string) bool {
	fmt.Fprintf(os.Stderr, "? Keep existing %s configuration? %s: ", platform, output.Bold("[Y/n]"))
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line != "n" && line != "no"
}

func collectPlatformConfig(reader *bufio.Reader, platform string, credPrompts map[string]string) messagingPlatformConfig {
	if !promptYesNo(reader, fmt.Sprintf("Configure %s?", platform)) {
		return messagingPlatformConfig{credentials: map[string]string{}}
	}

	ShowPlatformGuide(strings.ToLower(platform))

	cfg := messagingPlatformConfig{
		enabled:     true,
		credentials: map[string]string{},
	}

	for envKey, promptText := range credPrompts {
		validate := credentialValidators[envKey]
		if validate != nil {
			if val := promptWithValidation(reader, "  "+promptText, validate); val != "" {
				cfg.credentials[envKey] = val
			}
		} else {
			if val := prompt(reader, "  "+promptText); val != "" {
				cfg.credentials[envKey] = val
			}
		}
	}

	fmt.Fprint(os.Stderr, output.SectionHeader("Access Policy [Enter = accept defaults]"))
	cfg.dmPolicy = promptWithDefault(reader, "  DM policy", "allowlist")
	cfg.groupPolicy = promptWithDefault(reader, "  Group policy", "allowlist")
	cfg.requireMention = promptYesNo(reader, "  Require @mention in groups?")
	cfg.allowFrom = promptCommaList(reader, fmt.Sprintf("  Allowed users for %s", platform))

	fmt.Fprintf(os.Stderr, "  → %s: dm=%s group=%s mention=%t\n", platform, cfg.dmPolicy, cfg.groupPolicy, cfg.requireMention)
	return cfg
}

// ─── Step 5: Config file generation ─────────────────────────────────────────

func stepConfigGen(opts WizardOptions, tplOpts ConfigTemplateOptions) (StepResult, bool) {
	if _, err := os.Stat(opts.ConfigPath); err == nil && !opts.Force {
		return StepResult{Name: "config_gen", Status: "skip", Detail: "preserved existing config (no changes needed)"}, false
	}

	dir := filepath.Dir(opts.ConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "create config dir: " + err.Error()}, false
	}

	if err := os.WriteFile(opts.ConfigPath, []byte(BuildConfigYAML(tplOpts)), 0o600); err != nil {
		return StepResult{Name: "config_gen", Status: "fail", Detail: "write config: " + err.Error()}, false
	}
	return StepResult{Name: "config_gen", Status: "pass", Detail: opts.ConfigPath}, true
}

// ─── Step 6: Write config ───────────────────────────────────────────────────

func stepWriteConfig(envPath, jwtSecret, adminToken string, slackCfg, feishuCfg messagingPlatformConfig, configCreated bool, opts WizardOptions) StepResult {
	if err := os.WriteFile(envPath, []byte(buildEnvContent(jwtSecret, adminToken, slackCfg, feishuCfg, envPath)), 0o600); err != nil {
		return StepResult{Name: "write_config", Status: "fail", Detail: "write .env: " + err.Error()}
	}

	if configCreated {
		if _, err := config.Load(opts.ConfigPath, config.LoadOptions{}); err != nil {
			return StepResult{Name: "write_config", Status: "fail", Detail: "config parse error: " + err.Error()}
		}
	}

	return StepResult{Name: "write_config", Status: "pass", Detail: envPath}
}

func buildEnvContent(jwtSecret, adminToken string, slackCfg, feishuCfg messagingPlatformConfig, existingEnvPath string) string {
	var b strings.Builder
	b.WriteString("# HotPlex Worker Gateway - Environment Configuration\n# Generated by onboard wizard\n\n")
	b.WriteString("# ── Security ──\n")
	b.WriteString("HOTPLEX_JWT_SECRET=" + jwtSecret + "\n")
	b.WriteString("HOTPLEX_ADMIN_TOKEN_1=" + adminToken + "\n")

	writePlatformEnv := func(name, enabledEnv string, cfg messagingPlatformConfig, knownCredKeys []string) {
		if !cfg.enabled {
			return
		}
		fmt.Fprintf(&b, "\n# ── %s ──\n%s=true\n", name, enabledEnv)
		if cfg.kept {
			for k, v := range readExistingEnvCredentials(existingEnvPath, knownCredKeys) {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
			return
		}
		for _, key := range sortedKeys(cfg.credentials) {
			fmt.Fprintf(&b, "%s=%s\n", key, cfg.credentials[key])
		}
	}
	writePlatformEnv("Slack", "HOTPLEX_MESSAGING_SLACK_ENABLED", slackCfg, []string{
		"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN",
		"HOTPLEX_MESSAGING_SLACK_APP_TOKEN",
	})
	writePlatformEnv("Feishu", "HOTPLEX_MESSAGING_FEISHU_ENABLED", feishuCfg, []string{
		"HOTPLEX_MESSAGING_FEISHU_APP_ID",
		"HOTPLEX_MESSAGING_FEISHU_APP_SECRET",
	})

	b.WriteByte('\n')
	return b.String()
}

func readExistingEnvCredentials(envPath string, keys []string) map[string]string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil
	}
	creds := make(map[string]string, len(keys))
	content := string(data)
	for _, key := range keys {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, key+"=") && len(line) > len(key)+1 {
				creds[key] = line[len(key)+1:]
				break
			}
		}
	}
	return creds
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ─── Step 6.5: STT Check ─────────────────────────────────────────────────────

func stepSTTCheck(configPath string, reader *bufio.Reader) StepResult {
	checkers.SetConfigPath(configPath)
	sttCheckers := cli.DefaultRegistry.ByCategory("stt")

	var details []string
	for _, c := range sttCheckers {
		d := c.Check(context.Background())
		if d.Status == cli.StatusFail {
			details = append(details, d.Message)
			if d.FixHint != "" {
				details = append(details, "  → "+d.FixHint)
			}
		}
	}

	if len(details) == 0 {
		return StepResult{Name: "stt_check", Status: "pass", Detail: "STT environment ready"}
	}

	// Interactive install prompt
	if reader != nil {
		fmt.Fprint(os.Stderr, output.SectionHeader("STT Dependencies"))
		for _, d := range details {
			fmt.Fprintf(os.Stderr, "  %s %s\n", output.StatusSymbol("warn"), d)
		}
		if promptYesNo(reader, "Install STT dependencies?") {
			if err := installSTTDeps(); err != nil {
				details = append(details, "install failed: "+err.Error())
			} else {
				return StepResult{Name: "stt_check", Status: "pass", Detail: "STT dependencies installed"}
			}
		}
	}

	return StepResult{
		Name:   "stt_check",
		Status: "warn",
		Detail: "STT deps incomplete:\n  " + strings.Join(details, "\n  "),
	}
}

func installSTTDeps() error {
	cmd := exec.Command("python3", "-m", "pip", "install", "--quiet", "funasr-onnx", "modelscope")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ─── Step 7: Verify ─────────────────────────────────────────────────────────

func stepVerify(configPath string) StepResult {
	// Load .env so env-secret-based checkers can find values
	loadEnvFile(filepath.Dir(configPath))
	checkers.SetConfigPath(configPath)

	var allCheckers []cli.Checker
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("environment")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("config")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("dependencies")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("security")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("runtime")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("messaging")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("stt")...)
	allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("agent_config")...)

	var passCount, warnCount, failCount int
	var failLines []string
	for _, c := range allCheckers {
		d := c.Check(context.Background())
		switch d.Status {
		case cli.StatusPass:
			passCount++
		case cli.StatusWarn:
			warnCount++
		case cli.StatusFail:
			failCount++
			line := d.Name + ": " + d.Message
			if d.Detail != "" {
				line += "\n  missing: " + d.Detail
			}
			if d.FixHint != "" {
				line += "\n  → " + d.FixHint
			}
			failLines = append(failLines, line)
		}
	}

	if failCount > 0 {
		return StepResult{Name: "verify", Status: "fail", Detail: strings.Join(failLines, "\n")}
	}
	detail := fmt.Sprintf("%d passed", passCount)
	if warnCount > 0 {
		detail += fmt.Sprintf(", %d warnings", warnCount)
	}
	return StepResult{Name: "verify", Status: "pass", Detail: detail}
}

func loadEnvFile(dir string) {
	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}

	var loaded int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" && !security.IsProtected(key) {
			_ = os.Setenv(key, val)
			loaded++
		}
	}
	if loaded > 0 {
		fmt.Fprintf(os.Stderr, "  env loaded %d vars from %s\n", loaded, envPath)
	}
}

// ─── Prompt helpers ─────────────────────────────────────────────────────────

func promptWithValidation(reader *bufio.Reader, question string, validate func(string) error) string {
	for {
		val := prompt(reader, question)
		if val == "" {
			return "" // empty = auto-generate or skip
		}
		if err := validate(val); err != nil {
			fmt.Fprintf(os.Stderr, "  %s %s\n", output.StatusSymbol("fail"), err.Error())
			continue
		}
		return val
	}
}

// credentialValidators maps env keys to format validation functions.
var credentialValidators = map[string]func(string) error{
	"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": func(val string) error {
		if !strings.HasPrefix(val, "xoxb-") {
			return fmt.Errorf("slack Bot Token must start with xoxb-")
		}
		if len(val) < 20 {
			return fmt.Errorf("token appears too short")
		}
		return nil
	},
	"HOTPLEX_MESSAGING_SLACK_APP_TOKEN": func(val string) error {
		if !strings.HasPrefix(val, "xapp-") {
			return fmt.Errorf("slack App Token must start with xapp-")
		}
		if len(val) < 20 {
			return fmt.Errorf("token appears too short")
		}
		return nil
	},
	"HOTPLEX_MESSAGING_FEISHU_APP_ID": func(val string) error {
		if !strings.HasPrefix(val, "cli_") {
			return fmt.Errorf("feishu App ID must start with cli_")
		}
		return nil
	},
	"HOTPLEX_MESSAGING_FEISHU_APP_SECRET": func(val string) error {
		if len(val) < 16 {
			return fmt.Errorf("app Secret appears too short")
		}
		return nil
	},
}

func prompt(reader *bufio.Reader, question string) string {
	fmt.Fprintf(os.Stderr, "? %s: ", question)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptChoice(reader *bufio.Reader, question string, choices []string) string {
	fmt.Fprintf(os.Stderr, "? %s:\n", question)
	for i, c := range choices {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, c)
	}
	fmt.Fprintf(os.Stderr, "  Select [1]: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return choices[0]
	}
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(choices) {
		return choices[0]
	}
	return choices[idx-1]
}

func promptYesNo(reader *bufio.Reader, question string) bool {
	fmt.Fprintf(os.Stderr, "? %s [y/N]: ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func promptWithDefault(reader *bufio.Reader, question, def string) string { //nolint:unparam // def default varies by caller context
	fmt.Fprintf(os.Stderr, "? %s [%s]: ", question, def)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptCommaList(reader *bufio.Reader, question string) []string {
	fmt.Fprintf(os.Stderr, "? %s (comma-separated, Enter to skip): ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ─── Step 6b: Agent config ──────────────────────────────────────────────────

func stepAgentConfig() (StepResult, []string) {
	dir := filepath.Join(config.HotplexHome(), "agent-configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StepResult{Name: "agent_config", Status: "warn", Detail: "create dir: " + err.Error()}, nil
	}

	var created []string
	for _, t := range DefaultTemplates() {
		path := filepath.Join(dir, t.Name)
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return StepResult{Name: "agent_config", Status: "warn", Detail: "write " + t.Name + ": " + err.Error()}, created
		}
		if _, err := fh.WriteString(t.Content); err != nil {
			_ = fh.Close()
			_ = os.Remove(path)
			return StepResult{Name: "agent_config", Status: "warn", Detail: "write " + t.Name + ": " + err.Error()}, created
		}
		if err := fh.Close(); err != nil {
			_ = os.Remove(path)
			return StepResult{Name: "agent_config", Status: "warn", Detail: "close " + t.Name + ": " + err.Error()}, created
		}
		created = append(created, t.Name)
	}

	if len(created) == 0 {
		return StepResult{Name: "agent_config", Status: "pass", Detail: dir}, created
	}
	return StepResult{
		Name:   "agent_config",
		Status: "pass",
		Detail: fmt.Sprintf("%s (%s) — per-bot: %s/<platform>/<botID>/SOUL.md", dir, strings.Join(created, ", "), dir),
	}, created
}

func stepServiceInstall(reader *bufio.Reader, opts WizardOptions) StepResult {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return StepResult{Name: "service_install", Status: "skip", Detail: "unsupported OS: " + runtime.GOOS}
	}

	mgr := service.NewManager()
	level, _ := service.ParseLevel(opts.ServiceLevel)
	existing, _ := mgr.Status("hotplex", level)
	if existing != nil && existing.Installed {
		return StepResult{Name: "service_install", Status: "pass", Detail: "already installed at " + existing.UnitPath}
	}

	if opts.NonInteractive {
		if !opts.InstallService {
			return StepResult{Name: "service_install", Status: "skip", Detail: "not requested (use --install-service)"}
		}
	} else {
		fmt.Fprint(os.Stderr, output.SectionHeader("System Service"))
		if !promptYesNo(reader, "Install HotPlex as system service?") {
			return StepResult{Name: "service_install", Status: "skip", Detail: "skipped by user"}
		}
		levelStr := promptWithDefault(reader, "  Service level (user/system)", "user")
		var err error
		level, err = service.ParseLevel(levelStr)
		if err != nil {
			return StepResult{Name: "service_install", Status: "fail", Detail: err.Error()}
		}
	}

	if level == service.LevelSystem && !service.IsPrivileged() {
		return StepResult{Name: "service_install", Status: "fail", Detail: "system-level requires elevated privileges (use sudo or run as administrator)"}
	}

	binaryPath, err := service.ResolveBinaryPath()
	if err != nil {
		return StepResult{Name: "service_install", Status: "fail", Detail: err.Error()}
	}

	if err := mgr.Install(service.InstallOptions{
		BinaryPath: binaryPath,
		ConfigPath: opts.ConfigPath,
		Level:      level,
		Name:       "hotplex",
	}); err != nil {
		return StepResult{Name: "service_install", Status: "fail", Detail: err.Error()}
	}

	return StepResult{Name: "service_install", Status: "pass", Detail: string(level) + " level"}
}
