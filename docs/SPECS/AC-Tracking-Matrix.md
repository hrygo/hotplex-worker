# 验收标准跟踪矩阵

> 文档版本: v1.0  |  最后更新: 2026-03-31

**状态:** ⬜ TODO | 🟦 IN_PROGRESS | 🟩 PASS | 🟥 FAIL | ⬛ N/A
**优先级:** 🔴 P0 = MVP 必须 | 🟡 P1 = 重要 | ⚪ P2 = 增强

---

### AEP v1 协议  (30 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 1 | **AEP-001** | Envelope 结构符合规范 | 🔴 P0 | ⬜ TODO |  |  |
| 2 | **AEP-002** | Init 握手（init / init_ack） | 🔴 P0 | ⬜ TODO |  |  |
| 3 | **AEP-003** | Input 事件（C→S） | 🔴 P0 | ⬜ TODO |  |  |
| 4 | **AEP-004** | State 事件（S→C — 状态变更） | 🔴 P0 | ⬜ TODO |  |  |
| 5 | **AEP-005** | Message.delta 事件（S→C — 流式输出） | 🔴 P0 | ⬜ TODO |  |  |
| 6 | **AEP-006** | Tool_call 和 Tool_result 事件 | 🟡 P1 | ⬜ TODO |  |  |
| 7 | **AEP-007** | Done 事件（S→C — 执行完成） | 🔴 P0 | ⬜ TODO |  |  |
| 8 | **AEP-008** | Error 事件（双向 — 错误通知） | 🔴 P0 | ⬜ TODO |  |  |
| 9 | **AEP-009** | Ping / Pong 事件（双向 — 心跳保活） | 🔴 P0 | ⬜ TODO |  |  |
| 10 | **AEP-010** | Control 事件（双向 — 控制命令） | 🟡 P1 | ⬜ TODO |  |  |
| 11 | **AEP-011** | Reasoning / Step / Raw / PermissionRequest / PermissionResponse 事件 | ⚪ P2 | ⬜ TODO |  |  |
| 12 | **AEP-012** | Message 事件（S→C — 完整消息） | 🟡 P1 | ⬜ TODO |  |  |
| 13 | **AEP-013** | Session 状态机 — 5 状态 | 🔴 P0 | ⬜ TODO |  |  |
| 14 | **AEP-014** | Session 状态机 — 竞态防护 | 🔴 P0 | ⬜ TODO |  |  |
| 15 | **AEP-015** | Session GC 策略 | 🟡 P1 | ⬜ TODO |  |  |
| 16 | **AEP-016** | Backpressure — 有界通道与 delta 丢弃 | 🔴 P0 | ⬜ TODO |  |  |
| 17 | **AEP-017** | 时序约束 — 事件顺序 | 🔴 P0 | ⬜ TODO |  |  |
| 18 | **AEP-018** | 时序约束 — 时间限制 | 🔴 P0 | ⬜ TODO |  |  |
| 19 | **AEP-019** | 断线重连（Reconnect / Resume） | 🔴 P0 | ⬜ TODO |  |  |
| 20 | **AEP-020** | Worker 启动失败与 Crash 检测 | 🔴 P0 | ⬜ TODO |  |  |
| 21 | **AEP-021** | 分层终止策略 | 🔴 P0 | ⬜ TODO |  |  |
| 22 | **AEP-022** | Seq 分配与去重 | 🔴 P0 | ⬜ TODO |  |  |
| 23 | **AEP-023** | Session 连接去重 | 🔴 P0 | ⬜ TODO |  |  |
| 24 | **AEP-024** | Minimal Compliance — 必须支持的事件 | 🔴 P0 | ⬜ TODO |  |  |
| 25 | **AEP-025** | Full Compliance — 可选扩展事件 | 🟡 P1 | ⬜ TODO |  |  |
| 26 | **AEP-026** | 能力协商（Client Caps / Server Caps） | 🟡 P1 | ⬜ TODO |  |  |
| 27 | **AEP-027** | Authentication — 握手阶段认证 | 🔴 P0 | ⬜ TODO |  |  |
| 28 | **AEP-028** | 消息持久化与 Event Replay | 🟡 P1 | ⬜ TODO |  |  |
| 29 | **AEP-029** | Executor 执行模型（Turn Event Flow） | 🔴 P0 | ⬜ TODO |  |  |
| 30 | **AEP-030** | 版本协商与兼容性 | 🔴 P0 | ⬜ TODO |  |  |

