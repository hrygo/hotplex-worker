# HotPlex Worker Gateway — 验收标准 (AC)

> 文档版本: v1.0-draft
> 维护者: HotPlex Engineering
> 最后更新: 2026-03-31

本文档为 hotplex-worker 项目所有规划功能定义验收标准（Acceptance Criteria）。每条 AC 均可测试，包含正例、负例和边界条件。

**优先级说明：**
- **P0**: MVP 必须实现，发布前必须全部通过
- **P1**: 重要功能，MVP 后逐步实现
- **P2**: 增强功能，可安排在 v1.1/v1.2

---

## 目录

- [1. AEP v1 协议](#1-aep-v1-协议)
- [2. WebSocket Gateway](#2-websocket-gateway)
- [3. Session 管理](#3-session-管理)
- [4. Worker 抽象与进程管理](#4-worker-抽象与进程管理)
- [5. 安全](#5-安全)
- [6. Admin API](#6-admin-api)
- [7. 配置管理](#7-配置管理)
- [8. 可观测性](#8-可观测性)
- [9. 资源管理](#9-资源管理)
- [10. 消息持久化 (EventStore)](#10-消息持久化-eventstore)
- [11. 测试策略](#11-测试策略)

---

## 1. AEP v1 协议

### AEP-001 — Envelope 结构符合规范
**描述**: 所有 AEP v1 消息必须包含正确的 envelope 结构，所有字段类型和约束满足协议规范。

**验收标准**:
- Given 任意方向的消息, When Gateway 解析 JSON, Then version 字段必须为 'aep/v1'，否则返回 error.code='VERSION_MISMATCH' 且关闭 WS 连接
- Given 任意方向的消息, When aep.DecodeLine 被调用, Then 必须包含 non-empty 的 id/version/session_id/seq/timestamp/event.type，否则返回错误
- Given 有效 envelope, When seq <= 0, Then Validate 返回错误且 seq 不消耗
- Given 有效 envelope, When timestamp <= 0, Then Validate 返回错误
- Given 有效 envelope, When event.type 为空, Then Validate 返回错误
- Given 任意消息, When seq 被分配, Then seq 从 1 开始严格递增（同一 session 内），SeqGen 保证原子性
- Given 任意消息, When priority 字段缺失, Then 默认值为 'data'
- Given 任意消息, When priority='control', Then 跳过 backpressure 队列直接发送（hub.sendControlToSession 路径）
- Given 消息解析, When JSON 包含未知字段, Then DisallowUnknownFields 导致解码失败

### AEP-002 — Init 握手（init / init_ack）
**描述**: WS 连接建立后，第一条消息必须是 init；Gateway 必须响应 init_ack 或错误。

**验收标准**:
- Given WS 连接已建立, When ReadPump 执行, Then performInit 在 30s 内阻塞读取第一条消息，超时则关闭连接
- Given 第一条消息, When env.Event.Type != 'init', Then sendInitError 返回 PROTOCOL_VIOLATION 且 WS 关闭
- Given init 消息, When version != 'aep/v1', Then sendInitError 返回 VERSION_MISMATCH 且 WS 关闭
- Given init 消息, When worker_type 缺失或未知, Then sendInitError 返回 INVALID_MESSAGE 且 WS 关闭
- Given init 消息 (session_id 有值), When session 不存在, Then 创建新 session 并返回 init_ack(state: idle/created)
- Given init 消息 (session_id 有值), When session 状态为 DELETED, Then sendInitError 返回 SESSION_NOT_FOUND 且 WS 关闭
- Given init 消息 (session_id 有值), When session 状态为 TERMINATED, Then 尝试 resume，返回 init_ack(state: terminated)
- Given init_ack, Then 必须包含 session_id/state/server_caps，且 server_caps.protocol_version='aep/v1'
- Given init 握手完成, Then 后续消息进入标准消息循环（handler.Handle 路由）

### AEP-003 — Input 事件（C→S）
**描述**: Client 通过 input 事件发送用户任务；Session 繁忙时硬拒绝，不排队。

**验收标准**:
- Given Client 发送 input 消息, When session state = RUNNING, Then Gateway 返回 error.code='SESSION_BUSY'，input 不进入队列
- Given Client 发送 input 消息, When session state = TERMINATED/DELETED, Then Gateway 返回 error.code='SESSION_BUSY'（IsActive() = false）
- Given Client 发送 input 消息, When session state = IDLE/CREATED, Then TransitionWithInput 原子地将状态置为 RUNNING 并转发给 Worker
- Given input 处理, When TransitionWithInput 返回 ErrInvalidTransition, Then 返回 error.code='SESSION_BUSY'
- Given input 消息, When Worker 已 attached, Then w.Input(ctx, content) 被调用
- Given input 和 state 转换, When TransitionWithInput, Then 在同一 mutex 内完成（ms.mu.Lock）

### AEP-004 — State 事件（S→C — 状态变更）
**描述**: Gateway 向 Client 推送 session 状态变更事件，状态集合为 {created, running, idle, terminated}。

**验收标准**:
- Given session 状态从 CREATED → RUNNING, Then StateNotifier 发送 state{state: 'running'}
- Given Worker 执行完毕, When Transition → IDLE, Then 发送 state{state: 'idle'} 且设置 idle_expires_at = now + idle_timeout
- Given session 异常终止, When Transition → TERMINATED, Then 发送 state{state: 'terminated'}
- Given state 事件, Then state 字段必须是 {created, running, idle, terminated} 之一
- Given state 事件, Then 发送前调用 hub.NextSeq 分配 seq

### AEP-005 — Message.delta 事件（S→C — 流式输出）
**描述**: 唯一流式输出事件类型，每条 delta 代表文本/代码/图片的增量片段。

**验收标准**:
- Given Worker 输出流, When Worker Adapter 解析输出, Then 文本行被转换为 message.delta{ delta: { type: 'text', text: <line> } }
- Given message.delta, Then delta.type 必须是 'text'|'code'|'image' 之一
- Given message.delta, Then delta.text 包含实际增量文本内容
- Given message.delta, When Worker 是 raw stdout 类型（pi-mono）, Then 每行 stdout 转换为一条 delta
- Given message.delta, When 消息被 backpressure 丢弃, Then seq 不消耗（sessionDropped flag 设为 true）

### AEP-006 — Tool_call 和 Tool_result 事件（S→C）
**描述**: Worker 自行执行 tool（AUTONOMOUS 模式），tool_call/tool_result 仅为通知，不要求 Client 参与。

**验收标准**:
- Given Worker 执行 tool, When Claude Code 发送 tool_use 事件, Then 映射为 AEP tool_call { id, name, status, arguments }
- Given tool_call, Then 必须包含 id（调用唯一标识）、name（工具名）、arguments（参数对象）
- Given tool_result, Then 必须包含 tool_call_id 字段与对应的 tool_call.id 配对
- Given 并行 tool 调用, When Worker 发送 tool_call(c1) 和 tool_call(c2), Then Client 通过 tool_call_id 配对，不假设严格串行顺序

### AEP-007 — Done 事件（S→C — 执行完成）
**描述**: Turn 的终止符，标志一次 input 处理的结束；必须跟随 error（如果出错）或为最后一个 event。

**验收标准**:
- Given Worker 执行完毕（exit code=0）, Then 发送 done{ success: true, stats: { duration_ms, tool_calls, ... } }
- Given Worker crash（exit code 139 SIGSEGV/137 SIGKILL/143 SIGTERM）, Then 先发送 error 再发送 done(false)
- Given done 事件, Then done 必须是 turn 的最后一个 event（seq 最大）
- Given done, When 本轮发生过 delta 丢弃, Then done.data.dropped = true（UI Reconciliation flag）

### AEP-008 — Error 事件（双向 — 错误通知）
**描述**: 结构化错误码覆盖所有异常场景；error 必须在 done 之前，不单独终止 turn。

**验收标准**:
- Given Worker crash（SIGSEGV）, Then error.code='WORKER_CRASH' 且 details 包含 exit_code: 139
- Given Worker OOM（SIGKILL 137）, Then error.code='WORKER_OOM'
- Given Worker 被 SIGTERM 终止, Then error.code='PROCESS_SIGTERM'（exit code 143）
- Given Worker 僵死超时, When execution_timeout 超限, Then error.code='EXECUTION_TIMEOUT' 且最终 SIGKILL
- Given Worker 启动失败（binary 不存在/权限不足）, Then error.code='WORKER_START_FAILED'
- Given 单行 stdout 超过 10MB, Then error.code='WORKER_OUTPUT_LIMIT'
- Given session 不存在, Then error.code='SESSION_NOT_FOUND'
- Given session 正在执行, Then error.code='SESSION_BUSY'（硬拒绝）
- Given error 事件, Then 后跟 done(false) 才终止 turn

### AEP-009 — Ping / Pong 事件（双向 — 心跳保活）
**描述**: 应用层心跳保活，间隔 30s，3 次无响应视为断线。

**验收标准**:
- Given WS 连接建立, When WritePump 运行, Then 每 54s（pingPeriod = pongWait*9/10 = 54s）发送 WebSocket PingMessage
- Given Client 发送 ping, When Gateway 接收, Then 发送 pong{data: { state: <current_state> }}
- Given pong 响应, When WebSocket 底层 pong handler 触发, Then hb.MarkAlive() 被调用，missed count 重置为 0
- Given hb.MarkMissed() 返回 true, When missed >= 3, Then 连接被关闭（max missed pongs = 90s）
- Given pong 响应, Then 包含当前 session state（通过 sm.Get 获取）

### AEP-010 — Control 事件（双向 — 控制命令）
**描述**: Client→Server（terminate/delete）和 Server→Client（reconnect/session_invalid/throttle）两类控制消息。

**验收标准**:
- Given Client 发送 control{action:'terminate'}, When 处理, Then Transition → TERMINATED，发送 error(SESSION_TERMINATED) + done(false)
- Given Client 发送 control{action:'delete'}, When 处理, Then sm.Delete → DELETED（绕过 TERMINATED），不发送 done
- Given Server 发送 control{action:'reconnect'}, Then env.priority='control' 且 bypass backpressure
- Given Server 发送 control{action:'throttle'}, Then data 包含 reason/suggestion（max_message_rate/backoff_ms/retry_after）

### AEP-011 — Reasoning / Step / Raw / PermissionRequest / PermissionResponse 事件
**描述**: 可选扩展事件类型（Full Compliance），Minimal Compliance 不要求实现。

**验收标准**:
- Given Worker 输出 thinking 事件, When Worker 是 Claude Code, Then 映射为 message.delta 或 reasoning{ text, visibility }
- Given permission_response, When granted=true, Then Worker 继续执行 tool
- Given permission_response, When granted=false, Then Worker 跳过 tool 执行
- Given Unknown event type, When 接收, Then 未知字段被忽略（forward compatible），不报错

### AEP-012 — Message 事件（S→C — 完整消息）
**描述**: Turn 结束时的完整消息聚合，message 是所有 delta 的完整聚合。

**验收标准**:
- Given Worker 输出完整 assistant 消息, Then 发送 message{ role: 'assistant', content: [...] }
- Given delta 丢弃发生过, When done 发送前, Then 如果 Worker 提供了 message，Gateway 必须下发完整 message（UI Reconciliation）

### AEP-013 — Session 状态机 — 5 状态
**描述**: Session 必须在 CREATED/RUNNING/IDLE/TERMINATED/DELETED 五个状态之间按规则转换。

**验收标准**:
- Given session 创建, Then 初始状态为 CREATED
- Given CREATED, When fork+exec 成功, Then → RUNNING
- Given CREATED, When 启动失败（binary 不存在/权限不足/环境错误）, Then → TERMINATED（不经过 RUNNING）
- Given RUNNING, When Worker 执行完毕, Then → IDLE
- Given IDLE, When 收到新 input, Then → RUNNING
- Given RUNNING, When crash/timeout/kill, Then → TERMINATED
- Given IDLE, When idle_timeout/max_lifetime/GC/kill, Then → TERMINATED
- Given TERMINATED, When resume（重启 runtime），Then → RUNNING
- Given TERMINATED, When GC retention_period 过期, Then → DELETED
- Given 任意转换, When 违反 ValidTransitions 规则, Then Transition 返回 ErrInvalidTransition
- Given IsActive() 检查, Then CREATED/RUNNING/IDLE 返回 true，TERMINATED/DELETED 返回 false

### AEP-014 — Session 状态机 — 竞态防护
**描述**: Resume TOCTOU、done/input 竞态、并发 input 等场景必须被正确防护。

**验收标准**:
- Given Resume TOCTOU, When init 处理 session_id, Then 存活检查和 init_ack 发送在同一 ms.mu 临界区内完成
- Given done/input 竞态, When Worker 发送 done 同时 Client 发送 input, Then TransitionWithInput 和 done 处理共享 ms.mu.Lock，input 的 state 检查和转换原子完成
- Given 并发 input, When 两个 Client 同时发送 input 到同一 session, Then ms.mu.Lock 保证串行化，第一个成功，第二个收到 SESSION_BUSY
- Given Heartbeat 与 Reconnect 并存, When 新连接 JoinSession(sessionID), Then 旧连接被 Close()

### AEP-015 — Session GC 策略
**描述**: 后台 GC goroutine 定期清理过期 session，防止资源泄漏。

**验收标准**:
- Given GC 运行, When IDLE session 超过 idle_timeout（默认 30min）, Then → TERMINATED
- Given GC 运行, When session 超过 max_lifetime（默认 24h）, Then → TERMINATED
- Given GC 运行, When TERMINATED session 超过 retention_period（默认 7d）, Then → DELETED（删除 DB 记录）
- Given GC 运行, When RUNNING session 超过 execution_timeout 无 IO, Then → TERMINATED（Zombie IO Polling）
- Given GC 扫描间隔, Then 每 60s 执行一次（cfg.Session.GCScanInterval）

### AEP-016 — Backpressure — 有界通道与 delta 丢弃
**描述**: Worker 产出过快时，使用有界 channel；message.delta 可丢弃，关键事件必须送达。

**验收标准**:
- Given hub.broadcast channel, Then 容量由 broadcastQueueSize 决定（默认 256，config 可配置）
- Given SendToSession 执行, When event.type = message.delta 或 raw, Then isDroppable=true，进入非阻塞 select
- Given isDroppable 且 channel 满, When 非阻塞发送失败, Then delta 被静默丢弃，sessionDropped[sessionID]=true，seq 不消耗
- Given 非 isDroppable 事件（message/done/error/state/tool_call/tool_result/control）, When channel 满, Then 返回 error（broadcast queue full）
- Given done 发送前, When GetAndClearDropped(sessionID)=true, Then done.data.dropped=true

### AEP-017 — 时序约束 — 事件顺序
**描述**: 事件因果顺序必须满足协议规定的约束。

**验收标准**:
- Given turn 开始, Then state(running) 必须是第一个 S→C event（seq=1）
- Given turn 结束, Then done 必须是最后一个 S→C event
- Given error 事件, Then error 必须在 done 之前
- Given tool_call, When 发送 tool_result, Then tool_result.tool_call_id 与 tool_call.id 匹配

### AEP-018 — 时序约束 — 时间限制
**描述**: 心跳间隔、断线检测、进程终止等有明确的时间约束。

**验收标准**:
- Given WS Ping, When WritePump 运行, Then pingPeriod = 54s（pongWait*9/10）
- Given pong 超时, When ReadDeadline exceeded, Then missed += 1
- Given missed >= 3, Then 标记断线并关闭连接（90s 无响应）
- Given Worker SIGTERM, When 发送后, Then 等待最多 5s（分层终止 Phase 2）
- Given 5s 后进程仍存活, When SIGTERM 超时, Then SIGKILL 强制终止
- Given init 握手, When performInit 读取第一条消息, Then deadline = now + 30s

### AEP-019 — 断线重连（Reconnect / Resume）
**描述**: Client 断线后，Session 保持，runtime 可选保持；重连后通过 session_id 恢复。

**验收标准**:
- Given Client 断线, When WS 连接关闭, Then hub.LeaveSession 被调用，session 保留在 map 中
- Given Client 重新连接, When 发送 init{session_id}, Then handler.sm.Get 检查 session 存在性
- Given session 状态为 IDLE/TERMINATED, When init resume, Then init_ack 返回当前 state
- Given reconnect 场景, When 新连接 JoinSession, Then 旧连接被 Close()

### AEP-020 — Worker 启动失败与 Crash 检测
**描述**: Worker 启动失败和运行时 crash 必须被正确检测并向 Client 报告。

**验收标准**:
- Given runtime 启动失败（binary 不存在/权限不足/环境错误）, When Start() 返回错误, Then session 直接从 CREATED → TERMINATED
- Given 启动失败, When transition 完成, Then 发送 state(terminated) + error(WORKER_START_FAILED) + done(false)
- Given Worker 运行时 crash（SIGSEGV exit code 139）, When bridge 收到 EOF, Then 发送 error(WORKER_CRASH) + done(false)
- Given Worker 僵死（进程存活但无输出）, When LastIO() 超时, Then 发送 error(EXECUTION_TIMEOUT) + 分层终止 + done(false)

### AEP-021 — 分层终止策略
**描述**: SIGTERM → 等待 5s → SIGKILL，确保 Worker 优雅退出。

**验收标准**:
- Given admin kill, When 终止 Worker, Then 首先发送 SIGTERM（优雅终止信号）
- Given SIGTERM 发送后, When 进程在 5s 内退出, Then 终止完成，不发送 SIGKILL
- Given SIGTERM 发送后, When 5s 后进程仍存活, Then 发送 SIGKILL（强制终止）
- Given Admin force kill, When RUNNING/IDLE session, Then 直接 → DELETED（绕过 TERMINATED）且 SIGKILL

### AEP-022 — Seq 分配与去重
**描述**: seq 在同一 session 内严格递增；丢弃的 delta 不消耗 seq。

**验收标准**:
- Given hub.NextSeq(sessionID), Then 原子地返回 seq[sessionID]++，SeqGen.mu 保证线程安全
- Given delta 丢弃, When hub.SendToSession 返回 nil, Then seq 不消耗
- Given key event（message/done/error/control/state）, When 发送成功, Then seq 正常递增

### AEP-023 — Session 连接去重
**描述**: 同一 session_id 只保留一个活跃 WS 连接；新连接自动降级旧连接。

**验收标准**:
- Given JoinSession(sessionID, conn), When 该 session 已有连接, Then 旧连接被 c.Close()
- Given LeaveSession, When conn 从 session 移除后无剩余连接, Then delete(h.sessions, sessionID)

### AEP-024 — Minimal Compliance — 必须支持的事件
**描述**: Minimal Compliance 实现必须支持 C→S: init/input/control/ping 和 S→C: init_ack/message.delta/state/error/done/pong。

**验收标准**:
- Given Minimal Compliance 实现, When 接收 init, Then 必须处理并返回 init_ack 或错误
- Given Minimal Compliance 实现, When 接收 input, Then 必须转发给 Worker
- Given Minimal Compliance 实现, When 接收 ping, Then 必须回复 pong（附带当前 state）
- Given Minimal Compliance 实现, When turn 结束, Then 必须发送 done 事件

### AEP-025 — Full Compliance — 可选扩展事件
**描述**: Full Compliance 额外支持 message/tool_call/tool_result/reasoning/step/raw/permission_request/permission_response 和 Server-originated control。

### AEP-026 — 能力协商（Client Caps / Server Caps）
**描述**: Init 时交换 client_caps 和 server_caps；Gateway 只发送 Client 声明支持的事件类型。

**验收标准**:
- Given init 消息, When Client 发送 client_caps, Then Gateway 记录支持的 kinds 列表
- Given Server Caps, Then DefaultServerCaps 返回 SupportsResume/Delta/ToolCall/Ping = true

### AEP-027 — Authentication — 握手阶段认证
**描述**: WebSocket 升级时进行认证，拒绝未授权连接。

**验收标准**:
- Given HTTP WS upgrade 请求, When auth.AuthenticateRequest 返回错误, Then 返回 401 Unauthorized
- Given 认证失败, Then WS 连接不被建立，不进入消息循环

### AEP-028 — 消息持久化与 Event Replay
**描述**: Gateway 不负责 event log 和 replay；Worker 自身负责上下文持久化。

**验收标准**:
- Given Gateway, Then 不存储 event log（不写 sessions 表的 context_json 字段用于 event replay）
- Given reconnect, When Client 重连, Then Worker 自身持久化提供断线期间的输出，Gateway 不补发

### AEP-029 — Executor 执行模型（Turn Event Flow）
**描述**: Turn 内的标准 event 序列：state(running) → [message.delta* → tool_call? → tool_result? → message?] → done

**验收标准**:
- Given Turn 开始, When Transition → RUNNING, Then 第一个 S→C event 必须是 state(running)
- Given Turn 结束, When done 发送, Then SESSION_BUSY 锁释放，下一个 input 可进入

### AEP-030 — 版本协商与兼容性
**描述**: 版本协商在 init 握手中完成；未知字段/type 向前兼容。

**验收标准**:
- Given init{version:'aep/v2'}, When Gateway 仅支持 aep/v1, Then sendInitError(VERSION_MISMATCH) + WS close
- Given 未知 event.type, When 接收, Then 不报错（forward compatible）
- Given 未知 JSON 字段, When 解码, Then json.Decoder.DisallowUnknownFields 导致解码失败

---

## 2. WebSocket Gateway

### GW-001 — HTTP 握手 JWT 验证通过后升级为 WebSocket
**描述**: 客户端携带有效 JWT 发起 WS 握手请求，Gateway 在 HTTP 层完成认证后升级为 WebSocket 连接，并注册到 Hub。

**验收标准**:
- Given Authorization header 包含有效 JWT, When 客户端 GET /ws?session_id=xxx, Then HTTP 101 Upgrade 响应，WebSocket 连接建立，Hub.conns 中包含该连接
- Given Authorization header 缺失或 JWT 无效, When 客户端发起 WS 握手, Then HTTP 401 Unauthorized，连接不升级
- Given Origin header 不在 AllowedOrigins 白名单, When 客户端发起 WS 握手, Then HTTP 403 Forbidden 或拒绝 Upgrade
- Given WS 连接成功升级但首帧非 init 类型, When 首帧到达 ReadPump, Then 发送 init_ack error(protocol_violation)，关闭连接

### GW-002 — Init 握手协议正确处理会话创建与恢复
**描述**: 首帧必须为 init 消息，Gateway 校验 version/worker_type 后创建新会话或恢复已有会话。

**验收标准**:
- Given init.version == 'aep/v1' 且 worker_type 存在, When 客户端发送 init, Then 创建新会话（state=CREATED），发送 init_ack(state=CREATED)
- Given init.version 不匹配, When 客户端发送 init, Then 发送 init_ack error(version_mismatch)，关闭连接
- Given session_id 对应已存在的非 DELETED 会话, When 客户端发送 init (resume), Then 恢复该会话，发送 init_ack(state=当前状态)
- Given session_id 对应已 DELETED 会话, When 客户端发送 init, Then 发送 init_ack error(session_not_found)，关闭连接

### GW-003 — 心跳机制按规范间隔 ping 并检测对端失联
**描述**: WritePump 每 54s 发送一次 PingMessage；ReadPump 累计 3 次未收到 Pong 后主动断开连接。

**验收标准**:
- Given WS 连接已建立, When WritePump 定时器触发 (pingPeriod=54s), Then 发送 websocket.PingMessage
- Given 客户端正常响应 Pong, When ReadPump 的 SetPongHandler 回调触发, Then hb.MarkAlive() 重置 missed 计数器为 0
- Given 客户端累计 3 次未响应 Pong (read deadline exceeded), When 第 3 次超时触发, Then hb.MarkMissed() 返回 true，ReadPump 退出，连接关闭

### GW-004 — 同一 session_id 的新连接踢出旧连接（会话去重）
**描述**: Hub.JoinSession 将新连接加入 sessionID 映射时，自动关闭同一 session_id 的所有旧连接。

**验收标准**:
- Given 连接 A 已订阅 session_id=X, When 连接 B 调用 JoinSession(X, B), Then Hub.Close(A)，A 收到 WS 关闭帧
- Given 连接 A 订阅 session_id=X，连接 B 订阅 session_id=Y, When A 断开, Then B 不受影响，Y 的订阅映射不变

### GW-005 — Bridge 双向事件转发正确路由
**描述**: Bridge.forwardEvents 将 Worker 事件注入 Hub.SendToSession；Handler.handleInput 将客户端 Input 路由到 Worker.Input。

**验收标准**:
- Given Worker 产生 message.delta / tool_call / done 等事件, When Bridge.forwardEvents 收到, Then 注入 hub.SendToSession，客户端通过 WS 收到该事件
- Given 客户端发送 input 事件, When Handler.handleInput 处理, Then Worker.Input(content) 被调用，session 状态原子迁移到 RUNNING
- Given Worker 产生 done 事件且期间有 backpressure 丢弃 delta, When Bridge.forwardEvents 处理 done, Then done.data.stats.dropped == true 标记回传客户端

### GW-006 — 优雅关闭
**描述**: Conn.Close 关闭 done channel 后，WritePump 的 select 立即响应并退出；Hub.Shutdown 等待所有连接优雅关闭。

**验收标准**:
- Given Hub.Shutdown 被调用, When 遍历 Hub.conns 执行 c.Close, Then 所有连接收到 WS CloseNormalClosure 帧后关闭
- Given Hub.Shutdown context 超时, When 优雅关闭超时, Then 返回 context.DeadlineExceeded，已关闭的连接保持关闭

### GW-007 — SeqGen 为每个 session 分配单调递增序号
**描述**: Hub.seqGen 为每条出站消息分配全局单调递增的 seq，客户端可据此检测丢包或乱序。

**验收标准**:
- Given 同一 session_id 有 N 条消息出站, Then seq 严格单调递增（seq_N == seq_1 + N - 1），无跳号
- Given 不同 session_id 的消息序列独立, Then session_A 的 seq 不影响 session_B 的 seq
- Given message.delta 在 backpressure 时被静默丢弃, Then seq 不为丢弃的 delta 分配序号，客户端不会看到 seq 空洞

### GW-008 — 消息超长被拒绝
**描述**: ReadPump 设置 maxMessageSize (32KB) 限制，单帧超过此大小的消息触发协议错误。

**验收标准**:
- Given 单帧消息大小 <= 32KB, When ReadPump 解析, Then 正常处理
- Given 单帧消息大小 > 32KB, When ReadPump 读取, Then 发送 error(code=INVALID_MESSAGE)，连接保持但消息被丢弃

---

## 3. Session 管理

### SM-001 — SQLite WAL 模式启用且 busy_timeout 正确配置
**描述**: NewSQLiteStore 初始化时执行 PRAGMA journal_mode=WAL 和 PRAGMA busy_timeout=5000。

**验收标准**:
- Given 新建 SQLiteStore, When Store 创建完成, Then db.Exec('PRAGMA journal_mode=WAL') 无错误，WAL 文件存在
- Given 并发写入场景（两个 goroutine 同时写）, When 第二个写入阻塞, Then 最多等待 5000ms 后返回 SQLITE_BUSY

### SM-002 — sessions 表 schema 与索引正确创建
**描述**: migrate 函数在首次启动时创建 sessions 表及所有必要索引。

**验收标准**:
- Given 首次启动, When NewSQLiteStore.migrate 执行, Then 创建 sessions 表，字段: id(PK), user_id, worker_session_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, is_active, context_json
- Given sessions 表已存在, When Upsert 写入记录, Then ON CONFLICT(id) 执行 UPDATE，覆盖 state/updated_at/expires_at/idle_expires_at/is_active/context_json

### SM-003 — 5 状态机转换规则被严格遵守
**描述**: ValidTransitions map 定义了所有合法转换，非法转换返回 ErrInvalidTransition。

**验收标准**:
- Given state=CREATED, When Transition(RUNNING), Then 成功，IsActive()==true
- Given state=CREATED, When Transition(IDLE), Then 返回 ErrInvalidTransition，不变更状态
- Given state=TERMINATED, When Transition(RUNNING), Then 成功（resume 场景）
- Given state=DELETED, When Transition(*), Then 返回 ErrInvalidTransition，DELETED 是终态

### SM-004 — GC 定时清理
**描述**: runGC 每 GCScanInterval（默认 60s）触发一次，依次处理僵尸进程、超期 TERMINATED/DELETED 清理和空闲超时。

**验收标准**:
- Given 会话已超时 max_lifetime (expires_at <= now), When GC 执行, Then Transition(TERMINATED, 'max_lifetime')
- Given state=IDLE 且 idle_expires_at <= now, When GC 执行, Then Transition(TERMINATED, 'idle_timeout')
- Given 会话 state=TERMINATED 且 updated_at <= now - RetentionPeriod, When GC 执行, Then DELETE FROM sessions
- Given Worker 进程 lastIO 距今超过 ExecutionTimeout, When GC 执行 zombie 检测, Then Transition(TERMINATED, 'zombie')
- Given GC goroutine 已启动, When Manager.Close, Then gcStop() 被调用，runGC 响应 ctx.Done() 并 return

### SM-005 — 状态转换与 input 处理在同一互斥锁内原子完成
**描述**: TransitionWithInput 同时持有 ms.mu，将 state 变更和 input 记录打包为一次原子操作。

**验收标准**:
- Given ms.mu 由 TransitionWithInput 持有（写锁）, When 并发 goroutine 调用 Get, Then 阻塞等待 ms.mu.RUnlock
- Given TransitionWithInput 内部调用 store.Upsert 失败, When DB 写入错误, Then ms.info.state 未变更，错误向上传播
- Given TransitionWithInput 迁移到 IDLE, Then idle_expires_at = now + IdleTimeout，持久化到 DB

### SM-006 — mutex 显式命名 'mu'，零值安全，无 embedding
**描述**: 所有会话级互斥锁字段名为 mu，RWMutex 使用零值，不通过结构体 embedding 继承。

**验收标准**:
- Given 代码将 mutex 通过结构体 embedding 继承, When golangci-lint 检查, Then 报错：禁止 embedding sync.Mutex
- Given managedSession.mu 是 sync.RWMutex, When GetWorker 读取 worker, Then 使用 RLock，不阻塞其他读操作

### SM-007 — SESSION_BUSY 错误码正确拒绝并发 input
**描述**: 当会话不处于活跃状态时（RUNNING/IDLE/CREATED 之外），input 事件被拒绝并返回 SESSION_BUSY。

**验收标准**:
- Given state=TERMINATED, When 客户端发送 input, Then 发送 error(code=SESSION_BUSY, message='session not active: terminated')
- Given state=RUNNING 且已有 input 在处理中, When 第二条 input 同时到达, Then TransitionWithInput 返回 ErrSessionBusy，发送 error(code=SESSION_BUSY)
- Given SESSION_BUSY 错误发送后, When 客户端轮询重试且 state 已变为 IDLE, Then 接受该 input

### SM-008 — PoolManager 配额管理
**描述**: AttachWorker 调用 pool.Acquire，全部 worker 退出或 Delete 时调用 pool.Release。

**验收标准**:
- Given maxSize=10，当前 totalCount=10, When 新 AttachWorker, Then 返回 ErrPoolExhausted，拒绝 attach
- Given maxIdlePerUser=3，用户甲已有 3 个 session, When 用户甲新建第 4 个 session, Then 返回 ErrUserQuotaExceeded
- Given Delete(session_id) 调用, When 状态已为 DELETED, Then 无操作（幂等），不重复扣减 pool 配额

---

## 4. Worker 抽象与进程管理

### WK-001 — SessionConn 接口必须实现
**描述**: 所有 Worker 适配器必须实现 SessionConn 接口，编译时通过 var _ Worker = (*XWorker)(nil) 验证。

**验收标准**:
- Given Worker 已 Start, When Send(ctx, env), Then env 写入 Worker stdin，无阻塞（ctx 控制超时）
- Given Worker 已 Start, When Recv() 被调用, Then 返回只读 channel，Worker 终止后 channel 关闭
- Given Worker 已 Start, When Close(), Then stdin/stdout/stderr 全部关闭，Recv channel 关闭，无文件描述符泄漏

### WK-002 — Capabilities 接口正确声明各 Worker 类型能力
**描述**: Capabilities 接口返回 Type / SupportsResume / SupportsStreaming / SupportsTools / EnvWhitelist / SessionStoreDir。

**验收标准**:
- Given ClaudeCode Worker, When Capabilities 检查, Then Type()=='claude_code', SupportsResume()==true, SessionStoreDir() 返回非空路径
- Given OpenCode CLI Worker, When Capabilities 检查, Then Type()=='opencode_cli', SupportsResume()==false，EnvWhitelist 列出允许的环境变量名
- Given Pi-mono Worker, When Capabilities 检查, Then Type()=='pi-mono', SupportsResume()==false，SessionStoreDir()==''

### WK-003 — Claude Code Worker：--resume 恢复持久会话
**描述**: Claude Code 适配器通过 --print --session-id 和 --resume 参数实现持久化。

**验收标准**:
- Given Start(session) 启动, When exec.CommandContext 构造, Then args 包含 --print, --session-id, --model 等参数
- Given Resume(session) 调用, When 构造命令, Then args 包含 --resume 参数（区别于 Start）

### WK-004 — OpenCode CLI Worker：无 --session-id，从 step_start 事件提取 sessionID
**描述**: OpenCode CLI 不提供会话恢复机制，sessionID 需要从 Worker 输出的 step_start 事件中提取。

**验收标准**:
- Given OpenCode CLI Worker 启动, When 构造命令, Then args 不包含 --session-id 相关参数
- Given Worker 输出第一行 step_start 事件, When Parser 解析, Then 从 event.data.session_id 或 event.data.id 提取并记录 sessionID

### WK-005 — OpenCode Server Worker：HTTP+SSE 托管进程模式
**描述**: OpenCodeServerManager 启动 opencode serve 进程，通过 HTTP POST 和 SSE 获取事件。

**验收标准**:
- Given SSE 连接断开 (Worker 崩溃), When Bridge 检测到 Recv 关闭, Then Bridge goroutine 退出，状态变为 TERMINATED

### WK-006 — Hot-multiplexing：持久 Worker 在 turn 之间保持进程存活
**描述**: 持久 Worker (Claude Code, OpenCode Server) 不在 turn 间退出，内存中保持对话上下文。

**验收标准**:
- Given Claude Code Worker 已 Start, When 客户端发送多条 input, Then Worker 进程持续运行，PID 不变
- Given Worker 处于 IDLE 状态, When GC 扫描, Then 不被错误终止（只有 zombie/lifetime/idle_timeout 才触发）

### WK-007 — PGID 隔离：Setpgid=true 防止信号误伤 Gateway 进程
**描述**: Worker 进程以独立进程组启动，SIGTERM/SIGKILL 仅影响 Worker 子树。

**验收标准**:
- Given proc.Manager.Start() 调用, When 命令执行, Then cmd.SysProcAttr.Setpgid == true，进程组 PGID == cmd.Process.Pid
- Given proc.Manager.PGID() 返回值, When SIGTERM 发送, Then 发送给整个进程组 (-pgid)，Gateway 主进程不受影响

### WK-008 — 分层终止：SIGTERM → 5s grace period → SIGKILL
**描述**: proc.Manager.Terminate 先发送 SIGTERM，超时后强制 SIGKILL。

**验收标准**:
- Given Worker 进程运行中, When Terminate(ctx, SIGTERM, 5s) 调用, Then syscall.Kill(-pgid, SIGTERM) 被调用，日志记录 'sent SIGTERM'
- Given Worker 在 5s 后仍未退出, When grace period 超时, Then 调用 Kill() 发送 SIGKILL，日志记录 'graceful shutdown timeout, sending SIGKILL'

### WK-009 — 输出限制：64KB 初始 buffer，10MB 上限
**描述**: Worker stdout 解析使用 bufio.Scanner，初始 64KB，上限 10MB。

**验收标准**:
- Given Worker 输出单行 <= 64KB, When Scanner 读取, Then 正常解析
- Given Worker 输出单行 > 10MB, When Scanner 读取, Then 返回错误 error(code=WORKER_OUTPUT_LIMIT)，Worker 被终止

### WK-010 — Anti-pollution 触发重启：max_turns 或内存水位
**描述**: Worker 累计 turn 数或内存占用超限后，Gateway 触发 Terminate 并重新 Start。

**验收标准**:
- Given init.config.max_turns=N 已设置, When 第 N+1 条 input 到达, Then Transition(TERMINATED)，触发 Worker 重启流程
- Given Worker 内存占用超过配置水位, When GC 扫描 zombie 进程时检测, Then Transition(TERMINATED, 'anti-pollution')

### WK-011 — Worker 进程僵死检测（LastIO）防止僵尸 IO 轮询
**描述**: GC goroutine 检查 Worker.LastIO()，若超过 ExecutionTimeout 无 IO 活动则视为僵尸进程。

**验收标准**:
- Given Worker 正在执行长任务（有 stdout 输出）, When LastIO() 被 GC 查询, Then 返回最近一次 IO 时间
- Given 配置 ExecutionTimeout=0, When GC 执行, Then 使用默认值 5 分钟

### WK-012 — 所有 goroutine 有明确 shutdown 路径，无泄漏
**描述**: 每个启动的 goroutine 必须通过 ctx cancel、channel close 或 WaitGroup 有可预期的退出。

**验收标准**:
- Given ReadPump goroutine, When conn.Close() 被调用, Then ReadPump 的 for 循环因 websocket 读取错误退出，goroutine 结束
- Given Bridge.forwardEvents goroutine, When Worker.Recv() channel 关闭, Then for range 退出，goroutine 结束
- Given gc goroutine, When Manager.Close(), Then ctx cancel，runGC 的 select 响应 <-ctx.Done() 并 return

---

## 5. 安全

### SEC-001 — JWT 必须使用 ES256 签名
**描述**: Gateway 验证的所有 JWT 必须使用 ES256 (ECDSA P-256 SHA-256) 算法签名，HS256 必须被拒绝。

**验收标准**:
- Given 使用 ES256 签名且有效的 JWT, When JWTValidator.Validate 被调用, Then 返回 JWTClaims 且 error 为 nil
- Given 使用 HS256 签名的 JWT, When JWTValidator.Validate 被调用, Then 返回 ErrUnauthorized
- Given 使用未知算法 (如 none, RS256) 签名的 JWT, When JWTValidator.Validate 被调用, Then 返回 ErrUnauthorized

### SEC-002 — JWT Claims 必须包含完整结构
**描述**: Gateway 接受的所有 JWT 必须包含 RFC 7519 标准字段 (iss, sub, aud, exp, iat, jti) 以及 HotPlex 扩展字段 (role, scope, bot_id, session_id)。

**验收标准**:
- Given 有效 JWT 中 iss 字段为 'hotplex-worker', When JWTValidator.Validate, Then claims.Issuer == 'hotplex-worker'
- Given 有效 JWT 中 exp 字段已过期, When JWTValidator.Validate, Then 返回 ErrUnauthorized (token expired)
- Given JWTClaims 中存在 bot_id 字段, When Validate, Then claims.BotID 可正确解析为非空字符串

### SEC-003 — Token 生命周期必须正确实施
**描述**: 三类 Token 生命周期: Access (5min), Gateway (1h), Refresh (7d)。Gateway 拒绝已过期 Token。

**验收标准**:
- Given Access Token 过期超过 1 秒, When JWTValidator.Validate, Then 返回 ErrUnauthorized 且包含 'expired'
- Given Token 的 exp 字段使用 iat + ttl 方式计算, When GenerateToken 被调用, Then 生成的 exp == iat + ttl

### SEC-004 — WebSocket 认证流程必须安全
**描述**: WebSocket 连接通过 Cookie 或 Authorization Header 传递 JWT，init envelope 中可携带 JWT 进行消息级认证。

**验收标准**:
- Given WS Handshake 请求带有 Authorization: Bearer <jwt>, When Authenticator.AuthenticateRequest, Then 返回 userID 且 error 为 nil
- Given WS Handshake 请求无任何认证信息, When AuthenticateRequest, Then 返回 ErrUnauthorized
- Given Authorization header 值包含 'Bearer ' 前缀, When Validate, Then 自动 StripPrefix 'Bearer ' 后再解析

### SEC-005 — JTI 必须使用 crypto/rand 生成
**描述**: JWT ID (jti) 必须使用 crypto/rand 生成符合 RFC 7519 §4.1.7 的 UUID v4，绝对禁止使用 math/rand 或时间戳。

**验收标准**:
- Given 调用 GenerateJTI(), When 生成 JTI, Then 使用 crypto/rand.Read 读取 16 字节熵
- Given crypto/rand 读取失败, When GenerateJTI, Then 返回错误而非使用 math/rand 回退
- Given JTI 生成逻辑中包含 time.Now() 或时间戳, When 代码审查, Then 判定为安全漏洞

### SEC-006 — JTI 黑名单必须正确撤销 Token
**描述**: 被撤销的 Token jti 必须进入内存黑名单 (TTL 缓存)，后续 Validate 调用必须返回 ErrTokenRevoked。

**验收标准**:
- Given Token A 的 jti 进入黑名单, When JWTValidator.Validate(Token A), Then 返回 ErrTokenRevoked
- Given Token A 的 jti 进入黑名单后 TTL 过期, When JWTValidator.Validate(Token A), Then 返回正常 claims (黑名单已清理)

### SEC-007 — 多 Bot 隔离通过 ES256 + bot_id 实现
**描述**: 不同 bot_id 的 Token 不可跨 Bot 使用，Gateway 必须验证 Token 中的 bot_id 与请求的 Bot 匹配。

**验收标准**:
- Given Token 中 bot_id = 'bot-A', When Gateway 处理请求, Then 仅允许操作 bot-A 下的 session
- Given Token 中 bot_id = 'bot-A' 但请求 session_id 属于 bot-B, When Gateway 处理, Then 返回 ErrUnauthorized

### SEC-008 — API Key 比较使用恒定时间
**描述**: API Key 验证必须使用 crypto/subtle.ConstantTimeCompare 防止时序攻击。

**验收标准**:
- Given API Key 比较函数, When 执行比较, Then 使用 subtle.ConstantTimeCompare 而非 == 操作符

### SEC-010 — exec.Command 必须使用 []string 参数
**描述**: Gateway 执行外部命令时必须使用 exec.Command(name, args...) 传参，禁止使用 shell=true 或字符串拼接。

**验收标准**:
- Given BuildSafeCommand('claude', '--print', '--session-id', id), When 构建命令, Then 调用 exec.Command("claude", []string{...}...) 而非 shell 拼接
- Given 任何 exec.Command 调用使用 shell=true, When golangci-lint 检查, Then 触发安全告警

### SEC-011 — 命令白名单只允许 claude 和 opencode
**描述**: AllowedCommands 白名单只包含 'claude' 和 'opencode'，其他命令名必须被 ValidateCommand 拒绝。

**验收标准**:
- Given name = 'claude', When ValidateCommand(name), Then 返回 nil (允许)
- Given name = 'bash', When ValidateCommand(name), Then 返回 error 且包含 'not in whitelist'
- Given name = '/usr/bin/claude' (含路径), When ValidateCommand(name), Then 返回 error (命令名不能含路径分隔符)

### SEC-012 — 双层验证: 句法 + 语义
**描述**: 所有输入必须经过句法层 (JSON Schema, 类型, 长度) 和语义层 (白名单, 业务规则) 两层验证。

**验收标准**:
- Given 输入长度超过 MaxEnvelopeBytes (1MB), When InputValidator.ValidateInput, Then 返回 'input too large'
- Given 合法的 JSON Schema 且长度在限制内, When InputValidator.ValidateInput, Then 返回 nil

### SEC-013 — SafePathJoin 完整安全流程
**描述**: SafePathJoin 必须按顺序执行: filepath.Clean → 拒绝绝对路径 → filepath.Join → filepath.EvalSymlinks → 前缀验证。

**验收标准**:
- Given userPath = '/etc/passwd' (绝对路径), When SafePathJoin, Then 返回 error 且包含 'absolute paths not allowed'
- Given baseDir='/tmp/hotplex', userPath='symlink→/etc', When SafePathJoin, Then 解析 symlink 后检测到逃逸，返回 error
- Given baseDir='/tmp/hotplex', userPath='symlink→/tmp/hotplex/allowed', When SafePathJoin, Then 解析 symlink 后在允许范围内，返回路径

### SEC-014 — 危险字符检测作为纵深防御
**描述**: ContainsDangerousChars 检测 shell 元字符，即使在非 shell exec 模式下也触发告警。

**验收标准**:
- Given input = 'foo; bar', When ContainsDangerousChars, Then 返回 true
- Given input = '$(whoami)', When ContainsDangerousChars, Then 返回 true (含 $, (, ))
- Given input = 'normal_text', When ContainsDangerousChars, Then 返回 false

### SEC-015 — BaseDir 白名单必须限制会话工作目录
**描述**: AllowedBaseDirs 只包含 /var/hotplex/projects 和 /tmp/hotplex，ValidateBaseDir 拒绝其他路径。

**验收标准**:
- Given baseDir = '/var/hotplex/projects', When ValidateBaseDir, Then 返回 nil
- Given baseDir = '/home/user/projects', When ValidateBaseDir, Then 返回 error 且包含 'not in whitelist'

### SEC-016 — Model 白名单限制 AI 模型
**描述**: AllowedModels 限制可用的 AI 模型标识符，防止配置错误的模型请求。

**验收标准**:
- Given model = 'claude-sonnet-4-6', When ValidateModel, Then 返回 nil (大小写不敏感)
- Given model = 'gpt-4', When ValidateModel, Then 返回 error 且包含 'not in allowed list'

### SEC-020 — 仅允许 http/https 协议
**描述**: URL 验证必须拒绝非 http/https URL (如 file://, gopher://, ftp://, data://)。

**验收标准**:
- Given URL = 'http://example.com', When ValidateURL, Then 返回 nil (允许)
- Given URL = 'file:///etc/passwd', When ValidateURL, Then 返回 URLValidationError

### SEC-021 — 所有私有 IP 段和保留地址必须被阻止
**描述**: blockedCIDRs 包含所有私有地址段: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, ::1/128, fc00::/7, fe80::/10, 169.254.0.0/16, 0.0.0.0/8。

**验收标准**:
- Given IP = '10.0.0.1', When ValidateIP, Then 返回 error (10.0.0.0/8)
- Given IP = '169.254.169.254', When ValidateIP, Then 返回 error (云元数据端点)
- Given IP = '8.8.8.8', When ValidateIP, Then 返回 nil (公网 IP)

### SEC-022 — DNS 重新绑定攻击防护
**描述**: ValidateURL 必须执行 DNS 解析后检查所有返回 IP，并阻止特定主机名。

**验收标准**:
- Given hostname = 'localhost', When ValidateURL, Then 返回 error 且 Kind = 'blocked_hostname'
- Given hostname = 'metadata.google.internal', When ValidateURL, Then 返回 error (blocked_hostnames map)

### SEC-023 — URL 验证流程完整链路
**描述**: ValidateURL 按顺序执行: url.Parse → 空 host 检查 → 主机名黑名单 → IP 段前缀检查 → DNS 解析 → 所有 IP 检查 blockedCIDRs。

**验收标准**:
- Given URL = 'http://' (无 host), When ValidateURL, Then 返回 error 且 Kind = 'empty_host'
- Given URL = 'http://user:pass@evil.com@10.0.0.1/', When ValidateURL, Then url.Parse 提取 host = 'evil.com'（防止 @ 混淆）

### SEC-024 — SSRFValidator 日志记录被阻止的请求
**描述**: 所有被 SSRFValidator.Validate 阻止的 URL 必须以 WARN 级别记录日志。

**验收标准**:
- Given 存在恶意 URL 被阻止, When SSRFValidator.Validate 执行, Then slog.Warn 被调用，包含 url、kind、reason 字段

### SEC-030 — BaseEnvWhitelist 限制基础环境变量
**描述**: Worker 进程只能接收 BaseEnvWhitelist 中的系统变量: HOME, USER, SHELL, PATH, TERM, LANG, LC_ALL, PWD。

**验收标准**:
- Given os.Getenv('HOME') 返回非空, When SafeEnvBuilder.Build, Then 结果中包含 HOME=<value>
- Given os.Getenv('RANDOM_VAR') 返回非空, When SafeEnvBuilder.Build, Then 结果中不包含 RANDOM_VAR

### SEC-031 — Worker 类型特定白名单正确注入
**描述**: AddWorkerType 根据 worker 类型注入对应环境变量。

**验收标准**:
- Given workerType = 'claude-code', When AddWorkerType 后 Build, Then whitelisted vars 包含 CLAUDE_API_KEY, CLAUDE_MODEL, CLAUDE_BASE_URL
- Given workerType = 'opencode-cli', When AddWorkerType 后 Build, Then whitelisted vars 包含 OPENAI_API_KEY, OPENAI_BASE_URL, OPENCODE_API_KEY

### SEC-032 — ProtectedEnvVars 绝对不可被覆盖
**描述**: SafeEnvBuilder.AddHotPlexVar 和 AddSecret 必须拒绝任何尝试设置 ProtectedEnvVars 中的变量。

**验收标准**:
- Given AddHotPlexVar('HOME', '/evil/path'), When AddHotPlexVar, Then 返回 error 且包含 'protected system variable'
- Given AddHotPlexVar('PATH', '/usr/bin:/evil'), When AddHotPlexVar, Then 返回 error (PATH 在 ProtectedEnvVars)
- Given AddHotPlexVar('HOTPLEX_SESSION_ID', 'valid-id'), When AddHotPlexVar, Then 返回 nil (非 ProtectedEnvVars)

### SEC-033 — 敏感模式检测正确识别秘密信息
**描述**: IsSensitive 通过前缀和正则模式检测敏感环境变量，BuildWorkerEnv 对匹配项返回 [REDACTED]。

**验收标准**:
- Given key = 'AWS_ACCESS_KEY_ID', When IsSensitive, Then 返回 true (AWS_ 前缀)
- Given key = 'DATABASE_URL', When IsSensitive, Then 返回 true (exact match)
- Given key = 'USERNAME', When IsSensitive, Then 返回 false (无敏感模式)

### SEC-034 — 保护变量始终被剥离
**描述**: BuildWorkerEnv 必须无条件剥离 protectedVars 中的变量 (CLAUDECODE, GATEWAY_ADDR, GATEWAY_TOKEN)。

**验收标准**:
- Given env map 中存在 CLAUDECODE='...', When BuildWorkerEnv, Then 结果中不包含 CLAUDECODE
- Given env map 中存在 GATEWAY_TOKEN='secret', When BuildWorkerEnv, Then 结果中不包含 GATEWAY_TOKEN

### SEC-035 — HotPlex 必需变量正确注入
**描述**: SafeEnvBuilder 必须支持 HOTPLEX_SESSION_ID 和 HOTPLEX_WORKER_TYPE 作为必需变量注入。

**验收标准**:
- Given AddHotPlexVar('HOTPLEX_SESSION_ID', 'sess-123'), When Build, Then env 中包含 HOTPLEX_SESSION_ID=sess-123
- Given AddHotPlexVar('HOTPLEX_WORKER_TYPE', 'claude-code'), When Build, Then env 中包含 HOTPLEX_WORKER_TYPE=claude-code

### SEC-036 — Go 运行时环境变量白名单受保护
**描述**: GOPROXY, GOSUMDB 在 GoEnvWhitelist 中允许传递给 Worker，但 GOPATH/GOROOT 在 ProtectedEnvVars 中不可被覆盖。

**验收标准**:
- Given SafeEnvBuilder 默认初始化, When Build, Then whitelisted vars 包含 GOPROXY, GOSUMDB
- Given AddHotPlexVar('GOPATH', '/evil'), When AddHotPlexVar, Then 返回 error (GOPATH 在 ProtectedEnvVars)

### SEC-037 — 嵌套 Agent 调用被阻止
**描述**: StripNestedAgent 从环境变量切片中移除 CLAUDECODE=，防止 Worker 进程启动嵌套的 Claude Code Agent。

**验收标准**:
- Given env = ['HOME=/home/user', 'CLAUDECODE=abc123', 'PATH=/usr/bin'], When StripNestedAgent, Then 返回 ['HOME=/home/user', 'PATH=/usr/bin'] (CLAUDECODE 已移除)
- Given env 中无 CLAUDECODE, When StripNestedAgent, Then 返回原始 env 不变

### SEC-040 — AllowedTools 白名单限制可用工具
**描述**: ValidateTools 检查所有请求的工具是否在 AllowedTools 白名单中。

**验收标准**:
- Given tools = ['Read', 'Edit', 'Write'], When ValidateTools, Then 返回 nil (均在白名单)
- Given tools = ['Exec', 'RunCommand'], When ValidateTools, Then 返回 error 且包含 'not in allowed list' 和工具名
- Given tools = nil, When ValidateTools, Then 返回 nil (空列表视为无限制)

### SEC-041 — BuildAllowedToolsArgs 正确构建 CLI 参数
**描述**: BuildAllowedToolsArgs 将工具列表转换为 --allowed-tools flag 的 []string 切片格式。

**验收标准**:
- Given tools = ['Read', 'Edit'], When BuildAllowedToolsArgs, Then 返回 ['--allowed-tools', 'Read', '--allowed-tools', 'Edit']
- Given tools = [], When BuildAllowedToolsArgs, Then 返回空切片

### SEC-042 — 工具分类 (Safe/Risky/Network/System)
**描述**: AllowedTools 中的工具按风险等级分类: Safe(Read/Edit/Write/Grep/Glob), Risky(Bash), Network(WebFetch), System(Agent/NotebookEdit)。

**验收标准**:
- Given IsToolAllowed('Read'), When 调用, Then 返回 true
- Given IsToolAllowed('Bash'), When 调用, Then 返回 true (Risky 但在 AllowedTools 中)

### SEC-043 — 生产环境工具集无 Risky/Network 工具
**描述**: ProductionAllowedTools 只包含 Safe 类工具，禁止 Bash 和 WebFetch。

**验收标准**:
- Given ProductionAllowedTools map, When 检查键, Then 不包含 'Bash', 'WebFetch', 'Agent', 'NotebookEdit'

### SEC-044 — Dev 环境工具集包含所有工具
**描述**: DevAllowedTools 包含全部 10 个工具，包括 Risky (Bash) 和 Network (WebFetch)。

### SEC-045 — Tool 调用通过 --allowed-tools 传递给 Worker
**描述**: Gateway 将 ValidateTools 通过的 tool 列表构建为 --allowed-tools 参数，传递给 worker CLI 进程。

---

## 6. Admin API

### ADMIN-001 — GET /admin/sessions 返回会话列表
**描述**: 管理员携带有效 Token 请求会话列表，Gateway 应返回分页的会话数据。

**验收标准**:
- Given 有效 Admin Token (Bearer)，When GET /admin/sessions，Then 返回 HTTP 200 且 body 包含 sessions 数组、total、limit、offset 字段
- Given state=IDLE 查询参数，When GET /admin/sessions?state=IDLE，Then 仅返回 state=IDLE 的会话
- Given 无 Token，When GET /admin/sessions，Then 返回 HTTP 401 且 error_code=missing_admin_token
- Given Token 缺少 session:list 权限，When GET /admin/sessions，Then 返回 HTTP 403 且 error_code=permission_denied

### ADMIN-002 — GET /admin/sessions/{id} 获取会话详情
**描述**: 管理员获取指定会话的完整详情，包括 worker_process 和 stats 信息。

**验收标准**:
- Given 有效 Token 且有 session:read 权限，When GET /admin/sessions/sess_abc123，Then 返回 HTTP 200 且包含全部字段
- Given 不存在的 session_id，When GET /admin/sessions/sess_notfound，Then 返回 HTTP 404 且 error_code=session_not_found

### ADMIN-003 — DELETE /admin/sessions/{id} 强制终止会话
**描述**: 管理员强制终止指定会话，先发 SIGTERM，若 5 秒未退出则发 SIGKILL。

**验收标准**:
- Given 处于 RUNNING 状态的会话，When DELETE /admin/sessions/{id}，Then Worker 进程先收到 SIGTERM
- Given SIGTERM 后 5 秒内未退出，When DELETE /admin/sessions/{id}，Then Worker 进程收到 SIGKILL
- Given 不存在的 session_id，When DELETE /admin/sessions/sess_notfound，Then 返回 HTTP 404

### ADMIN-004 — GET /admin/stats 统计摘要
**描述**: 返回 Gateway、Worker、Database 层的实时统计汇总。

**验收标准**:
- Given 有效 Token，When GET /admin/stats，Then gateway.uptime_seconds >= 0 且 gateway.websocket_connections == 当前活跃 WS 连接数
- Given Token 缺少 stats:read 权限，When GET /admin/stats，Then 返回 HTTP 403

### ADMIN-005 — GET /admin/metrics Prometheus 格式指标
**描述**: 返回 Prometheus text exposition format 的实时指标。

**验收标准**:
- Given 有效 Token，When GET /admin/metrics，Then Content-Type 为 text/plain; version=0.0.4
- Given 有效 Token，When GET /admin/metrics，Then 包含 hotplex_sessions_active、hotplex_sessions_created_total、hotplex_worker_crashes_total

### ADMIN-006 — GET /admin/health Gateway 健康检查
**描述**: 返回 Gateway 整体健康状态，包含 gateway、database、workers 子检查。

**验收标准**:
- Given Gateway 所有组件正常，When GET /admin/health，Then 返回 HTTP 200 且 status=healthy
- Given /admin/health 不需要认证，When GET /admin/health，Then 即使无 Token 也返回 200（health:read 豁免）

### ADMIN-007 — GET /admin/health/workers Worker 健康检查
**描述**: 对每种 Worker 类型执行主动探测，返回可用性和错误状态。

**验收标准**:
- Given Worker 类型健康，When GET /admin/health/workers，Then worker.status=healthy 且 test_passed=true
- Given Worker 进程崩溃过，When GET /admin/health/workers，Then worker.status=degraded 且包含 error

### ADMIN-008 — GET /admin/logs 查询日志
**描述**: 支持按 level、session_id、user_id、start_time、end_time、limit 过滤日志。

**验收标准**:
- Given level=error 查询参数，When GET /admin/logs?level=error，Then 仅返回 level=error 的日志
- Given 无 Token，When GET /admin/logs，Then 返回 HTTP 401

### ADMIN-009 — POST /admin/config/validate 验证配置
**描述**: 对指定配置文件进行完整验证，返回 valid、warnings、errors。

**验收标准**:
- Given 缺少必填字段（如 gateway.server_addr），When POST /admin/config/validate，Then errors 包含必填字段错误
- Given 依赖检查失败（如 JWT enabled 但无 public_key_file），When POST /admin/config/validate，Then errors 包含依赖错误

### ADMIN-010 — GET /admin/debug/sessions/{id} 会话调试状态
**描述**: 返回内部调试信息，包括 input_queue_size、mutex 状态、进程详情。

**验收标准**:
- Given 有效 Token，When GET /admin/debug/sessions/sess_abc123，Then session.internal_state 包含 input_queue_size、last_seq_sent、mutex_locked
- Given Token 缺少 debug:read 权限，When GET /admin/debug/sessions/{id}，Then 返回 HTTP 403

### ADMIN-011 — Admin API 认证中间件完整认证链
**描述**: Token 提取 → Token 验证 → IP 白名单 → 权限检查 四个步骤必须全部执行。

**验收标准**:
- Given 缺少 Authorization header，When 请求任意 /admin/* 端点（health 除外），Then 返回 HTTP 401
- Given 来自非白名单 IP，When 请求，Then 即使 Token 有效也返回 HTTP 403 且 error_code=ip_not_allowed
- Given Rate Limit 超出，When 1 秒内请求超过 10 次，Then 返回 HTTP 429 且 error_code=rate_limit_exceeded

### ADMIN-012 — Admin API 分页行为
**描述**: 会话列表查询的分页边界条件处理。

**验收标准**:
- Given offset 超过总记录数，When GET /admin/sessions?offset=9999999，Then 返回空 sessions 数组且 total=实际总数
- Given limit 负数，When GET /admin/sessions?limit=-5，Then 返回 HTTP 400

### ADMIN-013 — Admin API 权限矩阵验证
**描述**: 验证每个端点与对应权限的精确映射。

**验收标准**:
- Given Token 具有 session:list 但无 session:read，When GET /admin/sessions/sess_abc123，Then 返回 HTTP 403
- Given Token 同时具有 session:list 和 session:read，When 同时访问 list 和 detail，Then 两次均返回 200

---

## 7. 配置管理

### CONFIG-001 — 配置加载 defaults.yaml + 环境覆盖
**描述**: 系统必须按 defaults.yaml → 环境配置（dev/staging/prod）→ config.yaml 的顺序加载。

**验收标准**:
- Given configs/_defaults/defaults.yaml 存在且定义了 gateway.server_addr=:8080，When 启动 Gateway，Then server_addr 字段取默认值 :8080
- Given HOTPLEX_ENV=staging，When 启动 Gateway，Then 加载 configs/environments/staging.yaml 并覆盖 defaults.yaml 中的同名字段
- Given config.yaml 中存在 inherits 字段指向 ./environments/prod.yaml，When 加载 config.yaml，Then 先加载 parent 再加载当前文件

### CONFIG-002 — ExpandEnv ${VAR} 和 ${VAR:-default} 语法支持
**描述**: 必须支持 shell 风格的 ${VAR} 和 ${VAR:-default} 语法，标准库 os.ExpandEnv 不支持后者。

**验收标准**:
- Given 环境变量 HOTPLEX_SERVER_ADDR=localhost:9090，When YAML 字段为 "${HOTPLEX_SERVER_ADDR}"，Then 展开为 "localhost:9090"
- Given 环境变量未设置，When YAML 字段为 "${HOTPLEX_VAR:-fallback}"，Then 展开为 "fallback"
- Given 环境变量值为空字符串（非未设置），When YAML 字段为 "${EMPTY_VAR:-default}"，Then 展开为空字符串（不使用默认值）

### CONFIG-003 — 配置验证必填字段、类型、业务规则
**描述**: 配置加载后必须进行必填字段检查、类型检查和业务规则验证。

**验收标准**:
- Given gateway.server_addr 为空，When Validate()，Then 返回错误且 message 包含 "gateway.server_addr is required"
- Given auth.jwt.enabled=true 但无 public_key_file，When Validate()，Then 返回依赖错误
- Given 所有必填字段存在且类型正确，When Validate()，Then 返回 nil

### CONFIG-004 — Secret Provider 三种实现
**描述**: 支持多种 Secret Provider，ChainedProvider 按顺序尝试各 Provider。

**验收标准**:
- Given EnvProvider，When Get("HOTPLEX_JWT_PUBLIC_KEY")，Then 返回环境变量值
- Given ChainedProvider([EnvProvider, VaultProvider])，When Get("VAR_IN_ENV")，Then EnvProvider 返回后直接使用，不调用 VaultProvider
- Given ChainedProvider，所有 Provider 均失败，When Get，Then 返回错误且 message 包含 "not found in any provider"

### CONFIG-005 — 配置继承循环检测
**描述**: 支持配置继承链（A inherits B inherits C），但必须检测循环继承。

**验收标准**:
- Given config_a.yaml inherits config_b.yaml，config_b.yaml inherits config_a.yaml，When 加载 config_a，Then 返回错误且 message 包含 "cyclic config inheritance detected"

### CONFIG-006 — 配置热更新 fsnotify + 500ms 防抖
**描述**: 配置文件变更后，以 500ms 防抖延迟自动重新加载。

**验收标准**:
- Given 500ms 内发生多次 Write 事件，When 连续写入，Then 仅在最后一次写入 500ms 后触发一次 reload（防抖生效）
- Given reload 失败（文件损坏），When Watcher 重新加载，Then 不更新内存配置，保持上一个有效配置

### CONFIG-007 — 热更新动态字段与静态字段区分
**描述**: 仅 HotReloadableFields 中的字段支持热更新，StaticFields 需要重启。

**验收标准**:
- Given gateway.logging.level 在 HotReloadableFields 中，When 热更新修改该字段，Then 新值立即生效且无需重启
- Given gateway.server_addr 在 StaticFields 中，When 热更新修改该字段，Then 忽略修改，记录警告日志建议重启

### CONFIG-008 — 配置变更审计日志
**描述**: 每次配置变更必须记录审计日志。

**验收标准**:
- Given 配置从 {logging:{level:info}} 变更为 {logging:{level:debug}}，When 审计日志写入，Then 记录 field、old_value、new_value

### CONFIG-009 — 配置回滚
**描述**: 支持通过版本 ID 回滚到历史配置版本。

**验收标准**:
- Given 存在版本 ID=version_001 且该版本配置有效，When RollbackConfig("version_001")，Then 配置文件内容替换为历史版本

### CONFIG-010 — 配置深度合并策略
**描述**: 配置合并必须是深度合并，而非顶级字段替换。

**验收标准**:
- Given defaults.yaml 有 gateway.server_addr 和 gateway.tls.enabled，prod.yaml 仅定义 gateway.server_addr，When 合并，Then gateway.tls.enabled 保留 defaults.yaml 的值

---

## 8. 可观测性

### OBS-001 — 日志格式 OTel Log Data Model 兼容
**描述**: 所有日志必须为 JSON 格式，包含 timestamp、level、message、service.name、trace_id、span_id 等必填字段。

**验收标准**:
- Given Logger 配置 serviceName=hotplex-gateway，When 输出日志，Then 包含 "service.name":"hotplex-gateway"
- Given 当前 context 有 trace_id，When 输出日志，Then 包含 trace_id 字段
- Given 当前 context 无 trace context，When 输出日志，Then 不包含 trace_id 和 span_id 字段（而非空字符串）

### OBS-002 — 日志级别规范 DEBUG/INFO/WARN/ERROR/FATAL
**描述**: ERROR 全量记录，正常日志按配置采样率采样。

**验收标准**:
- Given sample_rate=0.1，When 输出 1000 条 INFO 日志，Then 大约保留 100 条（约 10%）
- Given 任意 sample_rate，When 输出 ERROR 日志，Then 全量记录（不受采样率影响）

### OBS-003 — Prometheus 指标命名规范
**描述**: 指标名格式为 `<app_prefix>_<group>_<metric>_<unit_suffix>`，前缀固定 hotplex_。

**验收标准**:
- Given Session 持续时间直方图，When 注册指标，Then 指标名为 hotplex_session_duration_seconds
- Given 指标名不以 hotplex_ 开头，When 注册，Then 返回错误或警告（lint 规则）

### OBS-004 — RED 方法指标 API 层
**描述**: Rate（请求率）、Errors（错误率）、Duration（延迟分布）三个维度。

**验收标准**:
- Given 每次 AEP 请求出错，When 处理完成，Then hotplex_request_errors_total 增加 1
- Given Prometheus 查询 histogram_quantile(0.99, rate(hotplex_request_duration_seconds_bucket[5m]))，Then 返回 P99 延迟

### OBS-005 — USE 方法指标基础设施层
**描述**: Utilization（利用率）、Saturation（饱和度）、Errors（错误）三个维度。

**验收标准**:
- Given Worker 崩溃事件，When 记录，Then hotplex_worker_crashes_total{worker_type="xxx", reason="xxx"} 增加 1
- Given 所有错误，When 记录，Then hotplex_errors_total{component="xxx", error_code="xxx"} 增加 1

### OBS-006 — OTel Span 创建与上下文注入
**描述**: 每个 AEP 事件创建对应 span，trace context 注入到事件 metadata。

**验收标准**:
- Given 收到 AEP event，When 处理，Then 为该事件创建 span 且 span 名称格式为 aep.{kind}
- Given span 创建且 trace context 存在，When 注入 metadata，Then event.Metadata["trace_id"] 等于 spanCtx.TraceID.String()
- Given 跨 Worker 调用，When trace context 传递，Then trace_id 在整个调用链中保持一致

### OBS-007 — Tail Sampling 尾部采样策略
**描述**: ERROR trace 全量保留、>5s 慢 trace 优先保留、正常 trace 1% 采样。

**验收标准**:
- Given status_code=ERROR 的 trace，When OTel Collector tail sampling，Then 100% 保留
- Given latency > 5000ms 的 trace，When OTel Collector tail sampling，Then 优先保留

### OBS-008 — SLO 定义与测量
**描述**: 四个关键 SLO 必须可量化测量：Session 创建成功率 99.5%、P99 < 5s、Worker 可用性 99%、WAF 准确率 > 99.9%。

**验收标准**:
- Given Prometheus 查询 histogram_quantile(0.99, rate(hotplex_event_duration_seconds_bucket[5m]))，When 计算，Then 结果应 < 5s 满足 P99 < 5s SLO

### OBS-009 — 告警规则症状告警而非根因告警
**描述**: 告警基于用户可感知的症状，而非基础设施组件状态。

**验收标准**:
- Given Session 创建失败率 > 1%，When 持续 5 分钟，Then 触发 HighSessionCreationFailureRate 告警
- Given 告警名称为 HighSessionCreationFailureRate，When annotation，Then summary 说明症状而非根因

### OBS-010 — Grafana Dashboard 核心面板
**描述**: Dashboard 包含 Active Sessions、Events Throughput、Latency P50/P95/P99、Worker Resource、Error Rate 五个核心面板。

**验收标准**:
- Given Dashboard 加载，When 查看 Active Sessions 面板，Then 显示 hotplex_sessions_active 当前值
- Given Dashboard 加载，When 查看 Worker Resource Usage 面板，Then 按 worker_type 显示 hotplex_worker_memory_bytes

---

## 9. 资源管理

### RES-001 — Session 所有权 JWT sub claim
**描述**: 每个 Session 的 OwnerID 必须与 JWT sub claim 一致，Ownership 验证必须精确匹配。

**验收标准**:
- Given Session 由 user_001 创建，When ValidateOwnership(sess_abc123, "user_001")，Then 返回 nil（验证通过）
- Given Session 由 user_001 创建，When ValidateOwnership(sess_abc123, "user_002")，Then 返回 ErrSessionOwnershipMismatch

### RES-002 — 权限矩阵 Owner vs Admin 隔离
**描述**: Owner 可 input/terminate/delete 自己的 Session，Admin 可通过 Admin API 强制终止任何 Session。

**验收标准**:
- Given user_001 是 Session A 的 Owner，When user_002 发送 input 事件，Then 返回 ErrSessionOwnershipMismatch
- Given Admin Token 具有 session:kill 权限，When Admin DELETE /admin/sessions/sess_abc123，Then 忽略 Ownership，直接终止

### RES-003 — 输出限制 10MB/20MB/1MB
**描述**: Worker 输出超过限制时必须截断并返回错误。

**验收标准**:
- Given 单行输出数据 10MB+1 字节，When OutputLimiter.Check，Then 返回 ErrLineExceedsLimit 且 type=line
- Given 单轮累计输出 20MB，When OutputLimiter.Check(1 byte)，Then 返回 ErrTurnOutputLimitExceeded 且 type=turn
- Given Envelope 大小超过 1MB，When 编码 AEP JSON，Then 返回错误

### RES-004 — 并发限制 全局 20 / per_user 5
**描述**: WorkerPool 必须强制执行全局最大并发和 per_user 并发上限。

**验收标准**:
- Given 全局活跃 Worker = 20，When Acquire(任意用户)，Then 返回 ErrPoolExhausted
- Given user_001 已有 5 个活跃 Worker，When Acquire("user_001")，Then 返回 ErrUserQuotaExceeded
- Given Release(user_001) 后，When Acquire("user_001")，Then 返回 nil 且 perUserCount 减 1

### RES-005 — 内存限制 RLIMIT_AS
**描述**: Worker 进程通过 setrlimit(RLIMIT_AS) 限制内存使用。

**验收标准**:
- Given MemoryLimitMB=512，When WorkerProcess.Start()，Then syscall.Setrlimit(syscall.RLIMIT_AS) 被调用且 Cur=Max=536870912
- Given setrlimit 调用失败（权限不足），When WorkerProcess.Start()，Then 返回错误且 Worker 进程不启动

### RES-006 — Backpressure 队列容量与丢弃策略
**描述**: InputQueue 容量 100，OutputQueue 容量 50，message.delta 可丢弃。

**验收标准**:
- Given InputQueue 当前 size == 100（满），When HandleInput(input)，Then 立即返回 ErrInputQueueFull（不阻塞）
- Given OutputQueue 当前 size == 50 且 kind=message.done，When HandleOutput(output)，Then 返回 ErrOutputQueueFull（message.done 不可丢弃）

### RES-007 — 错误码完整定义
**描述**: 所有错误必须使用预定义的错误码。

**验收标准**:
- Given Session 不存在，When 任何操作，Then 返回 ErrSessionNotFound 且 error_code=not_found
- Given 全局 Worker 池满，When Acquire，Then 返回 ErrPoolExhausted 且 error_code=pool_exhausted
- Given 单行输出超限，When Check，Then 返回 ErrLineExceedsLimit 且 error_code=line_exceeds_limit

### RES-008 — per_user max_total_memory_mb 限制
**描述**: 每个用户所有 Worker 进程的总内存占用不得超过配置上限。

**验收标准**:
- Given user_001 的 Worker1 占用 800MB，Worker2 占用 600MB，max_total_memory_mb=2048，When Worker3 申请 800MB，Then 请求被拒绝（600+800+800=2200 > 2048）

### RES-009 — Worker 可用性 99% 崩溃率控制
**描述**: Worker 崩溃率必须控制在 1% 以内。

**验收标准**:
- Given Worker 正常退出（无 crash），When hotplex_worker_crashes_total，Then 不增加
- Given Worker 因 OOM 被杀掉，When hotplex_worker_crashes_total，Then 增加 1 且 reason=memory_limit

### RES-010 — Admin 强制终止不受并发限制影响
**描述**: Admin 强制终止 Session 不消耗 Owner 的 per_user 并发配额。

**验收标准**:
- Given Admin 强制终止后，When user_001 创建新 Session，Then 新 Session 可正常创建（配额已释放）

---

## 10. 消息持久化 (EventStore)

### EVT-001 — EventStore Schema 完整捕获所有事件类型
**描述**: Events 表 schema 必须包含所有 AEP 协议定义的事件类型。

**验收标准**:
- Given events 表已创建，When 查询 DESCRIBE events，Then 包含 event_id、event_type、event_version、session_id、user_id、seq、timestamp_ms、payload、metadata 字段
- Given 未知 event_type，When MessageStore.Append，Then 返回错误且不持久化该事件

### EVT-002 — Append-Only 触发器阻止 UPDATE 和 DELETE
**描述**: Events 表通过 SQLite 触发器强制 append-only 语义。

**验收标准**:
- Given events 表有触发器 prevent_event_update，When 执行 UPDATE events SET event_type='hacked' WHERE id=1，Then 返回错误且事件未被修改
- Given events 表有触发器 prevent_event_delete，When 执行 DELETE FROM events WHERE id=1，Then 返回错误且事件未被删除

### EVT-003 — MessageStore 接口定义与编译时验证
**描述**: MessageStore 插件接口精确实现 Append/Query/GetOwner/Close 四个方法。

**验收标准**:
- Given SQLiteMessageStore 实现，When 编译代码，Then var _ MessageStore = (*SQLiteMessageStore)(nil) 不报错
- Given PostgresMessageStore 实现，When 编译代码，Then var _ MessageStore = (*PostgresMessageStore)(nil) 不报错

### EVT-004 — Gateway 集成 EventStore 为可选插件
**描述**: EventStore 作为可选插件注入 SessionManager；当 EventStore 未配置时，Gateway 正常启动。

**验收标准**:
- Given 配置中 message_store.enabled=false，When Gateway 启动，Then Gateway 成功启动且不 panic
- Given EventStore 初始化后，When Append 因数据库错误失败，Then Gateway 记录错误日志但不影响 session 状态流转

### EVT-005 — EventWriter 异步批量写入
**描述**: EventWriter 后台 goroutine 接收事件 channel，按 batchSize 和 flushInterval 配置批量写入数据库。

**验收标准**:
- Given EventWriter 初始化 batchSize=5、flushInterval=100ms，When 连续写入 5 个事件，Then 后台 goroutine 在第 5 个事件到达时触发批量写入
- Given EventWriter 正在接收事件，When Shutdown 被调用，Then 已缓冲事件被全部写入后再退出（优雅关闭）

### EVT-006 — Ownership 验证无循环依赖
**描述**: EventStore.GetOwner 直接查询 sessions 表验证 ownership，不调用 SessionManager。

**验收标准**:
- Given sessions 表不存在指定 session_id，When 调用 GetOwner，Then 返回 ErrSessionNotFound 错误

### EVT-007 — SQLite WAL 模式启用
**描述**: v1.0 SQLite EventStore 必须启用 WAL 模式。

**验收标准**:
- Given SQLite MessageStore 初始化，When 执行 PRAGMA journal_mode=WAL，Then 返回 'wal'，WAL 文件被创建
- Given WAL 模式已启用，When 并发读写同时执行，Then 读操作不阻塞写操作，写操作不阻塞读操作

### EVT-008 — Audit Log 表与哈希链防篡改（P2）
**描述**: 独立 audit_log 表记录管理操作事件，哈希链确保不可篡改。

**验收标准**:
- Given 哈希链被破坏（previous_hash 不匹配），When 插入新审计事件，Then Append 返回 'hash chain broken' 错误且不写入
- Given 配置中 audit_log.enabled=false，When Gateway 启动，Then 不创建 audit_log 表

### EVT-009 — PostgreSQL JSONB 存储（v1.1）
**描述**: v1.1 生产级 EventStore 使用 PostgreSQL JSONB 存储。

**验收标准**:
- Given PostgreSQL MessageStore 已初始化，When 执行 CREATE TABLE events，Then payload 和 metadata 列为 JSONB 类型
- Given pgaudit 已配置，When 执行 INSERT/SELECT events，Then pgaudit.log 包含对应的 READ/WRITE 日志条目

### EVT-010 — MessageStore.Query 时序一致性
**描述**: Query 方法返回指定 session 的事件序列（从 fromSeq 起），按 seq ASC 排序。

**验收标准**:
- Given session-123 有 10 个事件（seq=1..10），When 调用 Query('session-123', 5)，Then 返回 seq=6..10 的事件（5 个事件）
- Given 不存在的 session_id，When 调用 Query，Then 返回 nil 结果和 ErrSessionNotFound 错误

### EVT-011 — EventStore 插件加载与配置解析
**描述**: 配置文件中 message_store 配置被正确解析并实例化对应插件。

**验收标准**:
- Given 配置文件包含 enabled=false，When SessionManager 初始化，Then eventStore 字段为 nil，Gateway 正常启动
- Given 配置缺少必需的 sqlite.path，When 初始化 SQLite MessageStore，Then 返回 'sqlite path is required' 错误

---

## 11. 测试策略

### TEST-001 — 单元测试使用表驱动模式
**描述**: 所有单元测试必须使用 Go 官方推荐的表驱动测试模式，子测试使用 t.Parallel() 并发执行。

**验收标准**:
- Given 任意新的单元测试函数，When 编写测试用例，Then 用例定义在 slice/struct 类型的变量中（tests 表）
- Given 测试执行中遇到错误，When 验证断言失败，Then 使用 t.Errorf 而非 t.Fatal，测试继续执行剩余用例

### TEST-002 — Mock 框架使用 testify/mock
**描述**: 核心功能（os/exec+PGID、WebSocket 连接、Session Pool 并发）不 Mock，必须使用真实实现。

**验收标准**:
- Given PGID 进程隔离逻辑，When 编写测试，Then 使用真实 exec.Command 创建子进程，不使用 mock
- Given WebSocket 消息路由逻辑，When 编写测试，Then 使用 httptest.Server + websocket.Upgrader 创建内嵌 mock server

### TEST-003 — Testcontainers 集成测试
**描述**: 使用 Testcontainers 启动真实 PostgreSQL 容器进行集成测试。

**验收标准**:
- Given go test -tags=integration，When 测试函数执行，Then 自动启动 postgres:16-alpine 容器（无手动准备）
- Given go test -short，When 执行集成测试，Then 跳过（t.Skip），CI 快速检查不会启动容器

### TEST-004 — WebSocket Mock Server 用于集成测试
**描述**: 使用 httptest.Server 配合 gorilla/websocket Upgrader 构建 WebSocket mock server。

**验收标准**:
- Given WebSocket mock server，When 模拟发送 init_ack 消息，Then 客户端成功接收且 Kind='init_ack'
- Given WebSocket mock server，When 模拟心跳超时，Then 客户端检测到心跳超时并触发重连

### TEST-005 — E2E 冒烟测试（Playwright）
**描述**: 使用 Playwright 编写 E2E 冒烟测试，覆盖 WebSocket 连接建立、会话生命周期、认证失败等关键路径。

**验收标准**:
- Given Playwright E2E 测试，When 连接 ws://gateway/gateway 且携带有效 Bearer token，Then WebSocket 握手成功，收到 init_ack
- Given Playwright E2E 测试，When 连接 ws://gateway/gateway 不携带 token，Then 收到 error 且 code=AUTH_REQUIRED，连接被关闭

### TEST-006 — 覆盖率目标 80%+
**描述**: 模块级覆盖率达标：internal/security ≥95%、internal/engine ≥90%、internal/session ≥90%、internal/config ≥85%，整体覆盖率 ≥80%。

**验收标准**:
- Given go test -coverprofile=coverage.out ./...，When 生成覆盖率报告，Then internal/security/ 包覆盖率 ≥95%
- Given go test -coverprofile=coverage.out ./...，When 生成覆盖率报告，Then 整体覆盖率 ≥80%

### TEST-007 — CI/CD 测试分层执行
**描述**: CI 流水线分层执行：go test -short -race → go test -tags=integration → go test -tags=e2e。

**验收标准**:
- Given GitHub Actions CI 配置，When push 到任何分支，Then 执行 go test -short -race ./...，包含竞态检测
- Given golangci-lint 配置，When CI 执行，Then 运行 golangci-lint run 并报告 lint 错误

### TEST-008 — 安全测试：命令注入 + Fuzzing
**描述**: 使用危险输入向量测试命令注入防护；使用 go test -fuzz 进行 EnvelopeValidation 模糊测试。

**验收标准**:
- Given 命令注入测试用例，When 输入包含 '; rm -rf /'，Then WAF/工具拒绝执行，不产生任何文件删除副作用
- Given FuzzEnvelopeValidation 模糊测试，When f.Fuzz 收到任意字节序列，Then ValidateEnvelope 不 panic，始终返回 error 或 nil

### TEST-009 — 性能测试：k6 阈值验证（P2）
**描述**: 使用 k6 进行负载测试，定义 http_req_duration、http_req_failed 等阈值。

**验收标准**:
- Given k6 会话并发测试，When 100 个并发 VU 建立 WebSocket 会话，Then 所有会话成功建立，错误率 0%
- Given k6 脚本配置，When 定义 thresholds.http_req_duration: ['p(95)<500']，Then 95% 请求延迟 <500ms 时测试通过

### TEST-010 — 测试基础设施文档化
**描述**: 测试命名规范、目录结构、基础设施要求有明确文档。

**验收标准**:
- Given 新开发者，When 查看 docs/testing/Testing-Strategy.md，Then 能理解测试金字塔、标签含义、运行命令

### TEST-011 — Benchmark 基准测试（P2）
**描述**: 关键路径（Session 创建、消息路由、EventStore 写入）有 go test -bench 基准测试。

**验收标准**:
- Given BenchmarkConcurrentSessions，When 运行 benchstat 对比，Then 报告每秒建立的会话数（sessions/sec）
- Given 基准测试结果，When PR 提交，Then 在 PR description 中记录关键基准变化（>10% 退化需 review）

---

## AC 汇总统计

| 区域 | P0 | P1 | P2 | 合计 |
|------|----|----|-----|------|
| AEP v1 协议 | 22 | 7 | 1 | 30 |
| WebSocket Gateway | 5 | 3 | 0 | 8 |
| Session 管理 | 6 | 2 | 0 | 8 |
| Worker 抽象与进程管理 | 5 | 6 | 1 | 12 |
| 安全 | 20 | 11 | 3 | 34 |
| Admin API | 9 | 4 | 0 | 13 |
| 配置管理 | 5 | 4 | 1 | 10 |
| 可观测性 | 6 | 4 | 0 | 10 |
| 资源管理 | 7 | 3 | 0 | 10 |
| 消息持久化 (EventStore) | 5 | 5 | 1 | 11 |
| 测试策略 | 5 | 4 | 2 | 11 |
| **合计** | **95** | **57** | **9** | **157** |

**P0 MVP 核心路径**（95 条）：
1. AEP 协议核心（Envelope、Init、Input、State、MessageDelta、Done、Error、PingPong、状态机、竞态防护、Backpressure、时序约束、重连、Worker 生命周期、分层终止、Seq 分配）
2. Gateway 连接管理（握手、重连去重、Bridge 路由、goroutine shutdown）
3. Session SQLite WAL、状态机、GC、mutex 规范
4. Worker SessionConn 接口、PGID 隔离、分层终止、输出限制
5. 安全：JWT ES256/JTI/Claims、命令白名单、双层验证、SafePathJoin、SSRF CIDR/DNSRebind、Env 白名单/Protected/Sensitive、AllowedTools
6. Admin API 核心端点 + 认证链 + 权限矩阵
7. 配置加载/ExpandEnv/验证/SecretProvider/深度合并
8. 可观测性核心：日志格式/级别、Prometheus 命名/RED/USE、OTel Span
9. 资源核心：所有权、权限矩阵、输出限制、并发限制、内存限制、Backpressure、错误码
10. EventStore 核心：Schema/AppendOnly/接口/可选插件/WAL
11. 测试核心：表驱动/mock 规范/E2E 冒烟/覆盖率目标/CI 分层

---

## 维护说明

- AC 状态: `TODO` = 未实现, `IN_PROGRESS` = 实现中, `PASS` = 已通过, `FAIL` = 未通过
- 更新频率: 每次 PR 合并后更新相关 AC 状态
- 变更流程: 新功能 → 提 AC → 评审 → 实现 → 验收 → 关闭
