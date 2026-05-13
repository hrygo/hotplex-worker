package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	slackapi "github.com/slack-go/slack"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/feishu"
	"github.com/hrygo/hotplex/internal/messaging/slack"
	"github.com/hrygo/hotplex/internal/messaging/stt"
	"github.com/hrygo/hotplex/internal/messaging/tts"
)

var (
	sttCache   = make(map[string]*stt.SharedTranscriber)
	sttCacheMu sync.Mutex

	ttsCache   = make(map[string]*tts.SharedSynthesizer)
	ttsCacheMu sync.Mutex
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

func closeTTSCache(ctx context.Context, log *slog.Logger) {
	ttsCacheMu.Lock()
	defer ttsCacheMu.Unlock()
	for key, s := range ttsCache {
		if err := s.Close(ctx); err != nil {
			log.Warn("tts: cache close", "key", key, "err", err)
		}
		delete(ttsCache, key)
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
	registry := messaging.DefaultBotRegistry()

	for _, pt := range messaging.RegisteredTypes() {
		var workerType, workDir string
		var botEntries []*messaging.BotEntry

		switch pt {
		case messaging.PlatformSlack:
			if !appCfg.Messaging.Slack.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "slack", Started: false})
				continue
			}
			workerType = appCfg.Messaging.Slack.WorkerType
			for i := range appCfg.Messaging.Slack.Bots {
				bc := &appCfg.Messaging.Slack.Bots[i]
				botEntries = append(botEntries, &messaging.BotEntry{
					Name:       bc.Name,
					Platform:   pt,
					Soul:       bc.Soul,
					WorkerType: bc.WorkerType,
					Status:     messaging.BotStatusStarting,
				})
			}
		case messaging.PlatformFeishu:
			if !appCfg.Messaging.Feishu.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "feishu", Started: false})
				continue
			}
			workerType = appCfg.Messaging.Feishu.WorkerType
			for i := range appCfg.Messaging.Feishu.Bots {
				bc := &appCfg.Messaging.Feishu.Bots[i]
				botEntries = append(botEntries, &messaging.BotEntry{
					Name:       bc.Name,
					Platform:   pt,
					Soul:       bc.Soul,
					WorkerType: bc.WorkerType,
					Status:     messaging.BotStatusStarting,
				})
			}
		}

		if len(botEntries) == 0 {
			continue
		}

		// Startup validation: duplicate bot name within same platform.
		seenNames := make(map[string]bool, len(botEntries))
		var validated []*messaging.BotEntry
		for _, e := range botEntries {
			if seenNames[e.Name] {
				log.Error("messaging: duplicate bot name, skipping", "platform", pt, "bot", e.Name)
				continue
			}
			seenNames[e.Name] = true
			validated = append(validated, e)
		}
		botEntries = validated

		// Startup validation: bot count limit.
		const maxBotsPerPlatform = 10
		if len(botEntries) > maxBotsPerPlatform {
			log.Warn("messaging: bot count exceeds limit, excess bots ignored",
				"platform", pt, "count", len(botEntries), "limit", maxBotsPerPlatform)
			botEntries = botEntries[:maxBotsPerPlatform]
		}

		workDir = appCfg.ResolvePlatformWorkDir(string(pt))

		for _, entry := range botEntries {
			// Per-bot workerType override.
			botWorkerType := workerType
			if entry.WorkerType != "" {
				botWorkerType = entry.WorkerType
			}

			adapter, err := messaging.New(pt, log)
			if err != nil {
				log.Warn("messaging: skip adapter", "platform", pt, "bot", entry.Name, "err", err)
				continue
			}

			msgBridge := messaging.NewBridge(log, pt, hub, sm, handler, gwBridge, botWorkerType, workDir)

			acfg := messaging.AdapterConfig{
				Hub:     hub,
				SM:      sm,
				Handler: handler,
				Bridge:  msgBridge,
				BotName: entry.Name,
				Extras:  make(map[string]any),
			}
			acfg.Extras["turn_summary_enabled"] = appCfg.Messaging.TurnSummaryEnabled

			switch pt {
			case messaging.PlatformSlack:
				botCfg := resolveSlackBot(appCfg, entry.Name)
				fillSlackExtras(&acfg, appCfg, botCfg, log)
			case messaging.PlatformFeishu:
				botCfg := resolveFeishuBot(appCfg, entry.Name)
				fillFeishuExtras(&acfg, appCfg, botCfg, log)
			}

			if err := adapter.ConfigureWith(acfg); err != nil {
				log.Warn("messaging: configure failed", "platform", pt, "bot", entry.Name, "err", err)
				continue
			}

			if err := adapter.Start(ctx); err != nil {
				log.Warn("messaging: start failed", "platform", pt, "bot", entry.Name, "err", err)
				statuses = append(statuses, AdapterStatus{Name: string(pt), BotName: entry.Name, Started: false})
				continue
			}

			// Update entry with runtime info and register.
			entry.BotID = adapter.GetBotID()
			entry.Status = messaging.BotStatusRunning
			entry.Adapter = adapter
			entry.Bridge = msgBridge
			entry.ConnectedAt = time.Now().UTC()
			registry.Register(entry)

			// Hint: global agent-config files without bot-level directory.
			if appCfg.AgentConfig.Enabled && appCfg.AgentConfig.ConfigDir != "" {
				if botID := adapter.GetBotID(); botID != "" {
					botDir := filepath.Join(appCfg.AgentConfig.ConfigDir, string(pt), botID)
					if _, err := os.Stat(botDir); os.IsNotExist(err) && agentconfig.HasGlobalFiles(appCfg.AgentConfig.ConfigDir) {
						log.Warn("agent-config: global files found but no bot-level directory",
							"platform", pt,
							"bot", entry.Name,
							"bot_id", botID,
							"bot_dir", botDir)
					}
				}
			}

			if err := msgBridge.SetAdapter(adapter); err != nil {
				log.Error("messaging: adapter platform mismatch", "platform", pt, "bot", entry.Name, "err", err)
			}
			adapters = append(adapters, adapter)
			statuses = append(statuses, AdapterStatus{Name: string(pt), BotName: entry.Name, Started: true})
			log.Info("messaging: adapter started", "platform", pt, "bot", entry.Name, "bot_id", entry.BotID)
		}
	}
	return adapters, statuses
}

