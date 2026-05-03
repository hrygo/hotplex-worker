# Per-Bot Agent Config Specification

> Issue: #127
> Status: Final
> Created: 2026-05-03
> Reviewed: 2026-05-03

## 1. Overview

Replace the existing platform-suffix agent config mechanism (`SOUL.slack.md`) with a directory-based 3-level fallback system supporting per-bot configuration granularity. Additionally, include botID in the session key derivation to ensure different bots in the same channel produce separate sessions.

## 2. Requirements

### 2.1 Per-File Granularity Fallback

Each config file (SOUL.md, AGENTS.md, SKILLS.md, USER.md, MEMORY.md) resolves independently through:

```
1. dir/{platform}/{botID}/{file}    ← bot-level (highest priority)
2. dir/{platform}/{file}            ← platform-level
3. dir/{file}                       ← global-level (fallback)
```

If a file exists at a higher priority level, it is used; lower levels are **not** appended (no merge/overlay). An empty file (content is empty after frontmatter stripping) is treated as "not found" and falls through to the next level — this is consistent with current behavior.

### 2.2 Four Configuration Dimensions

| Dimension | Example | Directory |
|-----------|---------|-----------|
| Global (system default) | All platforms | `agent-configs/SOUL.md` |
| Platform | Slack | `agent-configs/slack/SOUL.md` |
| Bot | Slack bot U12345 | `agent-configs/slack/U12345/SOUL.md` |
| WebChat | JWT bot_id | `agent-configs/webchat/my-bot/SOUL.md` |

### 2.3 Bot Identity — Direct botID as Directory Name

BotID is used **directly** as the subdirectory name. No config mapping needed.

```
~/.hotplex/agent-configs/
├── SOUL.md / AGENTS.md / ...          ← 全局默认
├── slack/                             ← Slack 平台默认
│   ├── SOUL.md / ...
│   ├── U12345/                        ← Slack bot (UserID from auth.test)
│   │   └── SOUL.md / ...
│   └── U67890/                        ← 另一个 Slack bot
│       └── SOUL.md / ...
├── feishu/                            ← 飞书平台默认
│   ├── ou_abc123/                     ← 飞书 bot (OpenID from Bot API)
│   │   └── SOUL.md / ...
└── webchat/                           ← WebChat 默认
    └── SOUL.md / ...
```

Platform bot IDs:
- **Slack**: `auth.test` → `UserID` (e.g., `U12345`)
- **Feishu**: Bot API → `OpenID` (e.g., `ou_abc123`)
- **WebChat**: JWT claim `bot_id` (e.g., `webchat-premium`)

### 2.4 BotID in Session Key Derivation

**Current**: `DerivePlatformSessionKey(ownerID, wt, PlatformContext{Platform, TeamID, ChannelID, ThreadTS, ChatID, UserID, WorkDir})` — no botID. Two bots in the same Slack channel responding to the same user **collide on the same session ID**.

**After**: `PlatformContext` gains a `BotID` field. It participates in the UUIDv5 hash so each bot gets its own session. This is critical for multi-bot deployments.

```go
// PlatformContext — 新增 BotID
type PlatformContext struct {
    Platform string
    TeamID    string
    ChannelID string
    ThreadTS  string
    ChatID    string
    UserID    string
    WorkDir   string
    BotID     string  // NEW
}
```

Session key derivation includes botID (after platform, before platform-specific fields):

```
// Before: owner|wt|slack|teamID|channelID|threadTS|userID|workDir
// After:  owner|wt|slack|botID|teamID|channelID|threadTS|userID|workDir
```

**Impact**: This is a **breaking change for session ID stability**. Existing platform sessions will get different IDs after upgrade. Sessions in TERMINATED state will not resume — users will start fresh sessions automatically (existing crash recovery handles this). This is acceptable for a minor version bump.

### 2.5 Breaking Changes Summary

