---

---

# SSRF Protection

> HotPlex v1.0 服务端请求伪造（Server-Side Request Forgery）防护设计。HotPlex 通过 AEP 协议代理 WebFetch 请求（Claude Code 的 WebFetch tool），可被滥用为 SSRF 跳板攻击内网服务或云元数据端点。

---

## 1. 威胁模型

### 1.1 攻击向量

```
攻击者 ──→ HotPlex Gateway ──→ 内部服务
              │
              ├── AWS EC2 元数据 (169.254.169.254)
              ├── Kubernetes API (10.0.0.1)
              ├── 数据库服务 (10.x.x.x)
              └── 本地服务 (127.0.0.1:8888)
```

### 1.2 危险端点

| 端点 | IP/域名 | 风险 | 凭证 |
|------|---------|------|------|
| AWS EC2 Metadata | `169.254.169.254` | 🔴 获取 IAM 角色凭证 | EC2 Role |
| GCP Metadata | `metadata.google.internal` | 🔴 获取服务账号令牌 | GCP SA |
| Azure Metadata | `169.254.169.254` | 🔴 获取 Managed Identity 令牌 | Azure MI |
| Kubernetes API | `10.0.0.1` | 🔴 集群控制面 | K8s RBAC |
| Docker API | `127.0.0.1:2375` | 🟡 容器逃逸/控制 | 无 |
| 内网数据库 | `10.x.x.x:5432` | 🟡 数据泄露 | DB 凭证 |

### 1.3 攻击场景

| 场景 | 描述 | 影响 |
|------|------|------|
| **云元数据窃取** | `curl http://169.254.169.254/latest/meta-data/` | 获取 EC2 IAM 凭证 |
| **内网端口扫描** | 对 10.0.0.0/8 范围端口扫描 | 探测内网服务 |
| **本地端口探测** | `curl http://127.0.0.1:8888/admin` | 访问本地管理接口 |
| **云存储枚举** | `curl https://s3.amazonaws.com/...` | S3 桶枚举（ACL 依赖） |
| **Redis/DB 未授权访问** | 内网 Redis 无认证 | 数据读写 |

---

## 2. 防护策略

### 2.1 分层防护

```
┌─────────────────────────────────────────────────────────┐
│  Layer 1: 协议层（最优先）                                │
│  ✅ 仅允许 http/https，禁止 file://, gopher:// 等          │
├─────────────────────────────────────────────────────────┤
│  Layer 2: IP 层                                          │
│  ✅ 阻断私有 IP范围 (10.x, 172.16.x, 192.168.x)           │
│  ✅ 阻断链路本地 (169.254.x, 224.x.x.x)                    │
│  ✅ 阻断环回地址 (127.x.x.x, ::1, 0.0.0.0)                 │
├─────────────────────────────────────────────────────────┤
│  Layer 3: DNS 重绑定防护（防绕过）                        │
│  ✅ DNS 解析后再次验证 IP（解析结果可能被污染）              │
├─────────────────────────────────────────────────────────┤
│  Layer 4: Host 头部验证                                   │
│  ✅ URL host 与最终解析 IP 一致性检查                      │
└─────────────────────────────────────────────────────────┘
```

### 2.2 IP 范围阻断表

```go
// internal/security/ssrf.go

import (
    "net"
    "net/url"
)

// BlockedCIDRs 禁止访问的 IP 范围
var BlockedCIDRs = []*net.IPNet{
    // 私有地址
    mustParseCIDR("10.0.0.0/8"),       // 10.0.0.0 - 10.255.255.255
    mustParseCIDR("172.16.0.0/12"),    // 172.16.0.0 - 172.31.255.255
    mustParseCIDR("192.168.0.0/16"),   // 192.168.0.0 - 192.168.255.255

    // 环回地址
    mustParseCIDR("127.0.0.0/8"),      // 127.0.0.0 - 127.255.255.255
    mustParseCIDR("::1/128"),          // IPv6 loopback
    mustParseCIDR("fc00::/7"),         // IPv6 unique local
    mustParseCIDR("fe80::/10"),        // IPv6 link-local

    // 链路本地（云元数据）
    mustParseCIDR("169.254.0.0/16"),  // 169.254.0.0 - 169.254.255.255 (AWS/GCP/Azure)

    // 广播地址
    mustParseCIDR("255.255.255.255/32"),

    // 保留地址
    mustParseCIDR("0.0.0.0/8"),        // 0.0.0.0 (Linux: 当前主机)
}

func mustParseCIDR(cidr string) *net.IPNet {
    _, n, err := net.ParseCIDR(cidr)
    if err != nil {
        panic("invalid CIDR: " + cidr)
    }
    return n
}

func isIPBlocked(ip net.IP) bool {
    for _, blocked := range BlockedCIDRs {
        if blocked.Contains(ip) {
            return true
        }
    }
    return false
}
```

