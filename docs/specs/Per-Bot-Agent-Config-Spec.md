# Per-Bot Agent Config Specification

> Issue: #127
> Status: Draft
> Created: 2026-05-03

## 1. Overview

Replace the existing platform-suffix agent config mechanism (`SOUL.slack.md`) with a directory-based 3-level fallback system supporting per-bot configuration granularity.

## 2. Requirements

### 2.1 Per-File Granularity Fallback

Each config file (SOUL.md, AGENTS.md, SKILLS.md, USER.md, MEMORY.md) resolves independently through:

```
1. dir/{platform}/{botID}/{file}    ← bot-level (highest priority)
2. dir/{platform}/{file}            ← platform-level
3. dir/{file}                       ← global-level (fallback)
```

If a file exists at a higher priority level, it is used; lower levels are **not** appended (no merge/overlay).

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

### 2.4 Breaking Change

The existing `SOUL.<platform>.md` suffix-append mechanism is **removed**. Users must migrate:

| Before | After |
|--------|-------|
| `SOUL.slack.md` | `slack/SOUL.md` |
| `AGENTS.feishu.md` | `feishu/AGENTS.md` |

## 3. API Changes

### 3.1 agentconfig.Load

```go
// Before
func Load(dir, platform string) (*AgentConfigs, error)

// After — botID is used directly as directory name
func Load(dir, platform, botID string) (*AgentConfigs, error)
```

### 3.2 config.AgentConfig — No Changes

`AgentConfig` remains unchanged — no `bots` mapping needed since botID is used directly as directory name.

```go
type AgentConfig struct {
    Enabled   bool   `mapstructure:"enabled"`
    ConfigDir string `mapstructure:"config_dir"`
}
```

### 3.3 PlatformAdapterInterface

```go
// Added method
type PlatformAdapterInterface interface {
    // ... existing ...
    GetBotID() string
}
```

### 3.4 messaging.SessionStarter

```go
// Before
StartPlatformSession(ctx, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string) error

// After
StartPlatformSession(ctx, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error
```

### 3.5 gateway.BridgeDeps — No Changes

No `AgentBotMap` needed — botID flows directly from adapter to `agentconfig.Load`.

### 3.6 bridge.injectAgentConfig

```go
// Before
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform string)

// After — botID passed directly to Load, no mapping step
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform, botID string)
```

## 4. Data Flow

### 4.1 Slack/Feishu Path

```
Adapter.Start()
  └─> auth.test / Bot API → adapter.botID / adapter.botOpenID

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
```

### 4.3 Resume Path

```
si.BotID (from DB) → used in workerLaunchParams.botID
  └─> injectAgentConfig(info, platform, botID)
```

## 5. Implementation Phases

### Phase 1: Core Fallback Logic
- Modify `agentconfig/loader.go`: new `Load` signature, `resolveFile` function, remove suffix-append
- Update `agentconfig/loader_test.go`: replace suffix-append tests with 3-level fallback tests
- Tests: 3-level fallback, per-file independence, backward compatibility

### Phase 2: BotID Propagation
- Add `GetBotID()` to `PlatformAdapterInterface` and implementations (Slack, Feishu)
- Extend `messaging.SessionStarter` with `botID` parameter
- Update `messaging.Bridge.Handle()` to extract and pass botID
- Wire adapter reference in `messaging_init.go`

### Phase 3: Gateway Bridge Integration
- Update `StartPlatformSession`, `startOrResumeOnInUse`, `injectAgentConfig` signatures
- Add `botID` to `workerLaunchParams`
- Wire in `gateway_run.go` (no new BridgeDeps fields needed)

### Phase 4: CLI & Skills Updates
- Update onboard wizard (`internal/cli/onboard/wizard.go`)
- Update onboard display panel (`cmd/hotplex/onboard.go`)
- Update hotplex-setup skill
- Add migration deprecation warning in gateway startup
- Add agent-config doctor checker

### Phase 5: Documentation & Rules Updates
- Update all rule files, design docs, user-facing docs
- Full test suite pass

## 6. Acceptance Criteria

| ID | Criterion | Validation |
|----|-----------|------------|
| PBAC-001 | `Load(dir, "slack", "U12345")` resolves SOUL.md from `slack/U12345/` first | Unit test |
| PBAC-002 | Missing bot-level file falls back to platform-level | Unit test |
| PBAC-003 | Missing platform-level file falls back to global | Unit test |
| PBAC-004 | Each file resolves independently (SOUL from bot-level, AGENTS from platform-level) | Unit test |
| PBAC-005 | Flat directory (no subdirs) produces identical results to current behavior | Unit test |
| PBAC-006 | `SOUL.slack.md` suffix files are no longer loaded | Unit test |
| PBAC-007 | Slack adapter exposes botID via `GetBotID()` | Unit test |
| PBAC-008 | Feishu adapter exposes botID via `GetBotID()` | Unit test |
| PBAC-009 | `StartPlatformSession` receives botID from adapter | Integration test |
| PBAC-010 | `injectAgentConfig` passes botID directly to `agentconfig.Load` | Unit test |
| PBAC-011 | WebChat JWT bot_id resolves to correct directory | Integration test |
| PBAC-012 | Resume sessions reuse persisted botID for config resolution | Integration test |
| PBAC-013 | `make check` passes (lint + test + build) | CI |
| PBAC-014 | Cross-platform build passes (linux/macOS/windows) | CI |

