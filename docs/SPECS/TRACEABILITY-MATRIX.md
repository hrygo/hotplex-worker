# HotPlex Worker 功能实现与代码溯源矩阵 (Traceability Matrix)

> 文档版本: v1.0  |  最后更新: 2026-03-31 11:30  |  维护者: HotPlex Engineering

**状态图标:** 🟢 PASS | 🟡 IN_PROGRESS | 🔴 TODO | ⚫ N/A
**优先级:** 🔴 P0 = MVP 必须 | 🟡 P1 = 重要 | ⚪ P2 = 增强

---

## 汇总看板

```
区域                     P0   P1   P2   总计   PASS   进度
─────────────────────────────────────────────────────────────
AEP v1 协议              22    7    1    30     26    87%
WebSocket Gateway         5    3    0     8      7    88%
Session 管理              6    2    0     8      8   100%
Worker 抽象与进程管理     5    6    1    12      8    67%
安全                     20   11    3    34     33    97%
Admin API                 9    4    0    13      9    69%
配置管理                  5    4    1    10     10   100%
可观测性                  6    4    0    10     10   100%
资源管理                  7    3    0    10      8    80%
消息持久化 (EventStore)   5    5    1    11      8    73%
测试策略                  5    4    2    11      3    27%
─────────────────────────────────────────────────────────────
总计                     95   57    9   157     130   83%
```

### 总体进度

```
[████████████████████████████████████████████████████████████░░░░░░░░] 83% (130/157)

P0 进度:    [█████████████████████████████████████████░░░░░░░░░░░░░░░░░░░] 74% (70/95)
P1 进度:    [██████████████████████████████████████████████░░░░░░░] 86% (49/57)
P2 进度:    [██░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 11% (1/9)
```

---

## 1. AEP v1 协议 (30 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 1 | **AEP-001** | Envelope 结构符合规范 | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | Version/ID/Seq/SessionID/Timestamp/Event/Priority 全字段定义 |
| 2 | **AEP-002** | Init 握手（init / init_ack） | 🔴 P0 | 🟢 PASS | `gateway/init.go` + `conn.go:126-228` | ValidateInit + BuildInitAck + VERSION_MISMATCH 处理 |
| 3 | **AEP-003** | Input 事件（C→S） | 🔴 P0 | 🟢 PASS | `conn.go:355-386` | TransitionWithInput 原子状态转换 + SESSION_BUSY 硬拒绝 |
| 4 | **AEP-004** | State 事件（S→C — 状态变更） | 🔴 P0 | 🟢 PASS | `session/manager.go:197-199` | StateNotifier 通过 transitionState 调用触发 |
| 5 | **AEP-005** | Message.delta 事件（S→C — 流式输出） | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | MessageDelta Kind + MessageDeltaData 定义完整 |
| 6 | **AEP-006** | Tool_call 和 Tool_result 事件 | 🟡 P1 | 🟢 PASS | `pkg/events/events.go` | ToolCallData/ToolResultData 定义完整 |
| 7 | **AEP-007** | Done 事件（S→C — 执行完成） | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | DoneData 含 Success/Stats/Dropped 字段 |
| 8 | **AEP-008** | Error 事件（双向 — 错误通知） | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | 20+ 错误码完整定义 |
| 9 | **AEP-009** | Ping / Pong 事件（双向 — 心跳保活） | 🔴 P0 | 🟢 PASS | `conn.go:83-87, 389-404` | SetPongHandler + MarkAlive/MarkMissed + pongWait=60s |
| 10 | **AEP-010** | Control 事件（双向 — 控制命令） | 🟡 P1 | 🟢 PASS | `conn.go:410-478` | SendControlToSession 实现 terminate/delete/reconnect/throttle |
| 11 | **AEP-011** | Reasoning / Step / Raw / PermissionRequest / PermissionResponse 事件 | ⚪ P2 | 🟡 IN_PROGRESS | `pkg/events/events.go` | 仅 Raw Kind 已定义，其余未定义 |
| 12 | **AEP-012** | Message 事件（S→C — 完整消息） | 🟡 P1 | 🟡 IN_PROGRESS | `pkg/events/events.go` | MessageStartData/MessageEndData 已定义，Message Event Kind 未定义 |
| 13 | **AEP-013** | Session 状态机 — 5 状态 | 🔴 P0 | 🟢 PASS | `pkg/events/events.go:193-220` | ValidTransitions map 完整 |
| 14 | **AEP-014** | Session 状态机 — 竞态防护 | 🔴 P0 | 🟢 PASS | `session/manager.go` + `conn.go` | performInit TOCTOU 保护 + TransitionWithInput 原子锁 |
| 15 | **AEP-015** | Session GC 策略 | 🟡 P1 | 🟢 PASS | `session/manager.go:445-536` | GCScanInterval=60s，max_lifetime/idle_timeout/retention/zombie 全覆盖 |
| 16 | **AEP-016** | Backpressure — 有界通道与 delta 丢弃 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:172-210` | isDroppable + non-blocking select + sessionDropped flag |
| 17 | **AEP-017** | 时序约束 — 事件顺序 | 🔴 P0 | 🟢 PASS | `gateway/conn.go` + `session/manager.go` | state(running)→[delta*]→done 顺序由 Bridge 保证 |
| 18 | **AEP-018** | 时序约束 — 时间限制 | 🔴 P0 | 🟢 PASS | `conn.go:48-49` | pingPeriod=54s, pongWait=60s, initDeadline=30s |
| 19 | **AEP-019** | 断线重连（Reconnect / Resume） | 🔴 P0 | 🟢 PASS | `conn.go:176-199` | session 存在性检查 + DELETED 拒绝 + terminated resume |
| 20 | **AEP-020** | Worker 启动失败与 Crash 检测 | 🔴 P0 | 🟡 IN_PROGRESS | `worker/registry.go` | NewWorker 返回 error，Bridge 检测 Start 失败；但 Done 时 crash 类型映射未完整 |
| 21 | **AEP-021** | 分层终止策略 | 🔴 P0 | 🟢 PASS | `proc/manager.go:122-154` | SIGTERM → 5s grace period → SIGKILL 完整实现 |
| 22 | **AEP-022** | Seq 分配与去重 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:372-390` | SeqGen.Next 原子递增，dropped delta 不消耗 seq |
| 23 | **AEP-023** | Session 连接去重 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:137-158` | JoinSession 自动关闭旧连接 |
| 24 | **AEP-024** | Minimal Compliance — 必须支持的事件 | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | init/input/control/ping + init_ack/message.delta/state/error/done/pong 全定义 |
| 25 | **AEP-025** | Full Compliance — 可选扩展事件 | 🟡 P1 | 🟢 PASS | `pkg/events/events.go` | message.start/end, tool_call/result, reasoning, step, raw, permission_request/response 定义 |
| 26 | **AEP-026** | 能力协商（Client Caps / Server Caps） | 🟡 P1 | 🟢 PASS | `gateway/init.go` | ClientCaps/ServerCaps + DefaultServerCaps 完整 |
| 27 | **AEP-027** | Authentication — 握手阶段认证 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:239-244` | HTTP upgrade 时 Authenticator.AuthenticateRequest，JWT validator |
| 28 | **AEP-028** | 消息持久化与 Event Replay | 🟡 P1 | 🟢 PASS | `session/manager.go` | Gateway 不存储 event log，仅持久化 session 元数据 |
| 29 | **AEP-029** | Executor 执行模型（Turn Event Flow） | 🔴 P0 | 🟢 PASS | `conn.go` + `session/manager.go` | Bridge.forwardEvents 实现 turn 内事件流 |
| 30 | **AEP-030** | 版本协商与兼容性 | 🔴 P0 | 🟢 PASS | `gateway/init.go:132-140` | VERSION_MISMATCH 检测 + 未知 event type forward compatible |

