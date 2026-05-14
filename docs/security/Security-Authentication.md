---

---

# Security: Authentication & Authorization Design

> HotPlex v1.0 WebSocket 认证授权设计，基于行业最佳实践。

---

## 1. 设计原则

### 1.1 行业最佳实践

| 来源 | 推荐方案 |
|------|----------|
| RFC 7519 (JWT) | 使用 `alg: ES256`，禁止 `HS256` |
| RFC 8725 (JOSE) | 始终验证 `aud` (Audience) claim |
| OAuth 2.0 | 短期 Access Token + Refresh Token |
| Discord Gateway | Session Resume 机制 |

### 1.2 核心决策

- ✅ **ES256 签名**：ECDSA P-256，性能优于 RSA，公钥可分发
- ✅ **jti 防重放**：每 Token 唯一 ID，支持 Redis 黑名单
- ✅ **aud 验证**：必须验证 JWT 接收方是 HotPlex Gateway
- ✅ **分层 TTL**：短 Access Token + 长 Session Token

---

## 2. JWT 签名与验证

### 2.1 签名算法选择

**推荐**：ES256 (ECDSA P-256 SHA-256)

```go
// 签名
privateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
tokenString, _ := token.SignedString(privateKey)

// 验证
publicKey := &privateKey.PublicKey
token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
    if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
        return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
    }
    return publicKey, nil
})
```

**优势对比**：

| 算法 | 签名大小 | 验证性能 | 公钥分发 | 推荐度 |
|------|----------|----------|----------|--------|
| ES256 | 64B | 快 | ✅ | ⭐⭐⭐⭐⭐ |
| RS256 | 256B | 中 | ✅ | ⭐⭐⭐⭐ |
| HS256 | 32B | 快 | ❌ | ⭐⭐（仅内部服务） |

### 2.2 JWT Claims 结构

```json
{
  "iss": "hotplex-auth-service",
  "sub": "user_abc123",
  "aud": "hotplex-gateway",
  "exp": 1710000000,
  "iat": 1709999700,
  "jti": "550e8400-e29b-41d4-a716-446655440000",
  "role": "user",
  "scope": "session:create session:read session:delete",
  "bot_id": "B0123456789",
  "session_id": "sess_a1b2c3d4"
}
```

| Claim | 类型 | 必需 | 说明 |
|-------|------|------|------|
| `iss` | string | ✅ | 签发者：hotplex-auth-service |
| `sub` | string | ✅ | 用户主体 ID |
| `aud` | string/[]string | ✅ | **必须验证**：hotplex-gateway |
| `exp` | int64 | ✅ | 过期时间（Unix timestamp） |
| `iat` | int64 | ✅ | 签发时间 |
| `jti` | string | ✅ | **JWT ID**：防重放攻击 |
| `role` | string | ✅ | RBAC 角色 |
| `scope` | string | ⚠️ | OAuth2 风格权限范围 |
| `bot_id` | string | ⚠️ | Bot 隔离标识 |
| `session_id` | string | ⚠️ | Session 绑定（可选） |

### 2.3 Token 类型分层

| Token 类型 | TTL | 用途 | 存储 |
|-----------|-----|------|------|
| **Access Token** | 5 min | API 认证 | 内存/Redis |
| **Gateway Token** | 1 hour | WebSocket 连接保活 | Client 内存 |
| **Refresh Token** | 7 days | 刷新 Access Token | HttpOnly Cookie |

---

## 3. WebSocket 认证流程

### 3.1 双保险认证机制

```
┌─────────────────────────────────────────────────────────────┐
│                  WebSocket Authentication                     │
│                                                              │
│  1. 握手阶段 (Handshake)                                     │
│     Client ──── Cookie (可选) ────► Gateway                  │
│                    │                                         │
│                    ▼                                         │
│     Cookie 无效 ──► 401 Unauthorized                          │
│                    │                                         │
│                    ▼                                         │
│     Cookie 有效 ──► 允许 Upgrade (101 Switching Protocols)    │
│                                                              │
│  2. 首条消息认证 (First Message)                              │
│     Client ──── JWT (Authorization header) ──► Gateway       │
│                    │                                         │
│                    ▼                                         │
│     JWT 验证失败 ──► error + WS Close (1008)                 │
│                    │                                         │
│                    ▼                                         │
│     JWT 验证成功 ──► 绑定 user_id + session_id              │
│                                                              │
│  3. 消息循环 (Message Loop)                                  │
│     Client ──── Envelope ──► Gateway                         │
│                    │                                         │
│                    ▼                                         │
│     session_id 一致性验证 ──► 处理事件                       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 握手阶段认证

**方案 A：Cookie 认证（浏览器环境）**

```http
GET /gateway HTTP/1.1
Host: hotplex.example.com
Upgrade: websocket
Connection: Upgrade
Cookie: hotplex_token=<gateway_token>
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