| Change | Impact | Migration |
|--------|--------|-----------|
| `SOUL.<platform>.md` suffix removed | Platform-specific configs not loaded | Move to `slack/SOUL.md` directory |
| `PlatformContext.BotID` added to session key | Existing session IDs change | Automatic — fresh sessions created on next message |
| Size limits unchanged | `MaxFileChars = 8,000`, `MaxTotalChars = 40,000` | N/A |

### 2.6 Versioning

Target: next minor version (e.g., v1.5.0). Both changes (suffix removal + session key) are breaking but have automatic fallback behavior. Document in CHANGELOG as **BREAKING** section.

## 3. API Changes

### 3.1 agentconfig.Load

```go
// Before
func Load(dir, platform string) (*AgentConfigs, error)

// After
func Load(dir, platform, botID string) (*AgentConfigs, error)
```

**Path safety**: `botID` is validated at entry — must satisfy `filepath.Base(botID) == botID` (no path separators or `..`). This prevents path traversal even with user-controlled WebChat JWT bot_id values.

**Internal changes**: `loadFile` and `loadFileWithErrorCount` are replaced by `resolveFile`. The `Load` function's total-size tracking logic remains unchanged.

### 3.2 agentconfig.resolveFile (NEW)

```go
// resolveFile implements the 3-level fallback for a single config file.
// Returns the content of the first found file, or ("", nil) if none exist.
func resolveFile(dir, platform, botID, fileName string) (string, error) {
    // 1. Bot-level: dir/platform/botID/fileName
    if botID != "" && platform != "" {
        content, err := readFile(filepath.Join(dir, platform, botID), fileName)
        if err != nil || content != "" {
            return content, err
        }
    }
    // 2. Platform-level: dir/platform/fileName
    if platform != "" {
        content, err := readFile(filepath.Join(dir, platform), fileName)
        if err != nil || content != "" {
            return content, err
        }
    }
    // 3. Global: dir/fileName
    return readFile(dir, fileName)
}
```

### 3.3 config.AgentConfig — No Changes

```go
type AgentConfig struct {
    Enabled   bool   `mapstructure:"enabled"`
    ConfigDir string `mapstructure:"config_dir"`
}
```

### 3.4 session.PlatformContext — BotID Added

```go
type PlatformContext struct {
    Platform  string
    TeamID    string
    ChannelID string
    ThreadTS  string
    ChatID    string
    UserID    string
    WorkDir   string
    BotID     string  // NEW: included in session key derivation
}

// FromMap — add bot_id parsing
func (pc *PlatformContext) FromMap(m map[string]string) {
    // ... existing fields ...
    pc.BotID = m["bot_id"]  // NEW
}
```

### 3.5 session.DerivePlatformSessionKey

```go
// BotID is included after Platform in the hash input
// Before: owner|wt|slack|teamID|channelID|...
// After:  owner|wt|slack|botID|teamID|channelID|...
```

### 3.6 PlatformAdapterInterface

```go
// Added method
type PlatformAdapterInterface interface {
    // ... existing ...
    GetBotID() string
}
```

### 3.7 messaging.SessionStarter

```go
// Before
StartPlatformSession(ctx, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string) error

// After
StartPlatformSession(ctx, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error
```

### 3.8 messaging.Bridge

```go
// Bridge gains an adapter reference for lazy botID resolution
type Bridge struct {
    // ... existing fields ...
    adapter PlatformAdapterInterface  // NEW: set after adapter.Start()
}

func (b *Bridge) SetAdapter(a PlatformAdapterInterface)
```

### 3.9 gateway.BridgeDeps — No Changes

No `AgentBotMap` needed — botID flows directly from adapter to `agentconfig.Load`.

### 3.10 bridge.injectAgentConfig

```go
// Before
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform string)

// After
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botID string)
```

### 3.11 messaging.Bridge.MakeSlackEnvelope / MakeFeishuEnvelope

```go
// Before
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text, workDir string) *events.Envelope

// After — botID parameter added
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text, workDir, botID string) *events.Envelope
```

`PlatformContext` in both methods now includes `BotID` field, which flows into `DerivePlatformSessionKey` and envelope metadata.

