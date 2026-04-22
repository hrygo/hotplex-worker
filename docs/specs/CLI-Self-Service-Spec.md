# HotPlex CLI 自配置/自检测/自修复设计文档

> **日期**: 2026-04-22
> **状态**: Draft
> **产品名**: `hotplex` 
> **目标用户**: 开发者/运维

## 概述

为 HotPlex 引入 CLI 级别的自配置（`onboard`）、自检测（`doctor`）、自修复（`doctor --fix`）和安全审计（`security`）能力。基于 Cobra 框架重构 CLI 入口，采用 Checker 接口模式实现模块化诊断。

参考了 OpenClaw CLI、Homebrew doctor、Docker Desktop diagnostics 等工具的设计模式。

## 1. 命令树

```
hotplex
├── serve              # 启动 gateway 服务（原 main 逻辑）
│   └── -c, --config   # 配置文件路径 (default: configs/config.yaml)
│   └── --dev          # 开发模式
├── onboard            # 交互式配置向导
│   └── --non-interactive  # 非交互模式
│   └── --force            # 覆盖已有配置
├── doctor             # 诊断检查
│   └── --fix          # 自动修复
│   └── --verbose      # 详细输出（含修复命令执行详情）
│   └── --json         # JSON 结构化输出
│   └── --category     # 只检查指定分类
├── security           # 安全审计
│   └── --fix          # 自动修复安全问题
└── version            # 版本信息
```

无子命令时默认执行 `serve`（向后兼容）。

## 2. 目录结构

```
cmd/
  worker/
    main.go              # Cobra root cmd + 子命令注册
    serve.go             # 原 main.go 核心启动逻辑
    onboard.go           # onboard 子命令
    doctor.go            # doctor 子命令
    security.go          # security 子命令

internal/
  cli/
    checker.go           # Checker 接口 + Diagnostic 类型 + Registry
    checkers/            # 各检查项实现（每个文件通过 init() 注册）
      environment.go     # Go 版本、OS、PATH 工具
      config.go          # YAML 语法、必填字段、值合法性
      dependencies.go    # Claude CLI、SQLite、网络可达性
      security.go        # JWT 强度、文件权限、敏感信息
      runtime.go         # 端口占用、孤儿进程、磁盘空间
      messaging.go       # Slack/Feishu 凭据格式
    onboard/
      wizard.go          # 交互式向导流程
      templates.go       # 配置模板生成
    output/
      printer.go         # 统一输出格式（✓/⚠/✗ + ANSI 颜色）
      report.go          # JSON/report 输出
```

## 3. 核心类型

### 3.1 Checker 接口

```go
// internal/cli/checker.go

type Status string

const (
    StatusPass Status = "pass"
    StatusWarn Status = "warn"
    StatusFail Status = "fail"
)

type Diagnostic struct {
    Name     string       // 检查项标识，如 "config.syntax"
    Category string       // 分类：environment / config / dependencies / security / runtime / messaging
    Status   Status       // pass / warn / fail
    Message  string       // 人类可读描述
    Detail   string       // 详细信息（--verbose 时显示）
    FixHint  string       // 修复建议文本
    FixFunc  func() error // 自动修复函数（nil = 不可自动修复）
}

type Checker interface {
    Name() string
    Category() string
    Check(ctx context.Context) Diagnostic
}

type CheckerRegistry struct {
    checkers []Checker
}

var DefaultRegistry = &CheckerRegistry{}

func (r *CheckerRegistry) Register(c Checker)
func (r *CheckerRegistry) All() []Checker
func (r *CheckerRegistry) ByCategory(cat string) []Checker
```

### 3.2 Checker 注册

每个 checker 文件通过 `init()` 自注册：

```go
// internal/cli/checkers/config.go

func init() {
    DefaultRegistry.Register(&ConfigSyntaxChecker{})
    DefaultRegistry.Register(&ConfigRequiredFieldsChecker{})
    DefaultRegistry.Register(&ConfigValuesChecker{})
}
```

doctor 命令执行时通过 blank import 触发注册：

```go
// cmd/worker/doctor.go

import _ "github.com/hotplex/hotplex-worker/internal/cli/checkers"
```

## 4. onboard 命令

交互式配置向导，7 步流程：

