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
	"github.com/hrygo/hotplex/internal/messaging/slack"
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
	cfg := deps.Config
	hub := deps.Hub
	sm := deps.SessionMgr
	handler := deps.Handler
	gwBridge := deps.Bridge
	for _, pt := range messaging.RegisteredTypes() {
		var workerType, workDir string
		switch pt {
		case messaging.PlatformSlack:
			if !cfg.Messaging.Slack.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "Slack", Started: false})
				continue
			}
			workerType = cfg.Messaging.Slack.WorkerType
			workDir = cfg.Messaging.Slack.WorkDir
		case messaging.PlatformFeishu:
			if !cfg.Messaging.Feishu.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "Feishu", Started: false})
				continue
			}
			workerType = cfg.Messaging.Feishu.WorkerType
			workDir = cfg.Messaging.Feishu.WorkDir
		}

		adapter, err := messaging.New(pt, log)
		if err != nil {
			log.Warn("messaging: skip adapter", "platform", pt, "err", err)
			continue
		}

		msgBridge := messaging.NewBridge(log, pt, hub, sm, handler, gwBridge, workerType, workDir)

		switch pt {
		case messaging.PlatformSlack:
			if sa, ok := adapter.(*slack.Adapter); ok {
				sa.Configure(cfg.Messaging.Slack.BotToken, cfg.Messaging.Slack.AppToken, msgBridge)
				gate := slack.NewGate(
					cfg.Messaging.Slack.DMPolicy,
					cfg.Messaging.Slack.GroupPolicy,
					cfg.Messaging.Slack.RequireMention,
					cfg.Messaging.Slack.AllowFrom,
					cfg.Messaging.Slack.AllowDMFrom,
					cfg.Messaging.Slack.AllowGroupFrom,
				)
				sa.SetGate(gate)
				sa.SetAssistantEnabled(cfg.Messaging.Slack.AssistantAPIEnabled)
				sa.SetReconnectDelays(cfg.Messaging.Slack.ReconnectBaseDelay, cfg.Messaging.Slack.ReconnectMaxDelay)
				if t := buildSlackTranscriber(cfg.Messaging.Slack, log); t != nil {
					sa.SetTranscriber(t)
				}
			}
		case messaging.PlatformFeishu:
			if fa, ok := adapter.(*feishu.Adapter); ok {
				fa.Configure(cfg.Messaging.Feishu.AppID, cfg.Messaging.Feishu.AppSecret, msgBridge)
				gate := feishu.NewGate(
					cfg.Messaging.Feishu.DMPolicy,
					cfg.Messaging.Feishu.GroupPolicy,
					cfg.Messaging.Feishu.RequireMention,
					cfg.Messaging.Feishu.AllowFrom,
					cfg.Messaging.Feishu.AllowDMFrom,
					cfg.Messaging.Feishu.AllowGroupFrom,
				)
				fa.SetGate(gate)

				if t := buildFeishuTranscriber(cfg.Messaging.Feishu, log); t != nil {
					fa.SetTranscriber(t)
				}
			}
		}

		if a, ok := adapter.(interface{ SetHub(messaging.HubInterface) }); ok {
			a.SetHub(hub)
		}
		if a, ok := adapter.(interface {
			SetSessionManager(messaging.SessionManager)
		}); ok {
			a.SetSessionManager(sm)
		}
		if a, ok := adapter.(interface {
			SetHandler(messaging.HandlerInterface)
		}); ok {
			a.SetHandler(handler)
		}
		if a, ok := adapter.(interface{ SetBridge(*messaging.Bridge) }); ok {
			a.SetBridge(msgBridge)
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