**验证逻辑**：

```go
func (g *Gateway) ValidateHandshake(r *http.Request) error {
    // 1. 检查 TLS
    if !g.config.TLS.Required && r.TLS == nil {
        return ErrTLSRequired
    }

    // 2. 验证 Cookie 中的 Gateway Token
    cookie, err := r.Cookie("hotplex_token")
    if err != nil {
        return ErrMissingAuthCookie
    }

    claims, err := g.validateGatewayToken(cookie.Value)
    if err != nil {
        return err
    }

    // 3. 存储用户信息到请求上下文
    r = r.WithContext(withUserClaims(r.Context(), claims))

    return nil
}
```

**方案 B：Authorization Header（CLI/Desktop）**

```http
GET /gateway HTTP/1.1
Host: hotplex.example.com
Upgrade: websocket
Connection: Upgrade
Authorization: Bearer <jwt_token>
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

### 3.3 首条消息认证（init.envelope）

**JWT Token 嵌入 Envelope**：

```json
{
  "version": "aep/v1",
  "kind": "init",
  "data": {
    "protocol_version": "aep/v1",
    "client_caps": ["streaming", "tools"],
    "auth": {
      "token": "<jwt_token>"
    }
  }
}
```

**验证流程**：

```go
func (g *Gateway) HandleInit(env *Envelope) (*Envelope, error) {
    // 1. 提取 JWT Token
    tokenStr := env.Data["auth"].(map[string]interface{})["token"].(string)

    // 2. 解析并验证 JWT
    claims, err := g.jwtValidator.Validate(tokenStr)
    if err != nil {
        return nil, &AuthError{Code: "AUTHENTICATION_FAILED", Reason: err.Error()}
    }

    // 3. 验证 jti 不在黑名单（防重放）
    if g.redis.Exists("jwt:blacklist:" + claims.JTI) {
        return nil, &AuthError{Code: "TOKEN_REVOKED", Reason: "jti already used"}
    }

    // 4. 验证 aud
    if !slices.Contains(claims.Audience, "hotplex-gateway") {
        return nil, &AuthError{Code: "INVALID_AUDIENCE", Reason: "wrong audience"}
    }

    // 5. 绑定用户到 session
    session := sm.CreateSession(claims.Sub, claims.BotID)

    return &Envelope{
        Kind: "init_ack",
        Data: map[string]interface{}{
            "session_id": session.ID,
            "server_caps": g.getServerCaps(),
        },
    }, nil
}
```

---

## 4. Session Ownership 验证

### 4.1 JWT 绑定 Session

```go
type Session struct {
    ID        string
    OwnerID   string    // JWT sub claim
    BotID     string    // JWT bot_id claim
    State     SessionState
    CreatedAt int64
}
```

### 4.2 Ownership 验证流程

```go
func (sm *SessionManager) ValidateOwnership(sessionID, userID string) error {
    session, err := sm.GetSession(sessionID)
    if err != nil {
        return ErrSessionNotFound
    }

    if session.OwnerID != userID {
        // 记录安全日志
        log.Warn("session ownership mismatch",
            "session_id", sessionID,
            "expected_owner", session.OwnerID,
            "actual_owner", userID,
        )
        return ErrSessionOwnershipMismatch
    }

    return nil
}
```

### 4.3 Admin API 权限矩阵

| 端点 | Required Scope | 说明 |
|------|----------------|------|
| `GET /admin/sessions` | `admin:read` | 列出所有 session |
| `DELETE /admin/sessions/{id}` | `admin:delete` | 强制终止 |
| `GET /admin/stats` | `admin:read` | 统计信息 |
| `GET /admin/metrics` | `admin:read` | Prometheus metrics |

---

## 5. Token 生命周期管理

### 5.1 jti 生成算法

> ⚠️ **必须使用 `crypto/rand` 生成 jti**，禁止使用 `math/rand` 或时间戳。

```go
// internal/security/jwt.go

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
)