### WebSocket Gateway  (8 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 31 | **GW-001** | HTTP 握手 JWT 验证通过后升级为 WebSocket | 🔴 P0 | ⬜ TODO |  |  |
| 32 | **GW-002** | Init 握手协议正确处理会话创建与恢复 | 🔴 P0 | ⬜ TODO |  |  |
| 33 | **GW-003** | 心跳机制按规范间隔 ping 并检测对端失联 | 🔴 P0 | ⬜ TODO |  |  |
| 34 | **GW-004** | 同一 session_id 的新连接踢出旧连接 | 🔴 P0 | ⬜ TODO |  |  |
| 35 | **GW-005** | Bridge 双向事件转发正确路由 | 🔴 P0 | ⬜ TODO |  |  |
| 36 | **GW-006** | 优雅关闭 | 🟡 P1 | ⬜ TODO |  |  |
| 37 | **GW-007** | SeqGen 为每个 session 分配单调递增序号 | 🟡 P1 | ⬜ TODO |  |  |
| 38 | **GW-008** | 消息超长被拒绝 | 🟡 P1 | ⬜ TODO |  |  |

### Session 管理  (8 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 39 | **SM-001** | SQLite WAL 模式启用且 busy_timeout 正确配置 | 🔴 P0 | ⬜ TODO |  |  |
| 40 | **SM-002** | sessions 表 schema 与索引正确创建 | 🔴 P0 | ⬜ TODO |  |  |
| 41 | **SM-003** | 5 状态机转换规则被严格遵守 | 🔴 P0 | ⬜ TODO |  |  |
| 42 | **SM-004** | GC 定时清理 | 🔴 P0 | ⬜ TODO |  |  |
| 43 | **SM-005** | 状态转换与 input 处理在同一互斥锁内原子完成 | 🔴 P0 | ⬜ TODO |  |  |
| 44 | **SM-006** | mutex 显式命名 'mu'，零值安全，无 embedding | 🟡 P1 | ⬜ TODO |  |  |
| 45 | **SM-007** | SESSION_BUSY 错误码正确拒绝并发 input | 🔴 P0 | ⬜ TODO |  |  |
| 46 | **SM-008** | PoolManager 配额管理 | 🟡 P1 | ⬜ TODO |  |  |

### Worker 抽象与进程管理  (12 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 47 | **WK-001** | SessionConn 接口必须实现 | 🔴 P0 | ⬜ TODO |  |  |
| 48 | **WK-002** | Capabilities 接口正确声明各 Worker 类型能力 | 🟡 P1 | ⬜ TODO |  |  |
| 49 | **WK-003** | Claude Code Worker：--resume 恢复持久会话 | 🟡 P1 | ⬜ TODO |  |  |
| 50 | **WK-004** | OpenCode CLI Worker：无 --session-id，从 step_start 提取 sessionID | 🟡 P1 | ⬜ TODO |  |  |
| 51 | **WK-005** | OpenCode Server Worker：HTTP+SSE 托管进程模式 | ⚪ P2 | ⬜ TODO |  |  |
| 52 | **WK-006** | Hot-multiplexing：持久 Worker 在 turn 之间保持进程存活 | 🟡 P1 | ⬜ TODO |  |  |
| 53 | **WK-007** | PGID 隔离：Setpgid=true 防止信号误伤 Gateway 进程 | 🔴 P0 | ⬜ TODO |  |  |
| 54 | **WK-008** | 分层终止：SIGTERM → 5s grace period → SIGKILL | 🔴 P0 | ⬜ TODO |  |  |
| 55 | **WK-009** | 输出限制：64KB 初始 buffer，10MB 上限 | 🔴 P0 | ⬜ TODO |  |  |
| 56 | **WK-010** | Anti-pollution 触发重启：max_turns 或内存水位 | 🟡 P1 | ⬜ TODO |  |  |
| 57 | **WK-011** | Worker 进程僵死检测（LastIO）防止僵尸 IO 轮询 | 🟡 P1 | ⬜ TODO |  |  |
| 58 | **WK-012** | 所有 goroutine 有明确 shutdown 路径，无泄漏 | 🔴 P0 | ⬜ TODO |  |  |