// resolveSlackBot finds the SlackBotConfig for the given bot name.
func resolveSlackBot(cfg *config.Config, name string) *config.SlackBotConfig {
	for i := range cfg.Messaging.Slack.Bots {
		if cfg.Messaging.Slack.Bots[i].Name == name {
			return &cfg.Messaging.Slack.Bots[i]
		}
	}
	return nil
}

// resolveFeishuBot finds the FeishuBotConfig for the given bot name.
func resolveFeishuBot(cfg *config.Config, name string) *config.FeishuBotConfig {
	for i := range cfg.Messaging.Feishu.Bots {
		if cfg.Messaging.Feishu.Bots[i].Name == name {
			return &cfg.Messaging.Feishu.Bots[i]
		}
	}
	return nil
}

// fillSlackExtras populates AdapterConfig.Extras for a Slack bot.
func fillSlackExtras(acfg *messaging.AdapterConfig, appCfg *config.Config, botCfg *config.SlackBotConfig, log *slog.Logger) {
	platformCfg := appCfg.Messaging.Slack
	gate := messaging.NewGate(
		platformCfg.DMPolicy,
		platformCfg.GroupPolicy,
		platformCfg.RequireMention,
		platformCfg.AllowFrom,
		platformCfg.AllowDMFrom,
		platformCfg.AllowGroupFrom,
	)
	acfg.Gate = gate

	// Use bot-specific credentials, falling back to platform-level.
	botToken := platformCfg.BotToken
	appToken := platformCfg.AppToken
	if botCfg != nil {
		if botCfg.BotToken != "" {
			botToken = botCfg.BotToken
		}
		if botCfg.AppToken != "" {
			appToken = botCfg.AppToken
		}
	}
	acfg.Extras["bot_token"] = botToken
	acfg.Extras["app_token"] = appToken
	acfg.Extras["assistant_enabled"] = platformCfg.AssistantAPIEnabled
	acfg.Extras["reconnect_base_delay"] = platformCfg.ReconnectBaseDelay
	acfg.Extras["reconnect_max_delay"] = platformCfg.ReconnectMaxDelay

	sttCfg := platformCfg.STTConfig
	ttsCfg := platformCfg.TTSConfig
	if botCfg != nil {
		sttCfg = botCfg.STTConfig
		ttsCfg = botCfg.TTSConfig
	}
	if t := buildSlackTranscriber(sttCfg, log); t != nil {
		acfg.Extras["transcriber"] = t
	}
	if p := buildSlackTTSPipeline(ttsCfg, botToken, appToken, log); p != nil {
		acfg.Extras["tts_pipeline"] = p
	}
}

// fillFeishuExtras populates AdapterConfig.Extras for a Feishu bot.
func fillFeishuExtras(acfg *messaging.AdapterConfig, appCfg *config.Config, botCfg *config.FeishuBotConfig, log *slog.Logger) {
	platformCfg := appCfg.Messaging.Feishu
	gate := messaging.NewGate(
		platformCfg.DMPolicy,
		platformCfg.GroupPolicy,
		platformCfg.RequireMention,
		platformCfg.AllowFrom,
		platformCfg.AllowDMFrom,
		platformCfg.AllowGroupFrom,
	)
	acfg.Gate = gate

	// Use bot-specific credentials, falling back to platform-level.
	appID := platformCfg.AppID
	appSecret := platformCfg.AppSecret
	if botCfg != nil {
		if botCfg.AppID != "" {
			appID = botCfg.AppID
		}
		if botCfg.AppSecret != "" {
			appSecret = botCfg.AppSecret
		}
	}
	acfg.Extras["app_id"] = appID
	acfg.Extras["app_secret"] = appSecret

	sttCfg := platformCfg.STTConfig
	ttsCfg := platformCfg.TTSConfig
	if botCfg != nil {
		sttCfg = botCfg.STTConfig
		ttsCfg = botCfg.TTSConfig
	}
	if t := buildFeishuTranscriber(sttCfg, appID, appSecret, log); t != nil {
		acfg.Extras["transcriber"] = t
	}
	if p := buildFeishuTTSPipeline(ttsCfg, appID, appSecret, log); p != nil {
		acfg.Extras["tts_pipeline"] = p
	}
}

