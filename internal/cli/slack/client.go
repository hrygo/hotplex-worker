package slackcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/internal/config"
)

func NewClient(botToken string) (*slack.Client, error) {
	if botToken == "" {
		return nil, fmt.Errorf("slack bot token not configured (HOTPLEX_MESSAGING_SLACK_BOT_TOKEN)")
	}
	return slack.New(botToken), nil
}

func ResolveChannel(flagCh string) (string, error) {
	if flagCh != "" {
		return flagCh, nil
	}
	if env := os.Getenv("HOTPLEX_SLACK_CHANNEL_ID"); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("--channel is required (or set HOTPLEX_SLACK_CHANNEL_ID env var)")
}

func ResolveThreadTS(flagTS string) string {
	if flagTS != "" {
		return flagTS
	}
	return os.Getenv("HOTPLEX_SLACK_THREAD_TS")
}

func LoadConfigAndClient(configPath string) (*config.Config, *slack.Client, error) {
	configPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve config path: %w", err)
	}

	loadEnvFile(filepath.Dir(configPath))

	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	if !cfg.Messaging.Slack.Enabled {
		return nil, nil, fmt.Errorf("slack is not enabled in configuration")
	}

	client, err := NewClient(cfg.Messaging.Slack.BotToken)
	if err != nil {
		return nil, nil, err
	}

	return cfg, client, nil
}

func loadEnvFile(dir string) {
	envPath := filepath.Join(dir, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