// GenerateJTI 生成符合 RFC 7519 的 JWT ID
// 使用 crypto/rand 确保密码学安全，格式兼容 UUID v4
func GenerateJTI() (string, error) {
    b := make([]byte, 16)
    n, err := rand.Read(b)
    if err != nil {
        return "", fmt.Errorf("crypto/rand unavailable: %w", err)
    }
    if n != 16 {
        return "", fmt.Errorf("crypto/rand read insufficient bytes: got %d, want 16", n)
    }

    // UUID v4 格式：xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
    // 设置版本号（4）和变体（8/9/a/b）
    b[6] = (b[6] & 0x0f) | 0x40
    b[8] = (b[8] & 0x3f) | 0x80

    return fmt.Sprintf("%s-%s-%s-%s-%s",
        hex.EncodeToString(b[0:4]),
        hex.EncodeToString(b[4:6]),
        hex.EncodeToString(b[6:8]),
        hex.EncodeToString(b[8:10]),
        hex.EncodeToString(b[10:16]),
    ), nil
}
```

### 5.2 ES256 密钥管理

```go
// internal/security/key_manager.go

type KeyManager interface {
    GetPrivateKey() (*ecdsa.PrivateKey, error)
    GetPublicKey() *ecdsa.PublicKey
    PublicKeyPEM() ([]byte, error)
    Rotate() error
}

// FileKeyManager 从文件加载密钥，支持密钥轮换
type FileKeyManager struct {
    privateKeyPath string
    publicKeyPath  string
    privateKey     *ecdsa.PrivateKey  // 缓存
    publicKey      *ecdsa.PublicKey   // 缓存
    mu             sync.RWMutex
}

func NewFileKeyManager(privatePath, publicPath string) (*FileKeyManager, error) {
    km := &FileKeyManager{
        privateKeyPath: privatePath,
        publicKeyPath:  publicPath,
    }
    // 预加载密钥
    if err := km.loadKeys(); err != nil {
        return nil, err
    }
    return km, nil
}

func (km *FileKeyManager) loadKeys() error {
    km.mu.Lock()
    defer km.mu.Unlock()

    privPEM, err := os.ReadFile(km.privateKeyPath)
    if err != nil {
        return fmt.Errorf("read private key: %w", err)
    }

    privBlock, _ := pem.Decode(privPEM)
    if privBlock == nil {
        return errors.New("invalid PEM in private key file")
    }

    priv, err := x509.ParseECPrivateKey(privBlock.Bytes)
    if err != nil {
        return fmt.Errorf("parse EC private key: %w", err)
    }

    km.privateKey = priv
    km.publicKey = &priv.PublicKey
    return nil
}

func (km *FileKeyManager) GetPrivateKey() (*ecdsa.PrivateKey, error) {
    km.mu.RLock()
    defer km.mu.RUnlock()
    if km.privateKey == nil {
        return nil, errors.New("private key not loaded")
    }
    return km.privateKey, nil
}

func (km *FileKeyManager) GetPublicKey() *ecdsa.PublicKey {
    km.mu.RLock()
    defer km.mu.RUnlock()
    return km.publicKey
}

func (km *FileKeyManager) PublicKeyPEM() ([]byte, error) {
    km.mu.RLock()
    defer km.mu.RUnlock()

    pubDER, err := x509.MarshalPKIXPublicKey(km.publicKey)
    if err != nil {
        return nil, fmt.Errorf("marshal public key: %w", err)
    }

    return pem.EncodeToMemory(&pem.Block{
        Type:  "PUBLIC KEY",
        Bytes: pubDER,
    }), nil
}

// Rotate 重新加载密钥（用于密钥轮换时热更新）
func (km *FileKeyManager) Rotate() error {
    return km.loadKeys()
}
```

**密钥文件格式**（PEM + PKCS8 / SEC1）：

```bash
# 生成 ES256 私钥（P-256 曲线，256-bit）
openssl ecparam -name prime256v1 -genkey -noout -out private_key.pem
openssl ec -in private_key.pem -outform PEM -out private_key.pem

# 导出公钥
openssl ec -in private_key.pem -pubout -out public_key.pem