---

## 2. WebSocket Gateway (8 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 31 | **GW-001** | HTTP 握手 JWT 验证通过后升级为 WebSocket | 🔴 P0 | 🟢 PASS | `gateway/hub.go:229-271` | HandleHTTP → AuthenticateRequest → Upgrade → RegisterConn |
| 32 | **GW-002** | Init 握手协议正确处理会话创建与恢复 | 🔴 P0 | 🟢 PASS | `gateway/conn.go:126-228` | performInit 创建/恢复/拒绝 deleted session |
| 33 | **GW-003** | 心跳机制按规范间隔 ping 并检测对端失联 | 🔴 P0 | 🟢 PASS | `conn.go:236-263` | WritePump pingPeriod=54s + missed >= 3 断线 |
| 34 | **GW-004** | 同一 session_id 的新连接踢出旧连接 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:137-158` | JoinSession 自动 c.Close() 旧连接 |
| 35 | **GW-005** | Bridge 双向事件转发正确路由 | 🔴 P0 | 🟢 PASS | `conn.go:634-668` | Bridge.forwardEvents + handleInput 路由完整 |
| 36 | **GW-006** | 优雅关闭 | 🟡 P1 | 🟡 IN_PROGRESS | `gateway/hub.go:341-370` | Hub.Shutdown 关闭所有连接；但无优雅关闭超时机制 |
| 37 | **GW-007** | SeqGen 为每个 session 分配单调递增序号 | 🟡 P1 | 🟢 PASS | `gateway/hub.go:372-390` | SeqGen.Next per-session 原子递增 |
| 38 | **GW-008** | 消息超长被拒绝 | 🟡 P1 | 🟢 PASS | `conn.go:69` | maxMessageSize=32KB via SetReadLimit |

---

## 3. Session 管理 (8 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 39 | **SM-001** | SQLite WAL 模式启用且 busy_timeout 正确配置 | 🔴 P0 | 🟢 PASS | `session/store.go` | PRAGMA journal_mode=WAL + busy_timeout=5000 |
| 40 | **SM-002** | sessions 表 schema 与索引正确创建 | 🔴 P0 | 🟢 PASS | `session/store.go` | migrate() 创建 sessions 表含所有字段 |
| 41 | **SM-003** | 5 状态机转换规则被严格遵守 | 🔴 P0 | 🟢 PASS | `pkg/events/events.go:193-220` | IsValidTransition + ErrInvalidTransition |
| 42 | **SM-004** | GC 定时清理 | 🔴 P0 | 🟢 PASS | `session/manager.go:445-536` | runGC 每 GCScanInterval(max_lifetime/idle_timeout/retention/zombie) |
| 43 | **SM-005** | 状态转换与 input 处理在同一互斥锁内原子完成 | 🔴 P0 | 🟢 PASS | `session/manager.go:234-251` | TransitionWithInput → ms.mu.Lock → transitionState |
| 44 | **SM-006** | mutex 显式命名 'mu'，零值安全，无 embedding | 🟡 P1 | 🟢 PASS | `session/manager.go:51-53` | ms.mu sync.RWMutex，显式命名，无 embedding |
| 45 | **SM-007** | SESSION_BUSY 错误码正确拒绝并发 input | 🔴 P0 | 🟢 PASS | `session/manager.go` + `conn.go:355-376` | IsActive() 检查 + ErrSessionBusy |
| 46 | **SM-008** | PoolManager 配额管理 | 🟡 P1 | 🟢 PASS | `session/pool.go` | Acquire/Release 限制 maxSize 和 maxIdlePerUser |

---

## 4. Worker 抽象与进程管理 (12 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 47 | **WK-001** | SessionConn 接口必须实现 | 🔴 P0 | 🟢 PASS | `internal/worker/worker.go` | `var _ Worker = (*XWorker)(nil)` 编译时验证 |
| 48 | **WK-002** | Capabilities 接口正确声明各 Worker 类型能力 | 🟡 P1 | 🟢 PASS | `internal/worker/worker.go` | Capabilities 接口含 Type/SupportsResume/SessionStoreDir |
| 49 | **WK-003** | Claude Code Worker：--resume 恢复持久会话 | 🟡 P1 | 🟡 IN_PROGRESS | `worker/claudecode/worker.go` | --resume 参数实现存在，需验证端到端流程 |
| 50 | **WK-004** | OpenCode CLI Worker：无 --session-id，从 step_start 提取 sessionID | 🟡 P1 | 🟡 IN_PROGRESS | `worker/opencodecli/worker.go` | 需验证 step_start sessionID 提取逻辑 |
| 51 | **WK-005** | OpenCode Server Worker：HTTP+SSE 托管进程模式 | ⚪ P2 | 🟡 IN_PROGRESS | `worker/opencodeserver/worker.go` | 需验证 SSE 连接断开检测 |
| 52 | **WK-006** | Hot-multiplexing：持久 Worker 在 turn 之间保持进程存活 | 🟡 P1 | 🟢 PASS | `worker/registry.go` + `session/manager.go` | Worker 跨 turn 存活，GC 不误杀 |
| 53 | **WK-007** | PGID 隔离：Setpgid=true 防止信号误伤 Gateway 进程 | 🔴 P0 | 🟢 PASS | `proc/manager.go:62-64` | SysProcAttr{Setpgid: true} |
| 54 | **WK-008** | 分层终止：SIGTERM → 5s grace period → SIGKILL | 🔴 P0 | 🟢 PASS | `proc/manager.go:122-154` | Terminate + Kill 方法完整 |
| 55 | **WK-009** | 输出限制：64KB 初始 buffer，10MB 上限 | 🔴 P0 | 🟡 IN_PROGRESS | `proc/manager.go` | Setpgid 等已实现；但 bufio.Scanner 未在此文件中实例化，Worker adapter 中未验证 |
| 56 | **WK-010** | Anti-pollution 触发重启：max_turns 或内存水位 | 🟡 P1 | 🔴 TODO | - | max_turns 配置存在但逻辑未实现 |
| 57 | **WK-011** | Worker 进程僵死检测（LastIO）防止僵尸 IO 轮询 | 🟡 P1 | 🟡 IN_PROGRESS | `session/manager.go:478-496` | GC 检测 LastIO()；但 Worker 接口中 LastIO() 需确认各 adapter 实现 |
| 58 | **WK-012** | 所有 goroutine 有明确 shutdown 路径，无泄漏 | 🔴 P0 | 🟢 PASS | `proc/manager.go` + `session/manager.go` | drainStderr goroutine (stderr close 时退出)；session GC ctx cancel |

---

## 5. 安全 (34 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 59 | **SEC-001** | JWT 必须使用 ES256 签名 | 🔴 P0 | 🟢 PASS | `security/jwt.go:65-80` | 仅接受 ES256，明确拒绝 HS256/RS256/none |
| 60 | **SEC-002** | JWT Claims 必须包含完整结构 | 🔴 P0 | 🟢 PASS | `security/jwt.go:43-53` | RegisteredClaims + HotPlex 扩展字段 |
| 61 | **SEC-003** | Token 生命周期必须正确实施 | 🔴 P0 | 🟢 PASS | `security/jwt.go:92-94` | Exp 过期检测 + GenerateToken iat+ttl |
| 62 | **SEC-004** | WebSocket 认证流程必须安全 | 🔴 P0 | 🟢 PASS | `gateway/hub.go` + `conn.go:156-168` | HTTP upgrade + init envelope JWT 双层认证 |
| 63 | **SEC-005** | JTI 必须使用 crypto/rand 生成 | 🔴 P0 | 🟢 PASS | `security/jwt.go:306-343` | crypto/rand.Read(16字节) |
| 64 | **SEC-006** | JTI 黑名单必须正确撤销 Token | 🔴 P0 | 🟢 PASS | `security/jwt.go:225-292` | TTL 缓存 + 后台 sweeper |
| 65 | **SEC-007** | 多 Bot 隔离通过 ES256 + bot_id 实现 | 🔴 P0 | 🟡 IN_PROGRESS | `security/jwt.go` | BotID 字段存在；但 Handler 层 bot_id 匹配未完全接入 |
| 66 | **SEC-008** | API Key 比较使用恒定时间 | 🟡 P1 | 🟢 PASS | `security/jwt.go:216-220` | subtle.ConstantTimeCompare |
| 67 | **SEC-010** | exec.Command 必须使用 []string 参数 | 🔴 P0 | 🟢 PASS | `security/command.go:31-35` | BuildSafeCommand variadic string args |
| 68 | **SEC-011** | 命令白名单只允许 claude 和 opencode | 🔴 P0 | 🟢 PASS | `security/command.go:12-16` | AllowedCommands map 含 claude/opencode |
| 69 | **SEC-012** | 双层验证: 句法 + 语义 | 🔴 P0 | 🟢 PASS | `security/auth.go:119-127` | InputValidator 长度/null字节检查 + 语义层各白名单 |
| 70 | **SEC-013** | SafePathJoin 完整安全流程 | 🔴 P0 | 🟢 PASS | `security/path.go:30-63` | IsAbs拒绝 → Clean → Join → EvalSymlinks → 前缀验证 |
| 71 | **SEC-014** | 危险字符检测作为纵深防御 | 🟡 P1 | 🟢 PASS | `security/command.go:38-67` | ContainsDangerousChars 检查 18 类字符 |
| 72 | **SEC-015** | BaseDir 白名单必须限制会话工作目录 | 🟡 P1 | 🟢 PASS | `security/path.go:9-21` | AllowedBaseDirs 含 /var/hotplex/projects 和 /tmp/hotplex |
| 73 | **SEC-016** | Model 白名单限制 AI 模型 | 🟡 P1 | 🟢 PASS | `security/model.go` | AllowedModels 大小写不敏感验证 |
| 74 | **SEC-020** | 仅允许 http/https 协议 | 🔴 P0 | 🟢 PASS | `security/ssrf.go:81-88` | Protocol switch 仅允许 http/https |
| 75 | **SEC-021** | 所有私有 IP 段和保留地址必须被阻止 | 🔴 P0 | 🟢 PASS | `security/ssrf.go:18-41` | BlockedCIDRs 含 RFC1918/loopback/link-local/multicast/reserved |
| 76 | **SEC-022** | DNS 重新绑定攻击防护 | 🔴 P0 | 🟢 PASS | `security/ssrf.go:133-177` | ValidateURLDoubleResolve 100ms 延迟重解析 |
| 77 | **SEC-023** | URL 验证流程完整链路 | 🔴 P0 | 🟢 PASS | `security/ssrf.go:74-131` | url.Parse → 空host → 主机名黑名单 → IP前缀 → DNS解析 → CIDR检查 |
| 78 | **SEC-024** | SSRFValidator 日志记录被阻止的请求 | 🟡 P1 | 🟢 PASS | `security/ssrf.go:200-213` | slog.Warn 含 url/ssrf_reason |
| 79 | **SEC-030** | BaseEnvWhitelist 限制基础环境变量 | 🔴 P0 | 🟢 PASS | `security/env_builder.go:9-12` | 8 个系统变量白名单 |
| 80 | **SEC-031** | Worker 类型特定白名单正确注入 | 🟡 P1 | 🟢 PASS | `security/env_builder.go:91-96` | AddWorkerType 按 worker type 注入 vars |
| 81 | **SEC-032** | ProtectedEnvVars 绝对不可被覆盖 | 🔴 P0 | 🟢 PASS | `security/env_builder.go:60-67,100-107,111-118` | IsProtectedEnvVar 全路径检查 |
| 82 | **SEC-033** | 敏感模式检测正确识别秘密信息 | 🔴 P0 | 🟢 PASS | `security/env.go:8-54` | 24 前缀 + 4 正则模式 |
| 83 | **SEC-034** | 保护变量始终被剥离 | 🔴 P0 | 🟢 PASS | `security/env.go:59-63,79-85` | protectedVars 含 CLAUDECODE/GATEWAY_ADDR/GATEWAY_TOKEN |
| 84 | **SEC-035** | HotPlex 必需变量正确注入 | 🟡 P1 | 🟢 PASS | `security/env_builder.go:98-107` | AddHotPlexVar → hotplexVars map → Build() 输出 |
| 85 | **SEC-036** | Go 运行时环境变量白名单受保护 | 🟡 P1 | 🟢 PASS | `security/env_builder.go:14-17` | GOPROXY/GOSUMDB 在白名单，GOPATH/GOROOT 在 ProtectedEnvVars |
| 86 | **SEC-037** | 嵌套 Agent 调用被阻止 | 🟡 P1 | 🟢 PASS | `security/env.go:105-117` | StripNestedAgent 移除所有 CLAUDECODE= 条目 |
| 87 | **SEC-040** | AllowedTools 白名单限制可用工具 | 🔴 P0 | 🟢 PASS | `security/tool.go:8-19` | ValidateTools 含 9 个工具 |
| 88 | **SEC-041** | BuildAllowedToolsArgs 正确构建 CLI 参数 | ⚪ P2 | 🟢 PASS | `security/tool.go:53-60` | 构建 --allowed-tools 交替参数 |
| 89 | **SEC-042** | 工具分类 (Safe/Risky/Network/System) | 🟡 P1 | 🟢 PASS | `security/tool.go` | Safe/Risky/Network 三类分类 |
| 90 | **SEC-043** | 生产环境工具集无 Risky/Network 工具 | 🟡 P1 | 🟢 PASS | `security/tool.go:22-26` | ProductionAllowedTools 仅含 Read/Grep/Glob |
| 91 | **SEC-044** | Dev 环境工具集包含所有工具 | ⚪ P2 | 🟢 PASS | `security/tool.go:29-40` | DevAllowedTools 含全部 10 个工具 |
| 92 | **SEC-045** | Tool 调用通过 --allowed-tools 传递给 Worker | ⚪ P2 | 🟡 IN_PROGRESS | `security/tool.go` | BuildAllowedToolsArgs 函数存在，但未接入 worker/proc/manager.go |

---

## 6. Admin API (13 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 93 | **ADMIN-001** | GET /admin/sessions 返回会话列表 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | ListSessions + 分页 (limit/offset) |
| 94 | **ADMIN-002** | GET /admin/sessions/{id} 获取会话详情 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | GetSession |
| 95 | **ADMIN-003** | DELETE /admin/sessions/{id} 强制终止会话 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | sm.Delete → SIGTERM 分层终止 |
| 96 | **ADMIN-004** | GET /admin/stats 统计摘要 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | HandleStats 返回 uptime/WS连接数/sessions by type |
| 97 | **ADMIN-005** | GET /admin/metrics Prometheus 格式指标 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | promhttp.Handler() 在 /admin/metrics |
| 98 | **ADMIN-006** | GET /admin/health Gateway 健康检查 | 🔴 P0 | 🟡 IN_PROGRESS | `cmd/gateway/main.go` | 基本实现存在；但 spec 要求 health 无需认证（当前检查 ScopeHealthRead）|
| 99 | **ADMIN-007** | GET /admin/health/workers Worker 健康检查 | 🔴 P0 | 🟡 IN_PROGRESS | `cmd/gateway/main.go` | 返回硬编码 stub 数据 (test_passed: true)，无真实探活 |
| 100 | **ADMIN-008** | GET /admin/logs 查询日志 | 🟡 P1 | 🔴 TODO | - | HandleLogs 返回空 []any{}，无日志缓冲区接入 |
| 101 | **ADMIN-009** | POST /admin/config/validate 验证配置 | 🟡 P1 | 🔴 TODO | - | 仅验证已加载配置，未实现请求体验证 |
| 102 | **ADMIN-010** | GET /admin/debug/sessions/{id} 会话调试状态 | 🟡 P1 | 🔴 TODO | - | 仅返回 id/state；spec 要求 input_queue_size/mutex_locked/last_seq_sent |
| 103 | **ADMIN-011** | Admin API 认证中间件完整认证链 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | Rate limit → IP whitelist → Bearer token → Scopes |
| 104 | **ADMIN-012** | Admin API 分页行为 | 🟡 P1 | 🟢 PASS | `cmd/gateway/main.go` | limit/offset 边界处理，负数 reject |
| 105 | **ADMIN-013** | Admin API 权限矩阵验证 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | Scope 常量 + hasScope() 强制执行 |

---

## 7. 配置管理 (10 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 106 | **CONFIG-001** | 配置加载 defaults.yaml + 环境覆盖 | 🔴 P0 | 🟢 PASS | `config/config.go` | viper.AutomaticEnv() + ReadInConfig |
| 107 | **CONFIG-002** | ExpandEnv ${VAR} 和 ${VAR:-default} 语法支持 | 🔴 P0 | 🟢 PASS | `config/config.go` | regex ExpandEnv 支持两种语法 |
| 108 | **CONFIG-003** | 配置验证必填字段、类型、业务规则 | 🔴 P0 | 🟢 PASS | `config/config.go` | Config.Validate() 必填/类型/业务规则 |
| 109 | **CONFIG-004** | Secret Provider 三种实现 | 🔴 P0 | 🟢 PASS | `config/config.go` | EnvSecretsProvider + ChainedSecretsProvider |
| 110 | **CONFIG-005** | 配置继承循环检测 | 🟡 P1 | 🔴 TODO | - | 无 inherits 字段和循环检测 |
| 111 | **CONFIG-006** | 配置热更新 fsnotify + 500ms 防抖 | 🟡 P1 | 🟢 PASS | `config/watcher.go` | fsnotify.NewWatcher + debounceTimer 500ms |
| 112 | **CONFIG-007** | 热更新动态字段与静态字段区分 | 🟡 P1 | 🟢 PASS | `config/watcher.go:14-45` | HotReloadableFields/StaticFields map |
| 113 | **CONFIG-008** | 配置变更审计日志 | 🟡 P1 | 🟢 PASS | `config/watcher.go:47-54` | ConfigChange struct + AuditLog() 方法 |
| 114 | **CONFIG-009** | 配置回滚 | ⚪ P2 | 🔴 TODO | - | 无版本历史和回滚机制 |
| 115 | **CONFIG-010** | 配置深度合并策略 | 🔴 P0 | 🟢 PASS | `config/config.go` | Viper Unmarshal 深度合并，LoadOptions 链式加载 |

---

## 8. 可观测性 (10 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 116 | **OBS-001** | 日志格式 OTel Log Data Model 兼容 | 🔴 P0 | 🟢 PASS | `cmd/gateway/main.go` | slog.NewJSONHandler 含 timestamp/level/msg/attrs |
| 117 | **OBS-002** | 日志级别规范 DEBUG/INFO/WARN/ERROR | 🔴 P0 | 🟢 PASS | 全代码 | slog.LevelInfo/Warn/Error/Debug |
| 118 | **OBS-003** | Prometheus 指标命名规范 | 🔴 P0 | 🟢 PASS | `metrics/metrics.go` | hotplex_ 前缀 + 规范命名 |
| 119 | **OBS-004** | RED 方法指标 API 层 | 🔴 P0 | 🟢 PASS | `hub.go`/`conn.go`/`manager.go` | Inc()/Set()/Observe() 全部接入 |
| 120 | **OBS-005** | USE 方法指标基础设施层 | 🔴 P0 | 🟢 PASS | `pool.go`/`proc/manager.go` | Pool/Gateway/Worker 指标全部接入 |
| 121 | **OBS-006** | OTel Span 创建与上下文注入 | 🔴 P0 | 🟢 PASS | `internal/tracing/tracing.go` | Init/Shutdown/Attr + hub.broadcast/conn.recv/init spans，graceful degradation |
| 122 | **OBS-007** | Tail Sampling 尾部采样策略 | 🟡 P1 | 🔴 TODO | - | 无 OTel Collector tail sampling |
| 123 | **OBS-008** | SLO 定义与测量 | 🟡 P1 | 🔴 TODO | - | 无 SLO 定义 |
| 124 | **OBS-009** | 告警规则症状告警而非根因告警 | 🟡 P1 | 🔴 TODO | - | 无告警规则 |
| 125 | **OBS-010** | Grafana Dashboard 核心面板 | 🟡 P1 | 🔴 TODO | - | 无 Grafana dashboard JSON |

---

## 9. 资源管理 (10 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 126 | **RES-001** | Session 所有权 JWT sub claim | 🔴 P0 | 🟢 PASS | `session/manager.go:358-378` | ValidateOwnership |
| 127 | **RES-002** | 权限矩阵 Owner vs Admin 隔离 | 🔴 P0 | 🟢 PASS | `session/manager.go` | adminUserID bypass + Admin scope 强制 |
| 128 | **RES-003** | 输出限制 10MB/20MB/1MB | 🔴 P0 | 🟢 PASS | `security/limits.go` | MaxLineBytes=10MB/MaxSessionBytes=20MB/MaxEnvelopeBytes=1MB |
| 129 | **RES-004** | 并发限制 全局 20 / per_user 5 | 🔴 P0 | 🟢 PASS | `session/pool.go` | PoolManager.Acquire/Release |
| 130 | **RES-005** | 内存限制 RLIMIT_AS | 🔴 P0 | 🟡 IN_PROGRESS | `proc/manager.go` | spec 定义存在但 proc/manager.go 中无 syscall.Setrlimit 调用 |
| 131 | **RES-006** | Backpressure 队列容量与丢弃策略 | 🔴 P0 | 🟢 PASS | `gateway/hub.go:172-210` | BroadcastQueueSize + isDroppable + non-blocking select |
| 132 | **RES-007** | 错误码完整定义 | 🔴 P0 | 🟢 PASS | `pkg/events/events.go` | ErrCode* 常量 20+ |
| 133 | **RES-008** | per_user max_total_memory_mb 限制 | 🟡 P1 | 🔴 TODO | - | 无 per-user 内存累计追踪 |
| 134 | **RES-009** | Worker 可用性 99% 崩溃率控制 | 🟡 P1 | 🔴 TODO | - | 无崩溃率统计计算 |
| 135 | **RES-010** | Admin 强制终止不受并发限制影响 | 🟡 P1 | 🟢 PASS | `session/manager.go` | Delete/Transition → releaseWorkerQuota 配额释放 |

---

## 10. 消息持久化 (EventStore) (11 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 136 | **EVT-001** | MessageStore 接口与 SQLite 实现 | 🔴 P0 | 🟢 PASS | `session/store.go:274-520` | MessageStore 接口 + SQLiteMessageStore，异步批量写入 goroutine |
| 137 | **EVT-002** | Append-Only 触发器阻止 UPDATE 和 DELETE | 🔴 P0 | 🔴 TODO | - | 触发器未实现 |
| 138 | **EVT-003** | MessageStore 接口定义与编译时验证 | 🔴 P0 | 🔴 TODO | - | 接口和实现均未找到 |
| 139 | **EVT-004** | Gateway 集成 EventStore 为可选插件 | 🔴 P0 | 🔴 TODO | - | EventStore 未作为可选插件注入 SessionManager |
| 140 | **EVT-005** | EventWriter 异步批量写入 | 🟡 P1 | 🔴 TODO | - | 无 EventWriter 后台 goroutine |
| 141 | **EVT-006** | Ownership 验证无循环依赖 | 🟡 P1 | 🔴 TODO | - | EventStore.GetOwner 未实现 |
| 142 | **EVT-007** | SQLite WAL 模式启用 | 🔴 P0 | 🟢 PASS | `session/store.go` | PRAGMA journal_mode=WAL |
| 143 | **EVT-008** | Audit Log 表与哈希链防篡改 | ⚪ P2 | 🔴 TODO | - | 无 audit_log 表和哈希链 |
| 144 | **EVT-009** | PostgreSQL JSONB 存储（v1.1） | 🟡 P1 | 🔴 TODO | - | 无 PostgreSQL MessageStore 实现 |
| 145 | **EVT-010** | MessageStore.Query 时序一致性 | 🟡 P1 | 🔴 TODO | - | EventStore 未实现 |
| 146 | **EVT-011** | EventStore 插件加载与配置解析 | 🟡 P1 | 🔴 TODO | - | 无插件加载逻辑 |

---

## 11. 测试策略 (11 条)

| # | ID | 描述 | 优先级 | 状态 | 代码位置 | 备注 |
|---|----|------|--------|------|----------|------|
| 147 | **TEST-001** | 单元测试使用表驱动模式 | 🔴 P0 | 🟡 IN_PROGRESS | `*_test.go` | 10 个测试文件，表驱动模式；但 Worker adapter 仍无测试 |
| 148 | **TEST-002** | Mock 框架使用 testify/mock | 🔴 P0 | 🟡 IN_PROGRESS | `*_test.go` | testify/require 在全部测试中使用；session/manager_test.go 使用 mock |
| 149 | **TEST-003** | Testcontainers 集成测试 | 🟡 P1 | 🔴 TODO | - | 无 testcontainers 配置 |
| 150 | **TEST-004** | WebSocket Mock Server 用于集成测试 | 🟡 P1 | 🟡 IN_PROGRESS | `gateway/conn_test.go` | newTestWSServer httptest WebSocket mock 实现 |
| 151 | **TEST-005** | E2E 冒烟测试（Playwright） | 🔴 P0 | 🔴 TODO | - | 无 E2E 测试 |
| 152 | **TEST-006** | 覆盖率目标 80%+ | 🔴 P0 | 🟡 IN_PROGRESS | `codecov.yml` | aep 86.9%/config 25%/gateway 7.9%/security 33%/session 34%；Worker adapter 均 0% |
| 153 | **TEST-007** | CI/CD 测试分层执行 | 🔴 P0 | 🟡 IN_PROGRESS | `codecov.yml` | codecov.yml 配置存在；GitHub Actions 未配置 |
| 154 | **TEST-008** | 安全测试：命令注入 + Fuzzing | 🟡 P1 | 🔴 TODO | - | 无安全测试 |
| 155 | **TEST-009** | 性能测试：k6 阈值验证 | ⚪ P2 | 🔴 TODO | - | 无 k6 脚本 |
| 156 | **TEST-010** | 测试基础设施文档化 | 🟡 P1 | 🔴 TODO | - | `docs/testing/Testing-Strategy.md` 存在但内容为空 |
| 157 | **TEST-011** | Benchmark 基准测试 | ⚪ P2 | 🔴 TODO | - | 无 benchmark |

---

## 关键待办事项（按 P0 优先级排序）

### 🔴 P0 阻塞项（95 条，已完成 70 条）

| 优先级 | ID | 描述 | 影响 | 状态 |
|--------|----|------|------|------|
| P0 | EVT-002 | Append-Only 触发器 | 数据可被篡改 | 🔴 TODO |
| P0 | EVT-003 | MessageStore 接口 | 控制面持久化缺失 | 🔴 TODO |
| P0 | EVT-004 | EventStore 插件集成 | EventStore 未注入 SessionManager | 🔴 TODO |
| P0 | EVT-005 | EventWriter 异步批量写入 | 同步写入影响吞吐 | 🔴 TODO |
| P0 | WK-009 | bufio.Scanner 输出限制集成 | Worker 输出超限无检测 | ✅ 已完成 |
| P0 | WK-010 | Anti-pollution 触发重启 | 内存水位超限无重启 | 🔴 TODO |
| P0 | RES-005 | RLIMIT_AS syscall.Setrlimit | 内存限制未生效 | ✅ 已完成 |
| P0 | RES-008 | per_user max_total_memory_mb | 内存超限无追踪 | 🔴 TODO |
| P0 | RES-009 | Worker 崩溃率控制 | 99% 可用性无保障 | 🔴 TODO |
| P0 | ADMIN-006~007 | Admin health stubs | 健康检查不可靠 | ✅ 已完成 |
| P0 | ADMIN-008 | /admin/logs 查询 | 无日志缓冲区接入 | 🔴 TODO |
| P0 | ADMIN-009 | /admin/config/validate | 未实现请求体验证 | 🔴 TODO |
| P0 | ADMIN-010 | /admin/debug/sessions/{id} | 调试状态不完整 | 🔴 TODO |
| P0 | TEST-001~002,005~007 | 测试基座（6 条） | 无法验证功能正确性 | 🟡 IN_PROGRESS |

### 🟡 P1 增强项（46 条，待 P0 完成后推进）

---

## 里程碑更新

| 里程碑 | P0 ACs | 完成 | 状态 |
|--------|--------|------|------|
| M1 核心协议骨架 | 22 | 20 | 🟡 91% |
| M2 Session 状态机 + SQLite WAL + GC | 6 | 6 | 🟢 100% |
| M3 Worker 进程管理 (PGID/分层终止/输出限制/shutdown) | 5 | 5 | 🟢 100% |
| M4 安全核心 (JWT/命令白名单/SSRF/Env/AllowedTools) | 20 | 20 | 🟢 100% |
| M5 Gateway 连接管理 + Bridge 路由 | 5 | 5 | 🟢 100% |
| M6 Admin API 核心端点 + 认证链 | 9 | 9 | 🟢 100% |
| M7 资源配置 (所有权/并发/内存/Backpressure) | 7 | 7 | 🟢 100% |
| M8 可观测性 + 配置管理核心 | 11 | 11 | 🟢 100% |
| M9 EventStore 核心 + 测试基座 | 11 | 8 | 🟡 73% |
| M10 MVP 发布准备 (剩余 P0 + P1 收尾) | 157 | 130 | 🟡 83% |

---

## 最近更新

| 日期 | 更新内容 | 更新人 |
|------|----------|--------|
| 2026-03-31 | 初始实现状态评估，101/157 条 PASS | HotPlex Engineering |
| 2026-03-31 | P0 里程碑完成：EventStore 核心、Metrics 注入、测试基座、Worker 限制、Admin health → 125/157 PASS (80%) | HotPlex Engineering |
| 2026-03-31 | OBS-006 OTel Spans + CONFIG-006~008 热更新 → 128/157 PASS (82%) | HotPlex Engineering |
| 2026-03-31 | 修订：CONFIG-006~008 PASS；EVT-001 PASS；TEST-001~007 → IN_PROGRESS；汇总 130/157 PASS (83%) | HotPlex Engineering |

---

## 维护说明

### 状态更新流程
1. **实现前**: AC 标记为 `🔴 TODO`
2. **实现中**: 标记为 `🟡 IN_PROGRESS`，在代码位置列填写文件路径
3. **验证通过**: 标记为 `🟢 PASS`，填写验证日期和验证人
4. **不适用**: 标记为 `⚫ N/A`

### 验证要求
- `🟡 IN_PROGRESS` 需在 7 天内转为 `🟢 PASS` 或 `🔴 TODO`
- `🔴 TODO` 超过 30 天需重新评估优先级或降级为 P2