### 安全  (34 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 59 | **SEC-001** | JWT 必须使用 ES256 签名 | 🔴 P0 | ⬜ TODO |  |  |
| 60 | **SEC-002** | JWT Claims 必须包含完整结构 | 🔴 P0 | ⬜ TODO |  |  |
| 61 | **SEC-003** | Token 生命周期必须正确实施 | 🔴 P0 | ⬜ TODO |  |  |
| 62 | **SEC-004** | WebSocket 认证流程必须安全 | 🔴 P0 | ⬜ TODO |  |  |
| 63 | **SEC-005** | JTI 必须使用 crypto/rand 生成 | 🔴 P0 | ⬜ TODO |  |  |
| 64 | **SEC-006** | JTI 黑名单必须正确撤销 Token | 🔴 P0 | ⬜ TODO |  |  |
| 65 | **SEC-007** | 多 Bot 隔离通过 ES256 + bot_id 实现 | 🔴 P0 | ⬜ TODO |  |  |
| 66 | **SEC-008** | API Key 比较使用恒定时间 | 🟡 P1 | ⬜ TODO |  |  |
| 67 | **SEC-010** | exec.Command 必须使用 []string 参数 | 🔴 P0 | ⬜ TODO |  |  |
| 68 | **SEC-011** | 命令白名单只允许 claude 和 opencode | 🔴 P0 | ⬜ TODO |  |  |
| 69 | **SEC-012** | 双层验证: 句法 + 语义 | 🔴 P0 | ⬜ TODO |  |  |
| 70 | **SEC-013** | SafePathJoin 完整安全流程 | 🔴 P0 | ⬜ TODO |  |  |
| 71 | **SEC-014** | 危险字符检测作为纵深防御 | 🟡 P1 | ⬜ TODO |  |  |
| 72 | **SEC-015** | BaseDir 白名单必须限制会话工作目录 | 🟡 P1 | ⬜ TODO |  |  |
| 73 | **SEC-016** | Model 白名单限制 AI 模型 | 🟡 P1 | ⬜ TODO |  |  |
| 74 | **SEC-020** | 仅允许 http/https 协议 | 🔴 P0 | ⬜ TODO |  |  |
| 75 | **SEC-021** | 所有私有 IP 段和保留地址必须被阻止 | 🔴 P0 | ⬜ TODO |  |  |
| 76 | **SEC-022** | DNS 重新绑定攻击防护 | 🔴 P0 | ⬜ TODO |  |  |
| 77 | **SEC-023** | URL 验证流程完整链路 | 🔴 P0 | ⬜ TODO |  |  |
| 78 | **SEC-024** | SSRFValidator 日志记录被阻止的请求 | 🟡 P1 | ⬜ TODO |  |  |
| 79 | **SEC-030** | BaseEnvWhitelist 限制基础环境变量 | 🔴 P0 | ⬜ TODO |  |  |
| 80 | **SEC-031** | Worker 类型特定白名单正确注入 | 🟡 P1 | ⬜ TODO |  |  |
| 81 | **SEC-032** | ProtectedEnvVars 绝对不可被覆盖 | 🔴 P0 | ⬜ TODO |  |  |
| 82 | **SEC-033** | 敏感模式检测正确识别秘密信息 | 🔴 P0 | ⬜ TODO |  |  |
| 83 | **SEC-034** | 保护变量始终被剥离 | 🔴 P0 | ⬜ TODO |  |  |
| 84 | **SEC-035** | HotPlex 必需变量正确注入 | 🟡 P1 | ⬜ TODO |  |  |
| 85 | **SEC-036** | Go 运行时环境变量白名单受保护 | 🟡 P1 | ⬜ TODO |  |  |
| 86 | **SEC-037** | 嵌套 Agent 调用被阻止 | 🟡 P1 | ⬜ TODO |  |  |
| 87 | **SEC-040** | AllowedTools 白名单限制可用工具 | 🔴 P0 | ⬜ TODO |  |  |
| 88 | **SEC-041** | BuildAllowedToolsArgs 正确构建 CLI 参数 | ⚪ P2 | ⬜ TODO |  |  |
| 89 | **SEC-042** | 工具分类 (Safe/Risky/Network/System) | 🟡 P1 | ⬜ TODO |  |  |
| 90 | **SEC-043** | 生产环境工具集无 Risky/Network 工具 | 🟡 P1 | ⬜ TODO |  |  |
| 91 | **SEC-044** | Dev 环境工具集包含所有工具 | ⚪ P2 | ⬜ TODO |  |  |
| 92 | **SEC-045** | Tool 调用通过 --allowed-tools 传递给 Worker | ⚪ P2 | ⬜ TODO |  |  |