func buildFeishuTranscriber(sttCfg config.STTConfig, appID, appSecret string, log *slog.Logger) stt.Transcriber {
	switch sttCfg.Provider {
	case config.STTProviderFeishu:
		client := lark.NewClient(appID, appSecret)
		return feishu.NewFeishuSTT(client, log)
	case config.STTProviderLocal:
		return buildLocalSTT("feishu", sttCfg, log)
	case config.STTProviderFeishuLocal:
		if sttCfg.LocalCmd == "" {
			log.Warn("feishu: stt_provider=feishu+local but stt_local_cmd is empty, using feishu only")
			client := lark.NewClient(appID, appSecret)
			return feishu.NewFeishuSTT(client, log)
		}
		client := lark.NewClient(appID, appSecret)
		return stt.NewFallbackSTT(
			feishu.NewFeishuSTT(client, log),
			buildLocalSTT("feishu", sttCfg, log),
			log,
		)
	default:
		return nil
	}
}

func buildSlackTranscriber(sttCfg config.STTConfig, log *slog.Logger) stt.Transcriber {
	if sttCfg.Provider != config.STTProviderLocal {
		return nil
	}
	return buildLocalSTT("slack", sttCfg, log)
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

	cacheKey := expandedCmd

	if existing, ok := sttCache[cacheKey]; ok {
		if existing.Refs() <= 0 {
			delete(sttCache, cacheKey)
		} else {
			log.Debug(platform+": reusing shared stt transcriber", "cmd", expandedCmd)
			return existing.Acquire()
		}
	}

	if cfg.IsPersistent() {
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

func buildSlackTTSPipeline(ttsCfg config.TTSConfig, botToken, appToken string, log *slog.Logger) *slack.TTSPipeline {
	if !ttsCfg.TTSEnabled {
		return nil
	}

	synth := buildTTSSynthesizer(ttsCfg, log)
	if synth == nil {
		return nil
	}

	client := slackapi.New(botToken, slackapi.OptionAppLevelToken(appToken))
	return slack.NewTTSPipeline(synth, client, ttsCfg.MaxChars, log)
}

func buildFeishuTTSPipeline(ttsCfg config.TTSConfig, appID, appSecret string, log *slog.Logger) *feishu.TTSPipeline {
	if !ttsCfg.TTSEnabled {
		return nil
	}

	synth := buildTTSSynthesizer(ttsCfg, log)
	if synth == nil {
		return nil
	}

	client := lark.NewClient(appID, appSecret)
	return feishu.NewTTSPipeline(synth, client, ttsCfg.MaxChars, log)
}

// ttsCacheKey derives a deterministic cache key from TTS configuration.
// Synthesizers with identical config share the same underlying process.
func ttsCacheKey(provider string, cfg config.TTSConfig) string {
	return fmt.Sprintf("%s|%s|%s|%d|%d|%d",
		provider, cfg.Voice, cfg.MossModelDir, cfg.MossPort, cfg.MossCpuThreads, int(cfg.MossIdleTimeout))
}

// buildTTSSynthesizer creates or reuses a shared TTS synthesizer from config.
// When both Slack and Feishu use the same TTS config, they share the same
// MOSS sidecar process, avoiding port conflicts.
func buildTTSSynthesizer(ttsCfg config.TTSConfig, log *slog.Logger) tts.Synthesizer {
	provider := ttsCfg.TTSProvider
	cacheKey := ttsCacheKey(provider, ttsCfg)

	ttsCacheMu.Lock()
	defer ttsCacheMu.Unlock()

	if existing, ok := ttsCache[cacheKey]; ok {
		if acquired := existing.TryAcquire(); acquired != nil {
			log.Debug("tts: reusing shared synthesizer", "key", cacheKey)
			return acquired
		}
		delete(ttsCache, cacheKey)
	}

	var synth tts.Synthesizer
	switch provider {
	case "edge":
		synth = tts.NewEdgeSynthesizer(ttsCfg.Voice, log)
	case "edge+moss":
		synth = tts.NewConfiguredSynthesizer(tts.SynthesizerConfig{
			EdgeVoice:       ttsCfg.Voice,
			MossModelDir:    ttsCfg.MossModelDir,
			MossVoice:       ttsCfg.MossVoice,
			MossPort:        ttsCfg.MossPort,
			MossCpuThreads:  ttsCfg.MossCpuThreads,
			MossIdleTimeout: ttsCfg.MossIdleTimeout,
		}, log)
	default:
		log.Warn("tts: unknown provider, TTS disabled", "provider", provider)
		return nil
	}

	shared := tts.NewSharedSynthesizer(synth)
	ttsCache[cacheKey] = shared
	return shared
}
