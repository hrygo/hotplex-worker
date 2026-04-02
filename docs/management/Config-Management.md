---
type: design
tags:
  - project/HotPlex
  - configuration/management
  - architecture/config
---

# Configuration Management Architecture

> HotPlex v1.0 配置管理架构设计，基于行业最佳实践。

---

## 1. 设计原则

### 1.1 12-Factor App 核心原则

| 原则 | HotPlex 实践 |
|------|-------------|
| **配置与代码分离** | 敏感值在 `.env`，不在代码 |
| **环境变量作为唯一来源** | YAML 中的 `${VAR}` 引用环境变量 |
| **严格环境隔离** | dev/staging/prod 环境独立配置 |
| **配置存储在环境** | 无 hardcoded 配置 |

### 1.2 HotPlex 配置模型

```
configs/
├── _defaults/
│   └── defaults.yaml         # 全局默认值
├── environments/
│   ├── dev.yaml              # 开发环境
│   ├── staging.yaml          # 预发布环境
│   └── prod.yaml             # 生产环境
└── config.yaml               # 主配置入口
```

---

## 2. 12-Factor 配置实现

### 2.1 环境变量注入

> ⚠️ **HotPlex 使用自定义 `ExpandEnv()` 函数，不使用 Go 标准库 `os.ExpandEnv`**。
>
> 标准库 `os.ExpandEnv` **不支持** `${VAR:-default}` 语法，展开结果为字面量。
> HotPlex 自定义实现支持 shell 风格的默认值语法，这是有意识的设计选择。
>
> ```go
> // ✅ os.ExpandEnv: 不支持默认值语法
> os.ExpandEnv("${HOTPLEX_VAR:-fallback}") // → "${HOTPLEX_VAR:-fallback}" (literal!)
>
> // ✅ HotPlex 自定义 ExpandEnv: 支持默认值语法
> ExpandEnv("${HOTPLEX_VAR:-fallback}")   // → "fallback" 或环境变量值
> ```

**YAML 中的 `${VAR}` 语法**：

```yaml
# configs/gateway.yaml
gateway:
  server_addr: "${HOTPLEX_SERVER_ADDR:-:8888}"
  tls:
    cert_file: "${HOTPLEX_TLS_CERT}"
    key_file: "${HOTPLEX_TLS_KEY}"

database:
  path: "${HOTPLEX_DB_PATH}"

worker:
  output_limit:
    max_total_bytes: 20971520  # 20MB
```

**环境变量读取**：

```go
// internal/config/env.go

// ExpandEnv 递归展开 ${VAR} 和 ${VAR:-default} 语法
func ExpandEnv(input string) string {
    // 展开 ${VAR} 形式
    re := regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

    return re.ReplaceAllStringFunc(input, func(match string) string {
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

### 2.2 配置验证

```go
// internal/config/validation.go

type Validator struct {
    schema *jsonschema.Schema
}

func (v *Validator) Validate(config *Config) error {
    // 1. 必填字段检查
    if config.Gateway.ServerAddr == "" {
        return errors.New("gateway.server_addr is required")
    }

    // 2. 类型检查
    if config.Gateway.MaxConnections < 1 {
        return errors.New("gateway.max_connections must be >= 1")
    }

    // 3. 业务规则检查
    if config.Gateway.TLS.Enabled && !config.Gateway.TLS.Required {
        log.Warn("TLS enabled but not required")
    }

    // 4. 依赖检查
    if config.Auth.JWT.Enabled {
        if config.Auth.JWT.PublicKeyFile == "" {
            return errors.New("auth.jwt.public_key_file is required when JWT is enabled")
        }
    }

    return nil
}
```

---

## 3. Secret 管理

### 3.1 分层 Secret 管理

| 阶段 | Secret 来源 | 说明 |
|------|-------------|------|
| **开发** | `.env` 文件 | 本地明文 |
| **测试** | CI 环境变量 | GitHub Secrets |
| **预发布** | Vault / 云 KMS | 运行时注入 |
| **生产** | HashiCorp Vault / AWS Secrets Manager | 动态 Secret |

### 3.2 Secret Provider 接口

```go
// internal/config/secrets.go

type SecretsProvider interface {
    Get(key string) (string, error)
}

// EnvProvider 从环境变量获取
type EnvProvider struct{}

