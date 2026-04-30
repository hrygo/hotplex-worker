package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/feishu"
	_ "github.com/hrygo/hotplex/internal/messaging/slack"
	"github.com/hrygo/hotplex/internal/messaging/stt"
)

var (
	sttCache   = make(map[string]*stt.SharedTranscriber)
	sttCacheMu sync.Mutex
)

func closeSTTCache(ctx context.Context, log *slog.Logger) {
	sttCacheMu.Lock()
	defer sttCacheMu.Unlock()
	for key, s := range sttCache {
		if err := s.Close(ctx); err != nil {
			log.Warn("stt: cache close", "key", key, "err", err)
		}
		delete(sttCache, key)
	}
}

func startMessagingAdapters(ctx context.Context, deps *GatewayDeps) ([]messaging.PlatformAdapterInterface, []AdapterStatus) {
	var adapters []messaging.PlatformAdapterInterface
	var statuses []AdapterStatus
	log := deps.Log
	appCfg := deps.Config
	hub := deps.Hub
	sm := deps.SessionMgr
	handler := deps.Handler
	gwBridge := deps.Bridge
	for _, pt := range messaging.RegisteredTypes() {
		var workerType, workDir string
		switch pt {
		case messaging.PlatformSlack:
			if !appCfg.Messaging.Slack.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "slack", Started: false})
				continue
			}
			workerType = appCfg.Messaging.Slack.WorkerType
			workDir = appCfg.Messaging.Slack.WorkDir
		case messaging.PlatformFeishu:
			if !appCfg.Messaging.Feishu.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "feishu", Started: false})
				continue
			}
			workerType = appCfg.Messaging.Feishu.WorkerType
			workDir = appCfg.Messaging.Feishu.WorkDir
		}

		adapter, err := messaging.New(pt, log)
		if err != nil {
			log.Warn("messaging: skip adapter", "platform", pt, "err", err)
			continue
		}

		msgBridge := messaging.NewBridge(log, pt, hub, sm, handler, gwBridge, workerType, workDir)

		acfg := messaging.AdapterConfig{
			Hub:     hub,
			SM:      sm,
			Handler: handler,
			Bridge:  msgBridge,
			Extras:  make(map[string]any),
		}

		switch pt {
		case messaging.PlatformSlack:
			gateway := messaging.NewGate(
				appCfg.Messaging.Slack.DMPolicy,
				appCfg.Messaging.Slack.GroupPolicy,
				appCfg.Messaging.Slack.RequireMention,
				appCfg.Messaging.Slack.AllowFrom,
				appCfg.Messaging.Slack.AllowDMFrom,
				appCfg.Messaging.Slack.AllowGroupFrom,
			)
			acfg.Gate = gateway
			acfg.Extras["bot_token"] = appCfg.Messaging.Slack.BotToken
			acfg.Extras["app_token"] = appCfg.Messaging.Slack.AppToken
			acfg.Extras["assistant_enabled"] = appCfg.Messaging.Slack.AssistantAPIEnabled
			acfg.Extras["reconnect_base_delay"] = appCfg.Messaging.Slack.ReconnectBaseDelay
			acfg.Extras["reconnect_max_delay"] = appCfg.Messaging.Slack.ReconnectMaxDelay
			if t := buildSlackTranscriber(appCfg.Messaging.Slack, log); t != nil {
				acfg.Extras["transcriber"] = t
			}
		case messaging.PlatformFeishu:
			gateway := messaging.NewGate(
				appCfg.Messaging.Feishu.DMPolicy,
				appCfg.Messaging.Feishu.GroupPolicy,
				appCfg.Messaging.Feishu.RequireMention,
				appCfg.Messaging.Feishu.AllowFrom,
				appCfg.Messaging.Feishu.AllowDMFrom,
				appCfg.Messaging.Feishu.AllowGroupFrom,
			)
			acfg.Gate = gateway
			acfg.Extras["app_id"] = appCfg.Messaging.Feishu.AppID
			acfg.Extras["app_secret"] = appCfg.Messaging.Feishu.AppSecret
			if t := buildFeishuTranscriber(appCfg.Messaging.Feishu, log); t != nil {
				acfg.Extras["transcriber"] = t
			}
		}

		if err := adapter.ConfigureWith(acfg); err != nil {
			log.Warn("messaging: configure failed", "platform", pt, "err", err)
			continue
		}

		if err := adapter.Start(ctx); err != nil {
			log.Warn("messaging: start failed", "platform", pt, "err", err)
			statuses = append(statuses, AdapterStatus{Name: string(pt), Started: false})
			continue
		}
		adapters = append(adapters, adapter)
		statuses = append(statuses, AdapterStatus{Name: string(pt), Started: true})
		log.Info("messaging: adapter started", "platform", pt)
	}
	return adapters, statuses
}