### 步骤 1: 环境预检
- Go 版本 >= 1.26
- 支持的 OS/ARCH（darwin/linux, amd64/arm64）
- 磁盘空间 >= 100MB
- 失败则提示退出

### 步骤 2: 配置文件生成
- 检测 `configs/config.yaml` 是否存在
- 不存在 → 从模板生成
- `env.example` → `.env`（如不存在）
- 已存在 → 提示覆盖或跳过

### 步骤 3: 必需配置项
- `JWT_SECRET`: 交互输入或自动生成 (`openssl rand -base64 48`)
- `ADMIN_TOKEN`: 交互输入或自动生成
- `WORKER_TYPE`: 选择 `claude_code` / `opencode_server`

### 步骤 4: Worker 依赖检查
- 检测 `claude` / `opencode` 二进制是否在 PATH
- 不在 PATH → 提示安装或跳过（可稍后配置）

### 步骤 5: 消息平台（可选）
- 是否配置 Slack？ → 输入 App Token / Bot Token
- 是否配置 Feishu？ → 输入 App ID / App Secret
- 可跳过，稍后通过配置文件设置

### 步骤 6: 写入配置
- 写入 `.env` 和 `config.yaml`
- 验证写入的配置语法正确

### 步骤 7: 验证
- 自动运行 `doctor`（不修复）
- 报告最终状态

**Flag**:
- `--non-interactive`: 使用默认值或环境变量，不提示输入
- `--force`: 覆盖已有配置文件

## 5. doctor 命令

### 检查项清单

| Category     | Name             | 检查内容                                  | Auto-fix              |
| ------------ | ---------------- | ----------------------------------------- | --------------------- |
| environment  | GoVersion        | Go >= 1.26                                | 否                    |
| environment  | OSArch           | 支持 darwin/linux, amd64/arm64            | 否                    |
| environment  | BuildTools       | golangci-lint、goimports 在 PATH          | 否                    |
| config       | ConfigExists     | config.yaml 存在                          | 是（模板生成）        |
| config       | ConfigSyntax     | YAML 语法正确                             | 否                    |
| config       | ConfigRequired   | 必填字段非空                              | 是（提示输入）        |
| config       | ConfigValues     | 端口范围、路径合法                        | 是（修正默认值）      |
| config       | EnvVars          | 必需环境变量已设置                        | 是（写入 .env）       |
| dependencies | WorkerBinary     | claude/opencode 在 PATH                   | 否                    |
| dependencies | SQLitePath       | DB 文件路径可写                           | 是（创建目录）        |
| dependencies | NetworkReachable | 外部 API 可达（可选，默认跳过）           | 否                    |
| security     | JWTStrength      | 密钥长度 >= 32 字符                       | 是（重新生成）        |
| security     | AdminToken       | token 非默认值                            | 是（重新生成）        |
| security     | FilePermissions  | 配置/数据目录权限                         | 是（chmod）           |
| security     | EnvInGit         | .env 不在 git 跟踪                        | 是（加入 .gitignore） |
| runtime      | DiskSpace        | 可用空间 >= 100MB                         | 否                    |
| runtime      | PortAvailable    | 端口 8888/9999 未被占用                   | 是（提示 kill）       |
| runtime      | OrphanPIDs       | 无孤儿 PID 文件                           | 是（清理）            |
| runtime      | DataDirWritable  | data/ 目录可写                            | 是（创建）            |
| messaging    | SlackCreds       | Slack token 格式合法（xoxb-、xapp- 前缀） | 否                    |
| messaging    | FeishuCreds      | Feishu 凭据格式合法                       | 否                    |

### --fix 修复流程

1. 运行所有检查，收集 Diagnostic 切片
2. 筛选 `FixFunc != nil && Status != Pass` 的项
3. 对用户确认后，按 category 顺序执行修复
4. 修复后重新运行该项检查，验证修复成功
5. 输出修复报告（N 项修复成功 / M 项仍需手动处理）

### 输出格式