func (p *EnvProvider) Get(key string) (string, error) {
    val := os.Getenv(key)
    if val == "" {
        return "", fmt.Errorf("secret %q not found", key)
    }
    return val, nil
}

// VaultProvider 从 HashiCorp Vault 获取
type VaultProvider struct {
    addr   string
    path   string
    client *api.Client
}

func (p *VaultProvider) Get(key string) (string, error) {
    secret, err := p.client.KVv2(p.path).Get(context.Background(), key)
    if err != nil {
        return "", err
    }
    return secret.Data["value"].(string), nil
}

// ChainedProvider 尝试多个 Provider
type ChainedProvider struct {
    providers []SecretsProvider
}

func (p *ChainedProvider) Get(key string) (string, error) {
    for _, provider := range p.providers {
        if val, err := provider.Get(key); err == nil {
            return val, nil
        }
    }
    return "", fmt.Errorf("secret %q not found in any provider", key)
}
```

### 3.3 Secret 注入

```go
// 在启动时注入 Secret
func (c *Config) InjectSecrets(provider SecretsProvider) error {
    // 读取需要 Secret 的字段
    secretFields := []string{
        "HOTPLEX_JWT_PUBLIC_KEY",
        "HOTPLEX_JWT_PRIVATE_KEY",
        "HOTPLEX_TLS_CERT",
        "HOTPLEX_TLS_KEY",
        "HOTPLEX_ANTHROPIC_API_KEY",
    }

    for _, field := range secretFields {
        val, err := provider.Get(field)
        if err != nil {
            return fmt.Errorf("failed to inject secret %s: %w", field, err)
        }
        os.Setenv(field, val)
    }

    return nil
}
```

---

## 4. 配置分层

### 4.1 配置继承

```yaml
# config.yaml
inherits: ./environments/prod.yaml

# 仅覆盖差异部分
logging:
  level: debug  # 生产用 info，这里覆盖为 debug
```

```go
// internal/config/loader.go

type Loader struct {
    basePath    string
    environment string
}

func (l *Loader) Load() (*Config, error) {
    config := &Config{}

    // 1. 加载默认配置
    if err := l.loadFile("_defaults/defaults.yaml", config); err != nil {
        return nil, err
    }

    // 2. 加载环境配置（覆盖）
    envConfig := &Config{}
    if err := l.loadFile(l.environment+".yaml", envConfig); err != nil {
        return nil, err
    }
    config.Merge(envConfig)

    // 3. 处理继承
    if config.Inherits != "" {
        parentConfig := &Config{}
        if err := l.loadFile(config.Inherits, parentConfig); err != nil {
            return nil, err
        }
        // 循环继承检测
        if l.isCyclic(config, parentConfig) {
            return nil, errors.New("cyclic config inheritance detected")
        }
        config.Merge(parentConfig)
    }

    // 4. 展开环境变量
    config.ExpandEnvVars()

    // 5. 验证配置
    if err := config.Validate(); err != nil {
        return nil, err
    }

    return config, nil
}
```

### 4.2 开发/生产配置示例

```yaml
# environments/dev.yaml
gateway:
  server_addr: ":8888"
  tls:
    enabled: false  # 开发环境关闭 TLS
  logging:
    level: debug

worker:
  global:
    max_concurrent: 5
    output_limit:
      max_total_bytes: 5242880  # 5MB

auth:
  jwt:
    enabled: false  # 开发环境关闭认证
```

```yaml
# environments/prod.yaml
gateway:
  server_addr: ":8888"
  tls:
    enabled: true
    cert_file: "${HOTPLEX_TLS_CERT}"
    key_file: "${HOTPLEX_TLS_KEY}"
    require_tls: true
  logging:
    level: info

worker:
  global:
    max_concurrent: 20
    output_limit:
      max_total_bytes: 20971520  # 20MB

auth:
  jwt:
    enabled: true
    algorithm: ES256
    public_key_file: "${HOTPLEX_JWT_PUBLIC_KEY}"
    issuer: "hotplex-auth-service"
    audience: "hotplex-gateway"
    expiry_seconds: 3600
```

---

## 5. 配置热更新

### 5.1 fsnotify 监控

```go
// internal/config/watcher.go

