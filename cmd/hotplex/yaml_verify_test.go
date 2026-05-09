package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli/onboard"
)

func TestEmbeddedConfigYAMLIntegrity(t *testing.T) {
	t.Parallel()

	yaml := onboard.DefaultConfigYAML()

	checks := []string{
		"gateway:", "admin:", "db:", "security:", "session:",
		"pool:", "worker:", "auto_retry:", "log:", "messaging:",
		"slack:", "feishu:", "ping_interval: 54s", "pong_timeout: 60s",
		"write_timeout: 10s", "idle_timeout: 5m", "max_frame_size: 32768",
		"broadcast_queue_size: 256", "rate_limit_enabled: true",
		"wal_mode: true", "busy_timeout: 5s",
		"api_key_header:", "tls_enabled: false",
		"jwt_audience:", "retention_period: 168h", "gc_scan_interval: 1m",
		"max_concurrent: 1000", "min_size: 0", "max_size: 100", "max_idle_per_user: 5",
		"max_memory_per_user: 3221225472",
		"max_lifetime: 24h", "execution_timeout: 30m",
		"max_retries: 9", "base_delay: 5s", "max_delay: 120s",
		"notify_user: true", "retry_input:",
		"require_mention:",
		"stt_provider:", "stt_local_cmd:",
		"stt_local_idle_ttl:", "socket_mode: true",
		"type:", "enabled:",
	}
	for _, c := range checks {
		require.True(t, strings.Contains(yaml, c), "embedded config.yaml missing: %s", c)
	}
}

func TestBuildConfigYAML_SlackEnabled(t *testing.T) {
	t.Parallel()

	yaml, err := onboard.BuildConfigYAML(onboard.ConfigTemplateOptions{SlackEnabled: true})
	require.NoError(t, err)
	require.Contains(t, yaml, "    enabled: true")
	require.Contains(t, yaml, "slack:")
}

func TestBuildConfigYAML_FeishuEnabled(t *testing.T) {
	t.Parallel()

	trueVal := true
	yaml, err := onboard.BuildConfigYAML(onboard.ConfigTemplateOptions{
		FeishuEnabled:        true,
		FeishuRequireMention: &trueVal,
		FeishuDMPolicy:       "open",
	})
	require.NoError(t, err)
	require.Contains(t, yaml, "feishu:\n        enabled: true")
	require.Contains(t, yaml, `dm_policy: open`)
}