**终端输出**（默认）：
```
HotPlex Doctor v0.1.0

✓ environment  Go 1.26+ installed (go1.26.0)
✓ environment  OS supported (darwin/arm64)
⚠ environment  golangci-lint not in PATH
✗ config       config.yaml not found at configs/config.yaml
  → Run: hotplex onboard to generate configuration
✓ dependencies claude binary found at /usr/local/bin/claude
✗ runtime      port 8888 already in use (PID 12345)
  → Stop the process or change gateway.port in config

6 passed, 1 warning, 2 failures
Run 'hotplex doctor --fix' to auto-fix 1 issue(s)
```

**JSON 输出**（`--json`）：
```json
{
  "version": "0.1.0",
  "timestamp": "2026-04-22T14:00:00Z",
  "summary": {"pass": 6, "warn": 1, "fail": 2},
  "diagnostics": [
    {
      "name": "config.exists",
      "category": "config",
      "status": "fail",
      "message": "config.yaml not found",
      "fix_hint": "Run: hotplex onboard"
    }
  ]
}
```

## 6. security 命令

在 doctor 的 security category 基础上扩展：

- **JWT 密钥强度评分**: 长度 + Shannon 熵
- **配置文件敏感信息扫描**: .env 中是否有明文密钥泄露风险
- **.gitignore 完整性**: 确保 .env、data/、logs/ 已忽略
- **TLS 配置检查**: 生产模式下是否启用 TLS
- **Admin API 认证**: admin token 是否已设置
- **SSRF 配置审查**: 安全模块是否正确配置

`--fix` 行为与 doctor 一致：确认后自动修复。

## 7. main.go 重构

### 重构目标

将现有 `cmd/worker/main.go` (~656行) 拆分：

- `main.go`: Cobra root cmd + 子命令注册（~50 行）
- `serve.go`: 原有启动逻辑（~600 行，几乎原封不动）
- `onboard.go` / `doctor.go` / `security.go`: 新增子命令

### serve cmd 的 flag 兼容

```go
func newServeCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "serve",
        Short: "Start the gateway server",
        RunE:  runServe,
    }
    cmd.Flags().StringP("config", "c", "configs/config.yaml", "config file path")
    cmd.Flags().Bool("dev", false, "development mode")
    return cmd
}
```

### 向后兼容

`hotplex` 无子命令时默认执行 `serve`：

```go
rootCmd.RunE = runServe  // 默认行为 = serve
```

保留 `hotplex -config xxx` 和 `hotplex -dev` 的兼容性。

### 依赖变更

新增依赖：
- `github.com/spf13/cobra` — CLI 框架

现有 `spf13/viper` 已在项目中。

## 8. Checker 与现有包的复用

| Checker                                      | 复用的现有包                                               |
| -------------------------------------------- | ---------------------------------------------------------- |
| ConfigSyntax / ConfigRequired / ConfigValues | `internal/config/` — `config.Load()` + `config.Validate()` |
| WorkerBinary                                 | `internal/worker/base/env.go` — PATH 查找逻辑              |
| PortAvailable                                | `net.Listen` 探测                                          |
| OrphanPIDs                                   | `internal/worker/proc/pidfile.go` — `CleanupOrphans`       |
| JWTStrength                                  | `internal/security/jwt.go` — 密钥验证                      |
| SlackCreds                                   | `internal/messaging/slack/` — token 前缀验证               |
| FeishuCreds                                  | `internal/messaging/feishu/` — 凭据验证                    |
| FilePermissions                              | `os.Stat` + `os.Chmod`                                     |

## 9. 退出码

| Code | 含义                   |
| ---- | ---------------------- |
| 0    | 全部通过（无 failure） |
| 1    | 存在 failure           |
| 2    | 参数/flag 错误         |
| 3    | 自动修复失败           |

## 10. 测试策略

- 每个 Checker 独立测试：构造临时目录/文件，验证 pass/warn/fail 各状态
- onboard wizard: mock stdin，验证交互流程
- doctor --fix: 验证修复函数执行后检查项从 fail → pass
- 集成测试：完整 onboard → serve → doctor 流程
- JSON 输出：反序列化验证结构正确

## 11. 不在范围内

以下明确不在首批实现范围：

- 运行时连接检查（连 Admin API 做运行时诊断）
- 交互式 TUI 界面
- 自动更新/升级功能
- 远程诊断报告上传
- 多语言支持（仅英文输出）
- Windows 支持（项目仅支持 POSIX）