### 3.12 messaging.Bridge.MakeEnvelope

```go
func (b *Bridge) MakeEnvelope(userID, text string, pctx session.PlatformContext) *events.Envelope {
    // ... existing ...
    if pctx.BotID != "" {
        md["bot_id"] = pctx.BotID
    }
    // ...
}
```

### 3.13 ExtractPlatformKeys — bot_id Extraction

```go
// Both Slack and Feishu cases must extract bot_id:
if v, ok := md["bot_id"].(string); ok && v != "" {
    pk["bot_id"] = v
}
```

This ensures the `platformKey` map persisted to DB includes `bot_id`, enabling `FromMap` to reconstruct `PlatformContext.BotID` on resume.

## 4. Data Flow

### 4.1 Slack/Feishu Path

```
Adapter.Start()
  └─> auth.test / Bot API → adapter.botID / adapter.botOpenID

Adapter.HandleTextMessage()
  └─> a.Bridge().MakeSlackEnvelope(teamID, ch, threadTS, userID, text, workDir, a.botID)
        └─> MakeEnvelope(userID, text, PlatformContext{..., BotID: botID})
              └─> DerivePlatformSessionKey(userID, wt, pctx)  ← botID in hash
              └─> metadata["bot_id"] = botID

messaging.Bridge.Handle()
  └─> adapter.GetBotID() → botID
  └─> starter.StartPlatformSession(..., botID)
        └─> gateway.Bridge.StartPlatformSession(..., botID)
              └─> startOrResumeOnInUse(..., botID)
                    └─> StartSession(..., botID, ...)
                          └─> sm.CreateWithBot(..., botID, ...)
                          └─> createAndLaunchWorker(params{botID})
                                └─> injectAgentConfig(info, platform, botID)
                                      └─> agentconfig.Load(dir, platform, botID)
                                            └─> resolveFile per file:
                                                  dir/platform/botID/SOUL.md
                                                  dir/platform/SOUL.md
                                                  dir/SOUL.md
```

### 4.2 WebChat Path

```
JWT token → claims.BotID → conn.botID
  └─> starter.StartSession(..., c.botID, ...)
        └─> (same as above from StartSession)

WebChat without botID (c.botID == ""):
  └─> Load(dir, "webchat", "") → resolves from webchat/ directory (platform-level)
```

### 4.3 Resume Path

```
si.BotID (from DB) → used in workerLaunchParams.botID
  └─> injectAgentConfig(info, platform, si.BotID)
```

Three `createAndLaunchWorker` call sites must pass botID:
1. `StartSession` (bridge.go:120) — botID from parameter
2. `resumeWithOpts` (bridge.go:254) — `botID: si.BotID` from DB
3. `attemptResumeFallback` (bridge.go:787) — `botID: si.BotID` from DB

## 5. Implementation Phases

### Phase 1: Core Fallback Logic
- Modify `agentconfig/loader.go`:
  - New `Load(dir, platform, botID)` signature
  - New `resolveFile` function implementing 3-level per-file fallback
  - Add botID path safety validation (`filepath.Base(botID) == botID`)
  - Remove `loadFile` / `loadFileWithErrorCount` (replaced by `resolveFile`)
  - Remove suffix-append logic (`SOUL.<platform>.md`)
- Update `agentconfig/loader_test.go`:
  - Replace suffix-append tests with 3-level fallback tests
  - Add path traversal test for botID
  - Add empty-file-equals-missing test
  - Add flat-directory backward-compatibility test

### Phase 2: BotID Propagation — Adapter Layer
- Add `GetBotID()` to `PlatformAdapterInterface` (`messaging/platform_adapter.go`)
- Implement in Slack adapter: `return a.botID`
- Implement in Feishu adapter: `return a.botOpenID`
- Add `adapter` field + `SetAdapter()` to `messaging.Bridge`
- Wire `SetAdapter()` in `messaging_init.go` after `adapter.Start()`
- Update `messaging.SessionStarter` interface: +botID parameter
- Update `messaging.Bridge.Handle()`: extract botID from adapter, pass to `StartPlatformSession`