## 7. Full Impact Map — Files Requiring Changes

### 7.1 Core Code Changes

| File | Change |
|------|--------|
| `internal/agentconfig/loader.go` | **核心改动**: `Load` 签名变更, `resolveFile` 3级 fallback, 删除 suffix-append |
| `internal/agentconfig/loader_test.go` | 替换 suffix-append 测试为 3级 fallback 测试 |
| `internal/messaging/platform_adapter.go` | `PlatformAdapterInterface` +`GetBotID()`, `SessionStarter` +botID |
| `internal/messaging/slack/adapter.go` | 实现 `GetBotID()` 返回 `a.botID` |
| `internal/messaging/feishu/adapter.go` | 实现 `GetBotID()` 返回 `a.botOpenID` |
| `internal/messaging/bridge.go` | `Handle()` 提取 botID, 传递到 `StartPlatformSession` |
| `internal/gateway/bridge.go` | `injectAgentConfig` +botID, `startOrResumeOnInUse` 使用 botID, `workerLaunchParams` +botID |
| `cmd/hotplex/messaging_init.go` | 注入 adapter 引用到 messaging.Bridge |
| `cmd/hotplex/gateway_run.go` | 无新字段，但需确认 botID 流转正确 |

### 7.2 CLI & Wizard Changes

| File | Change |
|------|--------|
| `internal/cli/onboard/wizard.go` | `stepAgentConfig()` 更新：说明目录结构（平台子目录、bot 子目录） |
| `internal/cli/onboard/agentconfig_templates.go` | 保持不变（全局模板仍在根目录生成） |
| `cmd/hotplex/onboard.go` | `displayAgentConfigPanel()` 更新说明文案，引导用户了解目录结构 |
| `internal/cli/checkers/` | **新增**: `AgentConfigChecker` — 检测旧 suffix 文件并提示迁移，验证目录结构合法性 |
| `cmd/hotplex/gateway_run.go` | **新增**: 启动时检测旧 `*.{platform}.md` suffix 文件，日志 deprecation warning |

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
| `.agent/rules/golang.md` | 更新 cross-reference: "Agent Config -> see agentconfig.md" |
| `.agent/rules/cli.md` | 更新 checker 列表（新增 AgentConfigChecker） |

### 7.5 Embedded Content Changes

| File | Change |
|------|--------|
| `internal/agentconfig/META-COGNITION.md` | **必须更新**: "Agent Config 架构" 段落 — 删除 "平台变体：SOUL.slack.md"，改为 "三级目录 fallback：全局 → 平台 → bot" |

### 7.6 Documentation Changes

| File | Change |
|------|--------|
| `docs/architecture/Agent-Config-Design.md` | **主要设计文档更新**: 替换 suffix-append 架构为目录 fallback 架构，更新示例、文件树、迁移说明 |
| `docs/Reference-Manual.md` | 更新 B/C 通道描述、平台变体说明、config 示例 |
| `docs/User-Manual.md` | 更新目录位置、文件描述、平台变体用法（删除 suffix 说明，改为目录说明） |
| `docs/management/Config-Reference.md` | 更新 B/C 通道文件列表、目录结构、环境变量说明 |
| `docs/Architecture-Design.md` | 更新 B/C 双通道概述 |
| `docs/specs/Per-Bot-Agent-Config-Spec.md` | 本 spec 文件（实施后标记为 Final） |

### 7.7 Root-Level Docs Changes

| File | Change |
|------|--------|
| `AGENTS.md` (→ `CLAUDE.md`) | 更新 agentconfig 模块描述、Agent Config section、文件加载说明、平台变体段落 |
| `README.md` | 更新 agent_config 配置表说明 |
| `README_zh.md` | 同步中文版更新 |
| `INSTALL.md` | 更新 `~/.hotplex/agent-configs/` 目录描述 |

### 7.8 Config Files Changes

| File | Change |
|------|--------|
| `configs/config.yaml` | agent_config 段新增注释说明目录结构 |
| `configs/env.example` | 更新 `HOTPLEX_AGENT_CONFIG_DIR` 注释说明 |
| `configs/README.md` | 更新 agent_config section 文档 |

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
hotplex doctor  # New AgentConfigChecker detects old suffix files and suggests migration
```

## 9. Risks

| Risk | Mitigation |
|------|------------|
| Breaking change for `SOUL.<platform>.md` users | Clear migration guide + startup deprecation warning + doctor checker |
| Performance: 3x stat calls per file | Negligible — runs once per session creation, not per message |
| BotID not yet available at adapter creation time | Lazy resolution: `GetBotID()` called in `Handle()`, after `Start()` |
| BotID may contain special chars | Slack UserID (`U[0-9A-Z]+`) and Feishu OpenID (`ou_[a-z0-9]+`) are safe for directory names |
| Doc drift across 30+ files | Systematic impact map (Section 7) + checklist in release skill |