type Watcher struct {
    configPath string
    config     atomic.Value  // *Config
    watcher    *fsnotify.Watcher
    debounce   time.Duration
    onUpdate   func(*Config)
}

func NewWatcher(path string, debounce time.Duration) (*Watcher, error) {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return nil, err
    }

    w := &Watcher{
        configPath: path,
        debounce:   debounce,
        config:     atomic.Value{},
    }

    dir := filepath.Dir(path)
    watcher.Add(dir)

    return w, nil
}

func (w *Watcher) Start(ctx context.Context, loader *Loader) error {
    var debounceTimer *time.Timer

    for {
        select {
        case <-ctx.Done():
            return nil

        case event := <-w.watcher.Events:
            if event.Op&fsnotify.Write == fsnotify.Write {
                // Debounce
                if debounceTimer != nil {
                    debounceTimer.Stop()
                }
                debounceTimer = time.AfterFunc(w.debounce, func() {
                    w.reload(loader)
                })
            }

        case err := <-w.watcher.Errors:
            log.Error("watcher error", "error", err)
        }
    }
}

func (w *Watcher) reload(loader *Loader) {
    newConfig, err := loader.Load()
    if err != nil {
        log.Error("failed to reload config", "error", err)
        return
    }

    // 原子更新
    w.config.Store(newConfig)

    // 回调
    if w.onUpdate != nil {
        w.onUpdate(newConfig)
    }

    log.Info("config reloaded successfully")
}
```

### 5.2 动态配置项

```go
// 允许热更新的配置项
var HotReloadableFields = map[string]bool{
    "gateway.logging.level":      true,
    "gateway.rate_limit":        true,
    "worker.global.max_concurrent": true,
}

// 静态配置项（需要重启）
var StaticFields = map[string]bool{
    "gateway.server_addr":       false,
    "auth.jwt":                  false,
    "database.path":             false,
}
```

---

## 6. 配置审计

### 6.1 配置变更日志

```go
// internal/config/audit.go

type ConfigAudit struct {
    db *sql.DB
}

func (a *ConfigAudit) Log(oldCfg, newCfg *Config, userID string) error {
    changes := a.diff(oldCfg, newCfg)

    for _, change := range changes {
        _, err := a.db.Exec(`
            INSERT INTO config_audit_log (change_id, timestamp, user_id, field, old_value, new_value)
            VALUES (?, ?, ?, ?, ?, ?)
        `, uuid.New().String(), time.Now().Unix(), userID,
            change.Field, change.OldValue, change.NewValue)

        if err != nil {
            return err
        }
    }

    return nil
}

type ConfigChange struct {
    Field    string
    OldValue string
    NewValue string
}
```

### 6.2 配置回滚

```go
// Admin API: 配置回滚
func (a *AdminAPI) RollbackConfig(versionID string) error {
    // 1. 获取历史配置
    history, err := a.audit.GetConfigHistory(versionID)
    if err != nil {
        return err
    }

    // 2. 验证配置
    if err := history.Config.Validate(); err != nil {
        return fmt.Errorf("invalid config: %w", err)
    }

    // 3. 应用配置
    os.WriteFile(configPath, history.Config.YAML, 0644)

    // 4. 触发热更新
    configWatcher.Reload()

    return nil
}
```

---

## 7. 配置验证工具

### 7.1 CLI 命令

```bash
# 验证配置
hotplexd config validate /etc/hotplex/config.yaml

# 输出示例
Valid: true
Warnings:
  - "tls.require_tls is false in production"
Errors: []

# 解释配置
hotplexd config explain /etc/hotplex/config.yaml

# 导出默认值
hotplexd config scaffold --output ./configs
```

### 7.2 配置文档生成

```bash
# 生成配置文档
hotplexd config doc --output ./docs/configuration.md
```

---

## 8. 实施路线图

| 阶段 | 任务 | 产出 |
|------|------|------|
| **Phase 1** | 强化验证函数 | 字段必填性、格式、业务规则 |
| **Phase 2** | Secret 管理 | FileProvider → Vault 集成 |
| **Phase 3** | 配置审计 | 变更历史 + 回滚 |
| **Phase 4** | CUE Schema | 可选配置验证增强 |

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
- [JSON Schema Validation](https://json-schema.org/)
- [CUE Configuration Language](https://cuelang.org/)