### Admin API  (13 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 93 | **ADMIN-001** | GET /admin/sessions 返回会话列表 | 🔴 P0 | ⬜ TODO |  |  |
| 94 | **ADMIN-002** | GET /admin/sessions/{id} 获取会话详情 | 🔴 P0 | ⬜ TODO |  |  |
| 95 | **ADMIN-003** | DELETE /admin/sessions/{id} 强制终止会话 | 🔴 P0 | ⬜ TODO |  |  |
| 96 | **ADMIN-004** | GET /admin/stats 统计摘要 | 🔴 P0 | ⬜ TODO |  |  |
| 97 | **ADMIN-005** | GET /admin/metrics Prometheus 格式指标 | 🔴 P0 | ⬜ TODO |  |  |
| 98 | **ADMIN-006** | GET /admin/health Gateway 健康检查 | 🔴 P0 | ⬜ TODO |  |  |
| 99 | **ADMIN-007** | GET /admin/health/workers Worker 健康检查 | 🔴 P0 | ⬜ TODO |  |  |
| 100 | **ADMIN-008** | GET /admin/logs 查询日志 | 🟡 P1 | ⬜ TODO |  |  |
| 101 | **ADMIN-009** | POST /admin/config/validate 验证配置 | 🟡 P1 | ⬜ TODO |  |  |
| 102 | **ADMIN-010** | GET /admin/debug/sessions/{id} 会话调试状态 | 🟡 P1 | ⬜ TODO |  |  |
| 103 | **ADMIN-011** | Admin API 认证中间件完整认证链 | 🔴 P0 | ⬜ TODO |  |  |
| 104 | **ADMIN-012** | Admin API 分页行为 | 🟡 P1 | ⬜ TODO |  |  |
| 105 | **ADMIN-013** | Admin API 权限矩阵验证 | 🔴 P0 | ⬜ TODO |  |  |

### 配置管理  (10 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 106 | **CONFIG-001** | 配置加载 defaults.yaml + 环境覆盖 | 🔴 P0 | ⬜ TODO |  |  |
| 107 | **CONFIG-002** | ExpandEnv ${VAR} 和 ${VAR:-default} 语法支持 | 🔴 P0 | ⬜ TODO |  |  |
| 108 | **CONFIG-003** | 配置验证必填字段、类型、业务规则 | 🔴 P0 | ⬜ TODO |  |  |
| 109 | **CONFIG-004** | Secret Provider 三种实现 | 🔴 P0 | ⬜ TODO |  |  |
| 110 | **CONFIG-005** | 配置继承循环检测 | 🟡 P1 | ⬜ TODO |  |  |
| 111 | **CONFIG-006** | 配置热更新 fsnotify + 500ms 防抖 | 🟡 P1 | ⬜ TODO |  |  |
| 112 | **CONFIG-007** | 热更新动态字段与静态字段区分 | 🟡 P1 | ⬜ TODO |  |  |
| 113 | **CONFIG-008** | 配置变更审计日志 | 🟡 P1 | ⬜ TODO |  |  |
| 114 | **CONFIG-009** | 配置回滚 | ⚪ P2 | ⬜ TODO |  |  |
| 115 | **CONFIG-010** | 配置深度合并策略 | 🔴 P0 | ⬜ TODO |  |  |

### 可观测性  (10 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 116 | **OBS-001** | 日志格式 OTel Log Data Model 兼容 | 🔴 P0 | ⬜ TODO |  |  |
| 117 | **OBS-002** | 日志级别规范 DEBUG/INFO/WARN/ERROR/FATAL | 🔴 P0 | ⬜ TODO |  |  |
| 118 | **OBS-003** | Prometheus 指标命名规范 | 🔴 P0 | ⬜ TODO |  |  |
| 119 | **OBS-004** | RED 方法指标 API 层 | 🔴 P0 | ⬜ TODO |  |  |
| 120 | **OBS-005** | USE 方法指标基础设施层 | 🔴 P0 | ⬜ TODO |  |  |
| 121 | **OBS-006** | OTel Span 创建与上下文注入 | 🔴 P0 | ⬜ TODO |  |  |
| 122 | **OBS-007** | Tail Sampling 尾部采样策略 | 🟡 P1 | ⬜ TODO |  |  |
| 123 | **OBS-008** | SLO 定义与测量 | 🟡 P1 | ⬜ TODO |  |  |
| 124 | **OBS-009** | 告警规则症状告警而非根因告警 | 🟡 P1 | ⬜ TODO |  |  |
| 125 | **OBS-010** | Grafana Dashboard 核心面板 | 🟡 P1 | ⬜ TODO |  |  |

