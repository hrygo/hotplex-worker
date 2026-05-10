---
type: design
tags:
  - project/HotPlex
  - configuration/management
  - architecture/config
---

# Configuration Management Architecture

> HotPlex 配置管理架构设计。

---

## 1. 设计原则

### 1.1 12-Factor App 核心原则

| 原则 | HotPlex 实践 |
|------|-------------|
| **配置与代码分离** | 敏感值在 `.env`，不在代码 |
| **环境变量作为唯一来源** | YAML 中的 `${VAR}` 引用环境变量 |
| **严格环境隔离** | dev/prod 环境独立配置文件 |
| **配置存储在环境** | 无 hardcoded 配置 |

### 1.2 配置文件结构

```
configs/
├── config.yaml         # 主配置（完整参考）
├── config-dev.yaml     # 开发环境
├── config-prod.yaml    # 生产环境
└── env.example         # 环境变量模板
```

通过 `inherits` 字段实现配置继承：

```yaml
# config-prod.yaml
inherits: config.yaml
security:
  tls_enabled: true
```

---

## 2. 环境变量注入

### 2.1 自定义 ExpandEnv

HotPlex 使用自定义 `ExpandEnv()` 函数，支持 `${VAR:-default}` shell 风格默认值语法。

> Go 标准库 `os.ExpandEnv` **不支持** `${VAR:-default}`，展开结果为字面量。

```go
// ✅ os.ExpandEnv: 不支持默认值语法
os.ExpandEnv("${HOTPLEX_VAR:-fallback}") // → "${HOTPLEX_VAR:-fallback}" (literal!)

// ✅ HotPlex ExpandEnv: 支持默认值语法
ExpandEnv("${HOTPLEX_VAR:-fallback}")   // → "fallback" 或环境变量值
```

**实现** (`internal/config/config.go`):

```go
// ExpandEnv expands ${VAR} and ${VAR:-default} references in a config value.
func ExpandEnv(s string) string {
    re := regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)
    return re.ReplaceAllStringFunc(s, func(match string) string {
        parts := re.FindStringSubmatch(match)
        key := parts[1]
        defaultVal := parts[2]
        if val := os.Getenv(key); val != "" {
            return val
        }
        return defaultVal
    })
}
```

---

## 3. Secret 管理

### 3.1 SecretsProvider 接口

```go
// internal/config/config.go

type SecretsProvider interface {
    Get(key string) string
}

// EnvSecretsProvider — 从环境变量获取
type EnvSecretsProvider struct{}
func (p *EnvSecretsProvider) Get(key string) string { return os.Getenv(key) }

// ChainedSecretsProvider — 按顺序尝试多个 Provider
type ChainedSecretsProvider struct { providers []SecretsProvider }
func (p *ChainedSecretsProvider) Get(key string) string {
    for _, pr := range p.providers {
        if val := pr.Get(key); val != "" { return val }
    }
    return ""
}
```

### 3.2 Secret 验证

```go
// RequireSecrets validates that all required sensitive fields are present.
func (c *Config) RequireSecrets() error {
    var missing []string
    if len(c.Security.JWTSecret) == 0 {
        missing = append(missing, "security.jwt_secret")
    }
    if len(missing) > 0 {
        return fmt.Errorf("config: missing required secrets: %s", strings.Join(missing, ", "))
    }
    return nil
}
```

> **未来扩展**: 可通过实现 `SecretsProvider` 接口接入 HashiCorp Vault、AWS Secrets Manager 等。当前仅支持环境变量。

---

## 4. 配置验证

### 4.1 Validate 方法

```go
// internal/config/config.go
func (c *Config) Validate() []string {
    var errs []string
    if c.Gateway.Addr == "" {
        errs = append(errs, "gateway.addr is required")
    }
    if c.DB.Path == "" {
        errs = append(errs, "db.path is required")
    }
    if !c.Security.TLSEnabled &&
        !strings.Contains(c.Gateway.Addr, "localhost") &&
        !strings.Contains(c.Gateway.Addr, "127.0.0.1") {
        errs = append(errs, "TLS is disabled on non-local address; enable tls_enabled for production")
    }
    return errs
}
```

### 4.2 CLI 验证命令

```bash
# 验证默认配置
hotplex config validate

# 验证指定配置文件
hotplex config validate -c /path/to/config.yaml

# 严格模式（同时检查 secrets）
hotplex config validate --strict
```