---

## 3. URL 验证实现

### 3.1 核心验证函数

```go
// internal/security/ssrf.go

type SSRFProtectionError struct {
    URL     string
    Reason  string
    Blocked string  // 被阻断的具体 IP/CIDR
}

func (e *SSRFProtectionError) Error() string {
    return fmt.Sprintf("SSRF blocked: %s (reason=%s, blocked_by=%s)", e.URL, e.Reason, e.Blocked)
}

// ValidateURL 验证 URL 是否安全（SSRF 防护）
// 验证顺序：协议 → 主机解析 → DNS 重绑定 → IP 阻断
func ValidateURL(targetURL string) error {
    // 1. 解析 URL
    u, err := url.Parse(targetURL)
    if err != nil {
        return &SSRFProtectionError{URL: targetURL, Reason: "invalid URL"}
    }

    // 2. 协议检查：仅允许 http/https
    switch u.Scheme {
    case "http", "https":
        // 允许
    case "":
        return &SSRFProtectionError{URL: targetURL, Reason: "missing scheme"}
    default:
        return &SSRFProtectionError{URL: targetURL, Reason: "disallowed scheme: " + u.Scheme}
    }

    // 3. 主机名检查：拒绝裸 IP（防止 CIDR 绕过）
    if ip := net.ParseIP(u.Hostname()); ip != nil {
        if isIPBlocked(ip) {
            return &SSRFProtectionError{
                URL:     targetURL,
                Reason:  "bare IP in URL is blocked",
                Blocked: ip.String(),
            }
        }
        return nil  // 直接 IP，无 DNS 重绑定风险
    }

    // 4. DNS 解析 + 重绑定防护
    // 必须解析后再检查 IP（防止 DNS 指向内网）
    ips, err := net.LookupIP(u.Hostname())
    if err != nil {
        return &SSRFProtectionError{URL: targetURL, Reason: "DNS lookup failed: " + err.Error()}
    }

    // 5. 检查所有解析结果
    for _, ip := range ips {
        if isIPBlocked(ip) {
            return &SSRFProtectionError{
                URL:     targetURL,
                Reason:  "DNS resolved to blocked IP",
                Blocked: ip.String(),
            }
        }
    }

    // 6. 额外检查：如果是域名，验证不指向内网后再解析
    // （可选）缓存合法域名减少 LookupIP 调用

    return nil
}
```

### 3.2 DNS 重绑定防护（高级）

> ⚠️ **DNS 重绑定可绕过静态 IP 检查**：攻击者注册域名 `evil.com` 解析到公网 IP，但短时间内切换到内网 IP。

```go
// ValidateURLWithRebindProtection DNS 重绑定防护
func ValidateURLWithRebindProtection(targetURL string, expectedTTL time.Duration) error {
    // 基础验证
    if err := ValidateURL(targetURL); err != nil {
        return err
    }

    // 额外检查：DNS 解析后的 TTL 是否过短（疑似 DNS 重绑定）
    // 注意：Go 标准库不提供 TTL 信息，需使用自定义 DNS 客户端
    // 如需完全防护，集成 `miekg/dns` 进行 AXFR 查询

    return nil
}

// 简化方案：强制二次解析（用于高敏感场景）
func ValidateURLDoubleResolve(targetURL string) error {
    // 第一次解析
    if err := ValidateURL(targetURL); err != nil {
        return err
    }

    // 短暂延迟后再次解析（增加 DNS 缓存失效概率）
    time.Sleep(100 * time.Millisecond)

    u, _ := url.Parse(targetURL)
    ips, _ := net.LookupIP(u.Hostname())

    for _, ip := range ips {
        if isIPBlocked(ip) {
            return &SSRFProtectionError{
                URL:     targetURL,
                Reason:  "DNS rebind detected: IP changed to blocked range",
                Blocked: ip.String(),
            }
        }
    }

    return nil
}
```