### 资源管理  (10 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 126 | **RES-001** | Session 所有权 JWT sub claim | 🔴 P0 | ⬜ TODO |  |  |
| 127 | **RES-002** | 权限矩阵 Owner vs Admin 隔离 | 🔴 P0 | ⬜ TODO |  |  |
| 128 | **RES-003** | 输出限制 10MB/20MB/1MB | 🔴 P0 | ⬜ TODO |  |  |
| 129 | **RES-004** | 并发限制 全局 20 / per_user 5 | 🔴 P0 | ⬜ TODO |  |  |
| 130 | **RES-005** | 内存限制 RLIMIT_AS | 🔴 P0 | ⬜ TODO |  |  |
| 131 | **RES-006** | Backpressure 队列容量与丢弃策略 | 🔴 P0 | ⬜ TODO |  |  |
| 132 | **RES-007** | 错误码完整定义 | 🔴 P0 | ⬜ TODO |  |  |
| 133 | **RES-008** | per_user max_total_memory_mb 限制 | 🟡 P1 | ⬜ TODO |  |  |
| 134 | **RES-009** | Worker 可用性 99% 崩溃率控制 | 🟡 P1 | ⬜ TODO |  |  |
| 135 | **RES-010** | Admin 强制终止不受并发限制影响 | 🟡 P1 | ⬜ TODO |  |  |

### 消息持久化  (11 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 136 | **EVT-001** | EventStore Schema 完整捕获所有事件类型 | 🔴 P0 | ⬜ TODO |  |  |
| 137 | **EVT-002** | Append-Only 触发器阻止 UPDATE 和 DELETE | 🔴 P0 | ⬜ TODO |  |  |
| 138 | **EVT-003** | MessageStore 接口定义与编译时验证 | 🔴 P0 | ⬜ TODO |  |  |
| 139 | **EVT-004** | Gateway 集成 EventStore 为可选插件 | 🔴 P0 | ⬜ TODO |  |  |
| 140 | **EVT-005** | EventWriter 异步批量写入 | 🟡 P1 | ⬜ TODO |  |  |
| 141 | **EVT-006** | Ownership 验证无循环依赖 | 🟡 P1 | ⬜ TODO |  |  |
| 142 | **EVT-007** | SQLite WAL 模式启用 | 🔴 P0 | ⬜ TODO |  |  |
| 143 | **EVT-008** | Audit Log 表与哈希链防篡改 | ⚪ P2 | ⬜ TODO |  |  |
| 144 | **EVT-009** | PostgreSQL JSONB 存储（v1.1） | 🟡 P1 | ⬜ TODO |  |  |
| 145 | **EVT-010** | MessageStore.Query 时序一致性 | 🟡 P1 | ⬜ TODO |  |  |
| 146 | **EVT-011** | EventStore 插件加载与配置解析 | 🟡 P1 | ⬜ TODO |  |  |

### 测试策略  (11 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 147 | **TEST-001** | 单元测试使用表驱动模式 | 🔴 P0 | ⬜ TODO |  |  |
| 148 | **TEST-002** | Mock 框架使用 testify/mock | 🔴 P0 | ⬜ TODO |  |  |
| 149 | **TEST-003** | Testcontainers 集成测试 | 🟡 P1 | ⬜ TODO |  |  |
| 150 | **TEST-004** | WebSocket Mock Server 用于集成测试 | 🟡 P1 | ⬜ TODO |  |  |
| 151 | **TEST-005** | E2E 冒烟测试（Playwright） | 🔴 P0 | ⬜ TODO |  |  |
| 152 | **TEST-006** | 覆盖率目标 80%+ | 🔴 P0 | ⬜ TODO |  |  |
| 153 | **TEST-007** | CI/CD 测试分层执行 | 🔴 P0 | ⬜ TODO |  |  |
| 154 | **TEST-008** | 安全测试：命令注入 + Fuzzing | 🟡 P1 | ⬜ TODO |  |  |
| 155 | **TEST-009** | 性能测试：k6 阈值验证 | ⚪ P2 | ⬜ TODO |  |  |
| 156 | **TEST-010** | 测试基础设施文档化 | 🟡 P1 | ⬜ TODO |  |  |
| 157 | **TEST-011** | Benchmark 基准测试 | ⚪ P2 | ⬜ TODO |  |  |