### Phase 3: Session Key Derivation
- Add `BotID` field to `session.PlatformContext`
- Update `FromMap()`: parse `m["bot_id"]`
- Update `DerivePlatformSessionKey()`: include botID in hash input
- Update `MakeSlackEnvelope` / `MakeFeishuEnvelope`: +botID parameter
- Update all callers of `MakeSlackEnvelope` / `MakeFeishuEnvelope` in adapter files
- Update `MakeEnvelope()`: include botID in metadata as `"bot_id"`
- Update `ExtractPlatformKeys`: extract `bot_id` from metadata for both Slack and Feishu cases

### Phase 4: Gateway Bridge Integration
- Update `StartPlatformSession`: +botID parameter
- Update `startOrResumeOnInUse`: use botID instead of hardcoded `""`
- Update `injectAgentConfig`: +botID parameter, pass to `agentconfig.Load`
- Add `botID` to `workerLaunchParams`
- Update all 3 `createAndLaunchWorker` call sites to pass botID:
  - `StartSession` (bridge.go:120): from parameter
  - `resumeWithOpts` (bridge.go:254): `si.BotID` from DB
  - `attemptResumeFallback` (bridge.go:787): `si.BotID` from DB

### Phase 5: CLI & Skills Updates
- Update `internal/cli/onboard/wizard.go` `stepAgentConfig()`: mention directory structure
- Update `cmd/hotplex/onboard.go` `displayAgentConfigPanel()`: update guidance text
- Update `.agent/skills/hotplex-setup/SKILL.md`: directory structure, bot subdirectories, env vars
- Add deprecation warning in `cmd/hotplex/gateway_run.go`: one-time scan for `*.{platform}.md` suffix files at startup
- Add `AgentConfigChecker` to `internal/cli/checkers/`: detect old suffix files, validate directory structure

### Phase 6: Documentation & Rules — Use Section 7 as Checklist
- Update all files listed in Section 7 below
- Full test suite pass (`make check`)
- Cross-platform build pass

## 6. Acceptance Criteria

| ID | Criterion | Validation |
|----|-----------|------------|
| PBAC-001 | `Load(dir, "slack", "U12345")` resolves SOUL.md from `slack/U12345/` first | Unit test |
| PBAC-002 | Missing bot-level file falls back to platform-level | Unit test |
| PBAC-003 | Missing platform-level file falls back to global | Unit test |
| PBAC-004 | Each file resolves independently (SOUL from bot-level, AGENTS from platform-level) | Unit test |
| PBAC-005 | Flat directory (no subdirs) produces identical results to current behavior | Unit test |
| PBAC-006 | `SOUL.slack.md` suffix files are no longer loaded | Unit test |
| PBAC-007 | `Load(dir, "slack", "../etc")` returns error (path traversal blocked) | Unit test |
| PBAC-008 | Empty file (frontmatter only) falls through to next level | Unit test |
| PBAC-009 | Slack adapter exposes botID via `GetBotID()` | Unit test |
| PBAC-010 | Feishu adapter exposes botID via `GetBotID()` | Unit test |
| PBAC-011 | `MakeSlackEnvelope` includes botID in PlatformContext | Unit test |
| PBAC-012 | `DerivePlatformSessionKey` with different botIDs produces different session IDs | Unit test |
| PBAC-013 | `StartPlatformSession` receives botID from adapter | Integration test |
| PBAC-014 | `injectAgentConfig` passes botID directly to `agentconfig.Load` | Unit test |
| PBAC-015 | WebChat JWT bot_id resolves to correct directory; empty bot_id uses platform-level | Integration test |
| PBAC-016 | Resume sessions reuse persisted botID for config resolution | Integration test |
| PBAC-017 | All 3 `createAndLaunchWorker` call sites pass botID | Unit test |
| PBAC-018 | Gateway startup logs deprecation warning when `*.{platform}.md` suffix files exist | Unit test |
| PBAC-019 | `hotplex doctor` detects old suffix files and suggests migration | Unit test |
| PBAC-020 | `make check` passes (lint + test + build) | CI |
| PBAC-021 | Cross-platform build passes (linux/macOS/windows) | CI |
| PBAC-022 | `MakeFeishuEnvelope` includes botID in PlatformContext and metadata | Unit test |
| PBAC-023 | `MakeEnvelope` includes `bot_id` in metadata map | Unit test |
| PBAC-024 | `ExtractPlatformKeys` extracts `bot_id` from metadata for both Slack and Feishu | Unit test |
| PBAC-025 | `messaging.Bridge.Handle()` extracts botID from adapter via `GetBotID()`, passes to `StartPlatformSession` | Unit test |
| PBAC-026 | `messaging_init.go` calls `SetAdapter(adapter)` after `adapter.Start()` | Integration test |
| PBAC-027 | `FromMap` reconstructs `BotID` from persisted `platformKey["bot_id"]` | Unit test |

