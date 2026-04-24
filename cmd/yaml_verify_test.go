package main

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/cli/onboard"
)

func main() {
	yaml := onboard.DefaultConfigYAML()
	lines := strings.Split(strings.TrimSpace(yaml), "\n")
	fmt.Printf("Total lines: %d\n", len(lines))

	checks := []string{
		"gateway:", "admin:", "db:", "security:", "session:",
		"pool:", "worker:", "auto_retry:", "log:", "messaging:",
		"slack:", "feishu:", "ping_interval: 54s", "pong_timeout: 60s",
		"write_timeout: 10s", "idle_timeout: 5m", "max_frame_size: 32768",
		"broadcast_queue_size: 256", "rate_limit_enabled: true",
		"wal_mode: true", "busy_timeout: 500ms",
		"api_key_header:", "tls_enabled: false",
		"jwt_audience:", "retention_period: 168h", "gc_scan_interval: 1m",
		"max_concurrent: 1000", "event_store_enabled: true",
		"min_size: 0", "max_size: 100", "max_idle_per_user: 5",
		"max_memory_per_user: 3221225472",
		"max_lifetime: 24h", "execution_timeout: 30m",
		"max_retries: 9", "base_delay: 5s", "max_delay: 120s",
		"notify_user: true", "retry_input:",
		"dm_policy:", "group_policy:", "require_mention:",
		"stt_provider:", "stt_local_cmd:", "stt_local_mode:",
		"stt_local_idle_ttl:", "socket_mode: true",
		"type:", "enabled:",
	}
	missing := false
	for _, c := range checks {
		if !strings.Contains(yaml, c) {
			fmt.Printf("MISSING: %s\n", c)
			missing = true
		}
	}

	// Test dynamic behavior
	yaml2 := onboard.BuildConfigYAML(onboard.ConfigTemplateOptions{SlackEnabled: true})
	if !strings.Contains(yaml2, "    enabled: true") || !strings.Contains(yaml2, "slack:") {
		fmt.Println("FAIL: SlackEnabled=true not reflected")
	}

	// Test feishu enabled
	trueVal := true
	yaml3 := onboard.BuildConfigYAML(onboard.ConfigTemplateOptions{FeishuEnabled: true, FeishuRequireMention: &trueVal, FeishuDMPolicy: "open"})
	if !strings.Contains(yaml3, "feishu:\n    enabled: true") {
		fmt.Println("FAIL: FeishuEnabled=true not reflected")
	}
	if !strings.Contains(yaml3, "dm_policy: \"open\"") {
		fmt.Println("FAIL: FeishuDMPolicy open not reflected")
	}

	if !missing {
		fmt.Println("All section checks passed")
	}
}