# 验证格式
openssl ec -in private_key.pem -text -noout
# EC Private-Key 曲率为 prime256v1 (NID_X9_62_prime256v1)
```

### 5.3 Redis 黑名单 TTL

> ⚠️ **jti TTL = access_token_ttl × 2**，允许时钟偏移（客户端与服务端时钟差 ≤ TTL）。

```go
const (
    AccessTokenTTL = 5 * time.Minute
    JTIBlacklistTTL = AccessTokenTTL * 2  // 10 分钟，允许 ±5 分钟时钟偏移
)
```

### 5.4 Token 刷新机制

```go
// Access Token 刷新
func (s *AuthService) RefreshToken(refreshToken string) (*TokenPair, error) {
    // 1. 验证 Refresh Token
    claims, err := s.validateRefreshToken(refreshToken)
    if err != nil {
        return nil, err
    }

    // 2. 验证 jti 不在黑名单（Rotation 机制）
    if s.redis.Exists("refresh:blacklist:" + claims.JTI) {
        return nil, ErrTokenReused  // 单次使用
    }

    // 3. 将旧 jti 加入黑名单
    s.redis.Set("refresh:blacklist:"+claims.JTI, "1", 7*24*time.Hour)

    // 4. 签发新 Token Pair
    return s.issueTokenPair(claims.Sub, claims.BotID)
}
```

### 5.5 吊销机制

**Redis Blacklist**：

```go
// JWT 吊销
func (s *AuthService) RevokeToken(jti string, ttl time.Duration) error {
    return s.redis.Set("jwt:blacklist:"+jti, "revoked", ttl)
}

// 登出时吊销所有 Token
func (s *AuthService) Logout(userID string) error {
    // 删除该用户的所有活跃 Token
    return s.redis.Del("user:tokens:" + userID)
}
```

---

## 6. 多 Bot 隔离方案

### 6.1 决策：共享密钥 + bot_id 隔离

> ✅ **采用共享 ES256 密钥 + JWT `bot_id` claim 隔离**，简化密钥分发运维。

```go
// JWT 中包含 bot_id，Gateway 按 bot_id 路由到对应 Worker Pool
type Session struct {
    ID        string
    OwnerID   string  // JWT sub claim
    BotID     string  // JWT bot_id claim（用于 Worker Pool 隔离）
}
```

**为何不用独立密钥**：
- 每个 Bot 需要独立密钥对，公钥分发复杂
- 共享密钥 + `bot_id` 在内部服务间足够安全（外部攻击者无 bot_id）

**何时需要独立密钥**：
- 多租户场景（每个租户自行管理密钥）
- 参见 v2.0-design 多实例分布式架构

---

## 7. 需要确认的决策点

### 6.1 关键问题

| # | 问题 | 选项 | 推荐 |
|---|------|------|------|
| 1 | Gateway Token TTL | 1小时 / 5分钟 | **1小时**（稳定性优先，WebSocket 长连接） |
| 2 | Refresh Token 存储 | HttpOnly Cookie / 客户端存储 | **HttpOnly Cookie**（浏览器）|
| 3 | 多 Bot 隔离 | 独立密钥 / 共享密钥 + bot_id | **共享密钥 + bot_id**（简化管理） |
| 4 | Session Resume | Discord 风格 / 无状态 | **有状态**（用户期望） |

### 6.2 待确认的 TTL 配置

```yaml
# 方案 A：Gateway Token = 1小时（稳定优先）
auth:
  gateway_token_ttl: 1h
  access_token_ttl: 5m
  refresh_token_ttl: 168h  # 7 days

# 方案 B：Gateway Token = 5分钟（安全优先，需定期刷新）
auth:
  gateway_token_ttl: 5m
  access_token_ttl: 5m
  refresh_token_ttl: 168h
```

---

## 7. 安全检查清单

- 使用 ES256 签名（禁止 HS256）
- 验证 JWT `aud` claim
- 实现 jti 防重放（Redis 黑名单）
- WebSocket 握手阶段 Cookie/Header 认证
- init.envelope 中 JWT 验证
- Session Ownership 绑定
- Token 刷新 + Rotation
- TLS 强制（生产环境）
- 安全日志（认证失败、Ownership 不匹配）

---

## 8. 参考资料

- [RFC 7519: JSON Web Token (JWT)](https://datatracker.ietf.org/doc/html/rfc7519)
- [RFC 8725: JSON Web Algorithms (JWE)](https://datatracker.ietf.org/doc/html/rfc8725)
- [RFC 6750: OAuth 2.0 Bearer Token Usage](https://datatracker.ietf.org/doc/html/rfc6750)
- [Discord Gateway Authentication](https://discord.com/developers/docs/topics/gateway#authorizing)
- [Auth0: JSON Web Token Best Practices](https://auth0.com/blog/ json-web-token-best-practices/)