## 7. Full Impact Map — Files Requiring Changes

### 7.1 Core Code Changes

| File | Change |
|------|--------|
| `internal/agentconfig/loader.go` | **核心改动**: `Load` +botID, `resolveFile` 3级 fallback, path safety check, 删除 `loadFile`/`loadFileWithErrorCount`/suffix-append |
| `internal/agentconfig/loader_test.go` | 替换 suffix-append 测试为 3级 fallback 测试, +path traversal +empty file tests |
| `internal/session/key.go` | `PlatformContext` +BotID, `FromMap` +bot_id, `DerivePlatformSessionKey` hash input +botID |
| `internal/messaging/platform_adapter.go` | `PlatformAdapterInterface` +`GetBotID()`, `SessionStarter` +botID, `ExtractPlatformKeys` +`bot_id` extraction |
| `internal/messaging/slack/adapter.go` | 实现 `GetBotID()`, 更新 `MakeSlackEnvelope` 调用 +botID 参数 |
| `internal/messaging/feishu/adapter.go` | 实现 `GetBotID()`, 更新 `MakeFeishuEnvelope` 调用 +botID 参数 |
| `internal/messaging/bridge.go` | `Bridge` +adapter field +`SetAdapter()`, `MakeSlackEnvelope`/`MakeFeishuEnvelope` +botID, `MakeEnvelope` metadata +bot_id, `Handle()` 提取 botID |
| `internal/gateway/bridge.go` | `injectAgentConfig` +botID, `startOrResumeOnInUse` 使用 botID, `workerLaunchParams` +botID, 3个 `createAndLaunchWorker` 调用点 +botID |
| `cmd/hotplex/messaging_init.go` | `adapter.Start()` 后调用 `msgBridge.SetAdapter(adapter)` |
| `cmd/hotplex/gateway_run.go` | 启动时扫描旧 suffix 文件, 日志 deprecation warning |

### 7.2 CLI & Wizard Changes

| File | Change |
|------|--------|
| `internal/cli/onboard/wizard.go` | `stepAgentConfig()` 更新：说明目录结构（平台子目录、bot 子目录） |
| `internal/cli/onboard/agentconfig_templates.go` | 保持不变（全局模板仍在根目录生成） |
| `cmd/hotplex/onboard.go` | `displayAgentConfigPanel()` 更新说明文案，引导用户了解目录结构 |
| `internal/cli/checkers/` **新增** | `AgentConfigChecker` — 检测旧 suffix 文件并提示迁移，验证目录结构合法性 |

### 7.3 Skills Changes

| File | Change |
|------|--------|
| `.agent/skills/hotplex-setup/SKILL.md` | 更新 agent-config 配置说明：目录结构、bot 子目录用法、环境变量 |
| `.agent/skills/hotplex-release/SKILL.md` | 更新 config area 列表描述 |
| `.agent/skills/hotplex-arch-analyzer/SKILL.md` | 更新 agentconfig 模块描述 |

### 7.4 Rule Files Changes