---

## 5. 配置继承

Config 结构体包含 `Inherits` 字段，支持加载父配置后用当前配置覆盖：

```go
type Config struct {
    Inherits string `mapstructure:"inherits"` // path to parent config file
    // ...
}
```

**加载顺序**:
1. 加载父配置文件（递归，含循环检测）
2. 用当前文件覆盖差异字段
3. 展开 `${VAR}` 环境变量
4. 调用 `Validate()` 验证

---

## 6. 配置热更新

### 6.1 Watcher 实现

`Watcher` 基于 `fsnotify` 监控配置文件变更，支持防抖和原子更新：

```go
// internal/config/watcher.go

type Watcher struct {
    log      *slog.Logger
    path     string
    sp       SecretsProvider
    viper    *fsnotify.Watcher
    debounce time.Duration
    onChange func(*Config)   // 热更新回调
    onStatic func(string)    // 静态字段变更通知
    store    *ConfigStore    // 原子配置持有者
}
```

### 6.2 热更新字段

**可热更新**（运行时生效，无需重启）:

| 字段 | 说明 |
|------|------|
| `log.level` | 日志级别 |
| `session.gc_scan_interval` | GC 扫描间隔 |
| `pool.max_size` | 全局会话池大小 |
| `pool.max_idle_per_user` | 每用户空闲会话数 |
| `security.api_keys` | API 密钥 |
| `security.allowed_origins` | CORS 允许的来源 |
| `worker.max_lifetime` | Worker 最大生命周期 |
| `worker.idle_timeout` | Worker 空闲超时 |
| `worker.execution_timeout` | Worker 执行超时 |
| `worker.auto_retry` | LLM 自动重试配置 |
| `admin.requests_per_sec` | Admin API 速率限制 |
| `admin.burst` | Admin API 突发限制 |
| `admin.tokens` | Admin API 令牌 |

**静态字段**（变更需重启）:

| 字段 | 说明 |
|------|------|
| `gateway.addr` | 网关监听地址 |
| `gateway.*_queue_size` | 广播队列大小 |
| `gateway.*_buffer_size` | 读写缓冲区大小 |
| `log.format` | 日志格式 (json/text) |
| `security.tls_*` | TLS 配置 |
| `security.jwt_secret` | JWT 密钥 |
| `db.path` | 数据库路径 |
| `db.wal_mode` | WAL 模式 |

### 6.3 变更审计

`ConfigChange` 记录每次配置变更：

```go
type ConfigChange struct {
    Timestamp time.Time
    Field     string
    OldValue  string
    NewValue  string
    Hot       bool // true = 已热更新生效，false = 需重启
}
```

---

## 7. 配置加载流程

```
                    ┌─────────────┐
                    │  CLI flags  │  Priority 1 (最高)
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │ Env vars    │  Priority 2
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │ Config file │  Priority 3 (含 inherits 继承)
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │  Default()  │  Priority 4 (最低，代码默认值)
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │ ExpandEnv() │  展开 ${VAR} / ${VAR:-default}
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    │  Validate() │  字段验证 + RequireSecrets()
                    └─────────────┘
```

---

## 8. 未来计划

| 阶段 | 任务 | 状态 |
|------|------|------|
| **Phase 1** | 强化 Validate() — 字段必填性、格式、业务规则 | ✅ 已实现 |
| **Phase 2** | Vault Secret Provider — 接入 HashiCorp Vault / AWS Secrets Manager | 📋 计划中 |
| **Phase 3** | 配置变更持久化审计 — 数据库记录 + 回滚 | 📋 计划中 |
| **Phase 4** | CUE Schema 增强验证（可选） | 📋 计划中 |

---

## 9. 不推荐的决策

| 方案 | 原因 |
|------|------|
| Feature Flags | HotPlex 是基础设施层，Feature Flags 应在调用方实现 |
| K8s Operator | 轻量审计 + YAML 版本控制足够 |
| CUE 作为必选 | Go 原生验证优先，CUE 作为可选增强 |

---

## 10. 参考资料

- [12-Factor App: Config](https://12factor.net/config)
- [HashiCorp Vault](https://developer.hashicorp.com/vault)
- [CUE Configuration Language](https://cuelang.org/)