---

## 汇总看板

```
区域                     P0   P1   P2   总计   DONE   进度
──────────────────────────────────────────────────────────
AEP v1 协议              22    7    1    30      0    0%
WebSocket Gateway         5    3    0     8      0    0%
Session 管理              6    2    0     8      0    0%
Worker 抽象与进程管理     5    6    1    12      0    0%
安全                     20   11    3    34      0    0%
Admin API                 9    4    0    13      0    0%
配置管理                  5    4    1    10      0    0%
可观测性                  6    4    0    10      0    0%
资源管理                  7    3    0    10      0    0%
消息持久化 (EventStore)   5    5    1    11      0    0%
测试策略                  5    4    2    11      0    0%
──────────────────────────────────────────────────────────
总计                     95   57    9   157      0    0%
```

### MVP P0 进度

```
[P0 待完成] █░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ 0/95 (0%)

剩余 P0: 95 条
```

### 按状态分布

```
TODO      : ████████████████████████████████████████ 157
IN_PROGRESS: 0
PASS      : 0
FAIL      : 0
N/A       : 0
```

---

## 里程碑

| 里程碑 | 描述 | 目标日期 | P0 ACs | 状态 |
|--------|------|----------|--------|------|
| M1 | 核心协议骨架 (AEP Envelope/Init/State/Done/Error) | TBD | AEP-001~005, 007~009, 013~014, 016~018, 021~023, 027, 029~030 (30条) | TODO |
| M2 | Session 状态机 + SQLite WAL + GC | TBD | SM-001~005, 007 (6条) | TODO |
| M3 | Worker 进程管理 (PGID/分层终止/输出限制/shutdown) | TBD | WK-001, 007~009, 012 (5条) | TODO |
| M4 | 安全核心 (JWT/命令白名单/SSRF/Env/AllowedTools) | TBD | SEC-001~007, 010~013, 020~023, 030, 032~034, 040 (22条) | TODO |
| M5 | Gateway 连接管理 + Bridge 路由 | TBD | GW-001~005 (5条) | TODO |
| M6 | Admin API 核心端点 + 认证链 | TBD | ADMIN-001~007, 011, 013 (9条) | TODO |
| M7 | 资源配置 (所有权/并发/内存/Backpressure) | TBD | RES-001~007 (7条) | TODO |
| M8 | 可观测性 + 配置管理核心 | TBD | OBS-001~006, CONFIG-001~004, 010 (13条) | TODO |
| M9 | EventStore 核心 + 测试基座 | TBD | EVT-001~004, 007, TEST-001~002, 005~007 (11条) | TODO |
| M10 | MVP 发布准备 (剩余 P0 + P1 收尾) | TBD | 所有 P0 + P1 (152条) | TODO |

---

## 最近更新

| 日期 | 更新内容 | 更新人 |
|------|----------|--------|
| 2026-03-31 | 初始版本创建，157 条 AC 全部标记为 TODO | hotplex-worker |

---

## 维护说明

### 状态更新流程
1. **实现前**: AC 标记为 `TODO`
2. **实现中**: 实现者将状态更新为 `IN_PROGRESS`，填写"备注"列
3. **验证通过**: 验证人在"验证日期"和"验证人"列填写信息，状态改为 `PASS`
4. **验证失败**: 验证人在"备注"列填写失败原因，状态改为 `FAIL`，退回实现

### 提交规范
更新跟踪矩阵时，commit message 格式：
```
docs: update AC tracking matrix

- PASS: AEP-001, AEP-002 (实现者/验证人)
- IN_PROGRESS: GW-001, SM-001
- FAIL: SEC-001 (原因: JWT 库不支持 ES256)
```