| File | Change |
|------|--------|
| `.agent/rules/agentconfig.md` | **核心更新**: 替换 suffix-append 文档为目录 fallback 文档，更新目录结构图、加载逻辑说明、大小限制 |
| `.agent/rules/golang.md` | 更新 cross-reference |
| `.agent/rules/cli.md` | 更新 checker 列表（新增 AgentConfigChecker） |
| `.agent/rules/session.md` | 更新 session key 派生说明（+botID） |

### 7.5 Embedded Content Changes

| File | Change |
|------|--------|
| `internal/agentconfig/META-COGNITION.md` | **必须更新**: "Agent Config 架构" 段落 — 删除 "平台变体：SOUL.slack.md"，改为 "三级目录 fallback：全局 → 平台 → bot" |

### 7.6 Documentation Changes

| File | Change |
|------|--------|
| `docs/architecture/Agent-Config-Design.md` | **主要设计文档更新**: 替换 suffix-append 架构为目录 fallback 架构 |
| `docs/Reference-Manual.md` | 更新 B/C 通道描述、平台变体说明 |
| `docs/User-Manual.md` | 更新文件描述、目录用法 |
| `docs/management/Config-Reference.md` | 更新文件列表、目录结构 |
| `docs/Architecture-Design.md` | 更新 B/C 双通道概述 |
| `docs/specs/Per-Bot-Agent-Config-Spec.md` | 本 spec（实施后标记 Final） |

### 7.7 Root-Level Docs Changes

| File | Change |
|------|--------|
| `AGENTS.md` (→ `CLAUDE.md`) | 更新 agentconfig 模块描述、Agent Config section、session key 派生 |
| `README.md` | 更新 agent_config 配置表 |
| `README_zh.md` | 同步中文版 |
| `INSTALL.md` | 更新目录描述 |

### 7.8 Config Files Changes

| File | Change |
|------|--------|
| `configs/config.yaml` | agent_config 段新增注释说明目录结构 |
| `configs/env.example` | 更新注释说明 |
| `configs/README.md` | 更新 agent_config section |

## 8. Migration Guide

### Step 1: Move platform suffix files to directories

```bash
mkdir -p ~/.hotplex/agent-configs/slack
mv ~/.hotplex/agent-configs/SOUL.slack.md ~/.hotplex/agent-configs/slack/SOUL.md
mv ~/.hotplex/agent-configs/AGENTS.slack.md ~/.hotplex/agent-configs/slack/AGENTS.md
# ... same for other files and platforms
```

### Step 2: Create bot-specific configs (optional)

```bash
# Use botID (from Slack auth.test / Feishu Bot API) as directory name
mkdir -p ~/.hotplex/agent-configs/slack/U12345
# Only create files that differ from platform-level defaults
vim ~/.hotplex/agent-configs/slack/U12345/SOUL.md
```

### Step 3: Verify with doctor

```bash
hotplex doctor  # AgentConfigChecker detects old suffix files and suggests migration
```

### Session ID Impact

Existing sessions will get new IDs (botID included in derivation). Terminated sessions will not resume — fresh sessions are created automatically on next message. No data loss; only conversation history continuity for active sessions is affected.

## 9. Risks

| Risk | Mitigation |
|------|------------|
| Breaking: `SOUL.<platform>.md` users | Migration guide + startup deprecation warning + doctor checker |
| Breaking: session IDs change (botID in key) | Automatic recovery via orphan resume path; documented in CHANGELOG |
| Performance: 3x stat calls per file | Negligible — runs once per session creation, not per message |
| BotID not yet available at adapter creation time | Lazy: `GetBotID()` called in `Handle()` after `Start()` |
| BotID path traversal (WebChat JWT) | `filepath.Base(botID) == botID` validation at `Load` entry |
| BotID special chars | Slack (`U[0-9A-Z]+`) and Feishu (`ou_[a-z0-9]+`) safe; WebChat validated |
| Doc drift across 30+ files | Systematic impact map (Section 7) used as implementation checklist |
