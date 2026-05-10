# HotPlex Worker Gateway — AEP 协议资产 (Protocol Core Asset)

> **版本**：aep/v1
> **定义**：AEP (Agent Event Protocol) 是 HotPlex 核心的资产协议，定义了客户端与 AI Agent 网关之间全双工通信的事件模型。

---

## 1. 设计哲学 (Design Philosophy)

AEP 协议遵循以下核心设计准则：
- **流式优先 (Streaming-first)**：原生支持 Token 级的流式输出，最大化降低首包延迟。
- **双向对等 (Bidirectional)**：统一的 Envelope（信封）结构，不区分上行和下行，简化编解码逻辑。
- **结构化交互 (Structured Interaction)**：将工具调用、权限申请、引导式问答等复杂交互抽象为标准事件。
- **弱 Schema 兼容 (Soft Schema)**：通过 `raw` 事件支持底层引擎特有数据的透明传输。

---

## 2. 信封结构 (The Envelope)

所有 AEP 消息均封装在统一的 `Envelope` 中。

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `version` | string | 固定为 `aep/v1` |
| `id` | string | 事件唯一 ID (UUID v4)，用于错误追溯 |
| `seq` | integer | 会话内严格递增的序列号（仅分配给实际下发的事件） |
| `priority` | string | `control` (插队) 或 `data` (排队) |
| `session_id` | string | 关联的逻辑会话 ID |
| `timestamp` | integer | 毫秒级 Unix 时间戳，用于精确排序 |
| `event` | object | 具体事件载荷，包含 `type` 和 `data` |

---

## 3. 事件分类法 (Event Taxonomy)

AEP 事件分为四类：

### 3.1 生命周期类
- **init / init_ack**：连接握手与能力协商。支持从 `session_id` 恢复（Resume）历史会话。
- **state**：会话状态变更通知（Created -> Running -> Idle -> Terminated）。
- **done**：Turn 执行完成，携带详尽的 Token 统计、成本分析及模型信息。

### 3.2 消息流转类
- **input**：客户端发送的用户指令。
- **message.start / .delta / .end**：标准的三段式流式输出。
- **message**：完整消息块（非流式或历史回溯）。
- **reasoning**：Agent 的内部思考过程，独立于输出内容。

### 3.3 工具与任务类
- **tool_call**：Worker 发起的工具调用通知。
- **tool_result**：工具执行结果的反馈。
- **step**：复杂任务的执行阶段标记。

### 3.4 交互与控制类
- **control**：核心控制信号（Terminate, Reset, GC, Reconnect, Throttle）。
- **permission_request / response**：结构化的权限确认交互。
- **error**：标准化的错误码体系（20+ 种错误码）。

---

## 4. 关键流程控制

### 4.1 优先级与背压 (Priority & Backpressure)
- **控制消息 (`priority: control`)**：如重连指令、错误通知。它们跳过发送队列，优先下发，确保系统控制力。
- **数据消息 (`priority: data`)**：如 `message.delta`。在网络拥塞时可能被智能丢弃（不消耗 seq），系统通过 `done.dropped` 标记通知客户端进行最终数据对账。

### 4.2 状态同步与幂等
- 每个 `init_ack` 和 `state` 变更都包含当前 Worker 的健康状态和能力声明，确保客户端与服务端在协议语义上始终保持一致。
- 所有的 `control` 指令设计为幂等，防止重复操作导致的系统状态混乱。