func buildFeishuTranscriber(cfg config.FeishuConfig, log *slog.Logger) stt.Transcriber {
	switch cfg.Provider {
	case config.STTProviderFeishu:
		client := lark.NewClient(cfg.AppID, cfg.AppSecret)
		return feishu.NewFeishuSTT(client, log)
	case config.STTProviderLocal:
		return buildLocalSTT("feishu", cfg.STTConfig, log)
	case config.STTProviderFeishuLocal:
		if cfg.LocalCmd == "" {
			log.Warn("feishu: stt_provider=feishu+local but stt_local_cmd is empty, using feishu only")
			client := lark.NewClient(cfg.AppID, cfg.AppSecret)
			return feishu.NewFeishuSTT(client, log)
		}
		client := lark.NewClient(cfg.AppID, cfg.AppSecret)
		return stt.NewFallbackSTT(
			feishu.NewFeishuSTT(client, log),
			buildLocalSTT("feishu", cfg.STTConfig, log),
			log,
		)
	default:
		return nil
	}
}

func buildSlackTranscriber(cfg config.SlackConfig, log *slog.Logger) stt.Transcriber {
	if cfg.Provider != config.STTProviderLocal {
		return nil
	}
	return buildLocalSTT("slack", cfg.STTConfig, log)
}

func buildLocalSTT(platform string, cfg config.STTConfig, log *slog.Logger) stt.Transcriber {
	if cfg.LocalCmd == "" {
		log.Warn(platform + ": stt_provider=local but stt_local_cmd is empty, STT disabled")
		return nil
	}

	sttCacheMu.Lock()
	defer sttCacheMu.Unlock()

	var transcriber stt.Transcriber
	expandedCmd := expandCommand(cfg.LocalCmd)

	cacheKey := cfg.LocalMode + ":" + expandedCmd

	if existing, ok := sttCache[cacheKey]; ok {
		if existing.Refs() <= 0 {
			delete(sttCache, cacheKey)
		} else {
			log.Debug(platform+": reusing shared stt transcriber", "mode", cfg.LocalMode, "cmd", expandedCmd)
			return existing.Acquire()
		}
	}

	if cfg.LocalMode == config.STTModePersistent {
		hash := sha256.Sum256([]byte(expandedCmd))
		pidKey := "stt-server-" + hex.EncodeToString(hash[:])[:12]
		transcriber = stt.NewPersistentSTT(expandedCmd, pidKey, cfg.LocalIdleTTL, log)
	} else {
		transcriber = stt.NewLocalSTT(expandedCmd, log)
	}

	shared := stt.NewSharedTranscriber(transcriber)
	sttCache[cacheKey] = shared
	return shared
}

func expandCommand(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return cmd
	}

	scriptsDir := filepath.Join(config.HotplexHome(), "scripts")

	for i, p := range parts {
		// 1. Expand ~/ paths
		if strings.HasPrefix(p, "~/") {
			parts[i], _ = config.ExpandAndAbs(p)
			continue
		}

		// 2. Smart Perception: If it's a known built-in script name,
		// and not an absolute path, try to find it in ~/.hotplex/scripts/
		if strings.HasSuffix(p, ".py") && !filepath.IsAbs(p) {
			localPath := filepath.Join(scriptsDir, p)
			if _, err := os.Stat(localPath); err == nil {
				parts[i] = localPath
			}
		}
	}
	return strings.Join(parts, " ")
}