### 3.3 主机名校验（Open Redirect 防护）

```go
// ValidateRedirectURL 验证跳转目标（防止 Open Redirect + SSRF 组合攻击）
func ValidateRedirectURL(redirectURL string) error {
    // 允许相对路径（安全）
    if !strings.HasPrefix(redirectURL, "http://") && !strings.HasPrefix(redirectURL, "https://") {
        return nil
    }
    // 绝对路径需要 SSRF 检查
    return ValidateURL(redirectURL)
}
```

---

## 4. 与 AEP WebFetch 集成

### 4.1 拦截点

```go
// internal/engine/aep_handler.go

// HandleWebFetch AEP WebFetch 事件处理
func (h *AEPHandler) HandleWebFetch(event *Envelope) (*Envelope, error) {
    url := event.Data["url"].(string)

    // SSRF 防护验证
    if err := ssrf.ValidateURL(url); err != nil {
        // 记录安全日志
        log.Warn("SSRF attempt blocked",
            "url", url,
            "user_id", h.session.OwnerID,
            "session_id", h.session.ID,
            "reason", err.Error(),
        )

        return &Envelope{
            Kind: "error",
            Data: map[string]interface{}{
                "code":    "SSRF_BLOCKED",
                "message": "URL blocked by security policy",
                "url":     url,
            },
        }, nil
    }

    // 执行请求
    return h.doWebFetch(url)
}
```

### 4.2 安全日志

```go
// SSRF 阻断日志字段
log.Warn("SSRF blocked",
    "session_id", sessionID,
    "user_id", userID,
    "url", blockedURL,
    "resolved_ip", resolvedIP,
    "blocked_cidr", cidrBlock,
    "timestamp", time.Now().Unix(),
)
```

---

## 5. 配置

```yaml
# configs/security.yaml
ssrf:
  # 允许的协议
  allowed_schemes:
    - http
    - https

  # 阻断的 IP 范围（可追加）
  blocked_ip_ranges:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
    - 127.0.0.0/8
    - 169.254.0.0/16
    - ::1/128
    - fc00::/7
    - fe80::/10

  # DNS 重绑定防护（高敏感场景）
  dns_rebind_protection:
    enabled: false  # 启用会增性能开销
    double_resolve: false
    min_ttl_seconds: 60

  # 域名白名单（完全信任的域名，跳过 SSRF 检查）
  # ⚠️ 谨慎使用，确保这些域名不会被攻击者控制
  allowlist:
    - "api.github.com"
    - "*.anthropic.com"
```

---

## 6. 安全检查清单

- 仅允许 http/https 协议
- 阻断私有 IP 段（10.x, 172.16.x, 192.168.x）
- 阻断链路本地地址（169.254.x）
- 阻断环回地址（127.x.x.x, ::1）
- 拒绝裸 IP URL（绕过 DNS 过滤）
- DNS 解析后再次验证 IP（防重绑定）
- SSRF 阻断日志（含 session_id, user_id）
- 域名白名单最小化（防止白名单域名被攻击者控制）
- 性能测试（DNS 解析不显著增加延迟）

---

## 7. 参考资料

- [OWASP SSRF](https://owasp.org/www-project-web-security-testing-guide/latest/4-Web_Application_Security_Testing/07-Input_Validation_Testing/10-Testing_for_Server-Side_Request_Forgery)
- [CWE-918: Server-Side Request Forgery](https://cwe.mitre.org/data/definitions/918.html)
- [PortSwigger: SSRF](https://portswigger.net/web-security/ssrf)
- [AWS: Instance Metadata Service](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html)
- [GCP: Metadata Server](https://cloud.google.com/compute/docs/metadata/overview)
- [DNS Rebinding Attack](https://en.wikipedia.org/wiki/DNS_rebinding)
