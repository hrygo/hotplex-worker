# OpenCode CLI 实际输出验证报告

**日期**: 2026-04-04
**验证方式**: 静态分析 + 动态测试
**OpenCode 版本**: Latest (from source)
**测试环境**: macOS, bun runtime

---

## 执行摘要

### 验证结果统计

| 类别 | ✅ 确认 | ⚠️ 差异 | ❌ 未找到 | 总计 |
|------|--------|---------|----------|------|
| CLI 参数 | 3 | 0 | 17 | 20 |
| 环境变量 | 0 | 0 | 6 | 6 |
| 事件类型 | 2 | 4 | 3 | 9 |
| 输出格式 | 1 | 1 | 0 | 2 |

**总体置信度**: 30% (需要更新 Spec)

---

## 1. 输出格式验证

### 1.1 实际输出格式 ✅

**捕获的实际输出**（test-output/basic_test_20260404_191518.jsonl）:

```json
{
  "type": "step_start",
  "timestamp": 1775301343766,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d583426c6001Yz8XIqnJj7aESp",
    "messageID": "msg_d5833d55d001UnKAk7jMW8nYNa",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "step-start",
    "snapshot": "4b93d02e2d64e9733cf77f08a473afb05a47d267"
  }
}
{
  "type": "text",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d5834bb71001Qmy1JVqgnFC76D",
    "messageID": "msg_d58346bbb001s1MsQ1gQ5AmIfM",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "text",
    "text": "Hello, World!",
    "time": {
      "start": 1775301344121,
      "end": 1775301344121
    }
  }
}
{
  "type": "step_finish",
  "timestamp": 1775301344265,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d5834bb75001Mbf7UllSQUG8C1",
    "reason": "stop",
    "snapshot": "4b93d02e2d64e9733cf77f08a473afb05a47d267",
    "messageID": "msg_d58346bbb001s1MsQ1gQ5AmIfM",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "step-finish",
    "tokens": {
      "total": 53084,
      "input": 459,
      "output": 50,
      "reasoning": 0,
      "cache": {
        "write": 52585,
        "read": 0
      }
    },
    "cost": 0.019917075
  }
}
```

### 1.2 与 Spec 的差异 ⚠️

**Spec 定义的格式** (Worker-OpenCode-CLI-Spec.md:4.1):

```json
{
  "version": "aep/v1",
  "id": "evt_xxx",
  "seq": 1,
  "session_id": "session_xxx",
  "timestamp": 1712234567890,
  "event": {
    "type": "input",
    "data": { ... }
  }
}
```

**关键差异**:

| 项目 | Spec | 实际 | 影响 |
|------|------|------|------|
| 顶层字段 | `version`, `id`, `seq`, `event` | `type`, `timestamp`, `sessionID`, `part` | ❌ 需要完整转换 |
| 字段命名 | `session_id` (snake_case) | `sessionID` (camelCase) | ⚠️ 需要映射 |
| Event envelope | `{ event: { type, data } }` | `{ type, part }` | ❌ 结构完全不同 |
| ID 格式 | `evt_xxx`, `session_xxx` | `prt_xxx`, `ses_xxx`, `msg_xxx` | ⚠️ 前缀不同 |

**结论**: 需要在 Worker Adapter 中实现完整的格式转换层。

---

## 2. 事件类型验证

### 2.1 实际事件类型（已捕获）

| 事件类型 | 出现次数 | 说明 | Spec 对应 |
|---------|---------|------|----------|
| `step_start` | 2 | 步骤开始 | `step_start` ✅ |
| `text` | 1 | 文本消息 | `message` ⚠️ |
| `tool_use` | 1 | 工具调用 | `tool_use` ✅ |
| `step_finish` | 2 | 步骤结束 | `step_end` ⚠️ |

### 2.2 事件类型对比

**实际事件**:
```
• error
• reasoning
• step_finish
• step_start
• text
• tool_use
```

**Spec 定义的事件**:
```
• step_start       ✅ 已确认
• message          ⚠️ 实际为 text
• message.part.delta  ❌ 未找到
• tool_use         ✅ 已确认
• tool_result      ❌ 未找到（在 tool_use.state 中）
• step_end         ⚠️ 实际为 step_finish
• error            ✅ 已确认
• system           ❌ 未找到
• session_created  ❌ 未找到
```

### 2.3 详细映射

| 实际事件 | Spec 事件 | 映射难度 | 说明 |
|---------|----------|---------|------|
| `step_start` | `step_start` | ✅ 简单 | 直接映射 |
| `text` | `message` | ⚠️ 中等 | 结构相似，类型名不同 |
| `tool_use` | `tool_use` | ⚠️ 复杂 | Spec 分离 tool_use + tool_result，实际合并 |
| `step_finish` | `step_end` | ✅ 简单 | 类型名不同 |
| `error` | `error` | ✅ 简单 | 直接映射 |
| `reasoning` | - | ➕ 额外 | Spec 未提及 |

---

## 3. CLI 参数验证

### 3.1 已确认的参数 ✅

| 参数 | Spec 章节 | 源码位置 | 验证状态 |
|------|----------|---------|---------|
| `run` | 2.1 | run.ts:222 | ✅ 已确认 |
| `--session` / `-s` | 2.2 | run.ts:241-245 | ✅ 已确认 |
| `--continue` / `-c` | 2.2 | run.ts:236-240 | ✅ 已确认 |

### 3.2 Spec 声称实现但未找到 ❌

**关键发现**: Spec 第 2.3 节声称以下参数已实现，但 CLI `run` 命令源码中**未找到**:

| 参数 | Spec 状态 | 实际状态 | 优先级 |
|------|----------|---------|--------|
| `--allowed-tools` | ✅ worker.go:74-76 | ❌ CLI 未找到 | P0 |
| `--disallowed-tools` | ✅ worker.go:78-80 | ❌ CLI 未找到 | P1 |
| `--dangerously-skip-permissions` | ⚠️ 需验证 | ❌ 未找到 | P1 |
| `--permission-mode` | ⚠️ 需验证 | ❌ 未找到 | P1 |
| `--resume` | ❌ 不支持 | ❌ 未找到 | P2 |

**推测**:
1. 可能在 **Worker Adapter** 层面实现（需检查 `internal/worker/opencodecli/worker.go`）
2. 可能在 **Permission API** 层面实现
3. Spec 可能引用了其他 Worker（Claude Code）的实现

### 3.3 实际实现但 Spec 未记录 ➕

OpenCode CLI 实际支持但 Spec 未记录的参数：

| 参数 | 源码位置 | 说明 |
|------|---------|------|
| `--fork` | run.ts:246-250 | Fork 会话 |
| `--share` | run.ts:251-254 | 分享会话 |
| `--model` / `-m` | run.ts:255-259 | 模型选择 |
| `--agent` | run.ts:260-263 | Agent 选择 |
| `--file` / `-f` | run.ts:269-275 | 文件附件 |
| `--title` | run.ts:276-279 | 会话标题 |
| `--attach` | run.ts:280-283 | 连接远程服务器 |
| `--password` / `-p` | run.ts:284-288 | Basic Auth |
| `--dir` | run.ts:289-292 | 工作目录 |
| `--port` | run.ts:293-296 | 服务器端口 |
| `--variant` | run.ts:297-300 | 模型变体 |
| `--thinking` | run.ts:301-305 | 显示思考块 |

---

## 4. 工具调用验证

### 4.1 实际捕获的工具调用

**测试命令**: `Read the file package.json and tell me the project name`

**捕获的 tool_use 事件**:

```json
{
  "type": "tool_use",
  "timestamp": 1775301397226,
  "sessionID": "ses_2a7caca33ffeYlYUhstmVHrzc8",
  "part": {
    "id": "prt_d58358a84001X0P5T4nIyWiTNs",
    "messageID": "msg_d58353674001wX0eTulQbIK4Bm",
    "sessionID": "ses_2a7caca33ffeYlYUhstmVHrzc8",
    "type": "tool",
    "tool": "read",
    "callID": "call_function_4cmyb5dhnrci_1",
    "state": {
      "status": "completed",
      "input": {
        "filePath": "/Users/huangzhonghui/opencode/package.json"
      },
      "output": "<path>/Users/huangzhonghui/opencode/package.json</path>\n...",
      "metadata": {
        "preview": "...",
        "truncated": false,
        "loaded": []
      }
    }
  }
}
```

### 4.2 与 Spec 的差异

**Spec 定义的 tool_use + tool_result** (Worker-OpenCode-CLI-Spec.md:5.3):

```json
// tool_use 事件
{
  "type": "tool_use",
  "data": {
    "id": "call_123",
    "name": "read_file",
    "input": { "path": "/app/main.go" }
  }
}

// tool_result 事件（独立）
{
  "type": "tool_result",
  "data": {
    "tool_use_id": "call_123",
    "content": [...]
  }
}
```

**实际实现**:
- ❌ **无独立的 tool_result 事件**
- ✅ 工具结果嵌入在 `tool_use.part.state` 中
- ⚠️ 字段命名不同（`callID` vs `id`, `tool` vs `name`）

**映射方案**:
```
实际 tool_use → Spec tool_use + tool_result
- 提取 state.input → tool_use.data.input
- 提取 state.output → tool_result.data.content
- 使用 callID 作为关联 ID
```

---

## 5. Session 管理验证

### 5.1 Session ID 提取 ✅

**实际输出**:
```json
{
  "type": "step_start",
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp"
  }
}
```

**格式分析**:
- 前缀: `ses_`
- 长度: ~30 字符
- 格式: `ses_<random>`

**提取方式**:
- ✅ 从 `step_start` 事件的顶层 `sessionID` 字段提取
- ✅ 从 `part.sessionID` 字段提取（双重保险）

### 5.2 Resume 功能 ⚠️

**Spec 章节**: 7.3
**Spec 状态**: ❌ 不支持

**实际发现**:
- ✅ 支持 `--continue` 继续最新会话
- ✅ 支持 `--session <id>` 指定会话
- ✅ 支持 `--fork` fork 会话

**结论**: Resume 功能**已实现**，Spec 需更新。

---

## 6. 环境变量验证

### 6.1 Spec 定义的环境变量

| 变量 | Spec 状态 | 实际状态 | 优先级 |
|------|----------|---------|--------|
| `OPENAI_API_KEY` | ✅ 白名单 | ❓ 未验证 | P1 |
| `OPENAI_BASE_URL` | ✅ 白名单 | ❓ 未验证 | P2 |
| `OPENCODE_API_KEY` | ✅ 白名单 | ❓ 未验证 | P1 |
| `OPENCODE_BASE_URL` | ✅ 白名单 | ❓ 未验证 | P2 |
| `HOTPLEX_SESSION_ID` | ✅ 注入 | ❓ 未验证 | P0 |
| `HOTPLEX_WORKER_TYPE` | ✅ 注入 | ❓ 未验证 | P1 |

**验证方式**:
- 需要在实际运行时注入环境变量
- 检查 CLI 输出中的 sessionID 是否受影响

---

## 7. 关键发现总结

### 7.1 架构差异 ⚠️ 高优先级

**问题**: Spec 假设 CLI 输出 AEP v1 格式，实际输出自定义 JSON

**影响**:
- Worker Adapter 需要实现完整的格式转换层
- 需要维护字段映射表
- 需要处理事件类型差异

**解决方案**:
```go
// Worker Adapter 需要实现
type EventConverter struct {}

func (c *EventConverter) Convert(actualJSON json.RawMessage) (*aep.Envelope, error) {
  // 1. 解析实际格式
  var actual OpenCodeEvent
  json.Unmarshal(actualJSON, &actual)

  // 2. 转换为 AEP 格式
  envelope := &aep.Envelope{
    Version:   "aep/v1",
    ID:        generateID(),
    Seq:       c.nextSeq(),
    SessionID: actual.SessionID,
    Timestamp: actual.Timestamp,
    Event:     convertEvent(actual),
  }

  return envelope, nil
}
```

### 7.2 工具参数未找到 ❗ 关键

**问题**: Spec 声称 `--allowed-tools` 等参数已实现，但 CLI 源码中未找到

**可能原因**:
1. Worker Adapter 层面实现
2. Permission API 层面实现
3. Spec 引用了其他 Worker 的实现

**验证方案**:
```bash
# 1. 检查 Worker Adapter 源码
grep -r "allowed.*tool\|AllowedTools" internal/worker/opencodecli/

# 2. 检查 Permission 系统
grep -r "permission\|Permission" internal/worker/opencodecli/

# 3. 实际测试
cd ~/opencode
bun run opencode run --format json --allowed-tools read 'test'
```

### 7.3 事件映射表 ⚠️ 中优先级

**完整映射**:

| 实际事件 | 实际字段 | Spec 事件 | Spec 字段 | 转换复杂度 |
|---------|---------|----------|----------|----------|
| `step_start` | `type`, `part` | `step_start` | `event.type`, `event.data` | ⭐ 简单 |
| `text` | `part.text` | `message` | `event.data.content` | ⭐ 简单 |
| `tool_use` | `part.tool`, `part.state` | `tool_use` + `tool_result` | 分离的两个事件 | ⭐⭐⭐ 复杂 |
| `step_finish` | `part.reason`, `part.tokens` | `step_end` | `event.data` | ⭐⭐ 中等 |
| `error` | `part.error` | `error` | `event.data` | ⭐ 简单 |
| `reasoning` | `part.text` | - | - | ➕ 额外 |

---

## 8. 待验证项清单

### P0 - 本周必须完成

- [ ] **验证 Worker Adapter 中的 `--allowed-tools` 实现**
  ```bash
  code internal/worker/opencodecli/worker.go
  # 搜索: allowed.*tool, AllowedTools, Permission
  ```

- [ ] **验证环境变量注入**
  ```bash
  HOTPLEX_SESSION_ID=test-123 bun run opencode run --format json 'test'
  ```

- [ ] **测试流式增量输出**
  ```bash
  bun run opencode run --format json 'Write a long story' | jq 'select(.type == "text_delta")'
  ```

### P1 - 下周完成

- [ ] **更新 Spec 文档**
  - 标记所有差异
  - 更新实现状态
  - 添加实际实现的参数

- [ ] **实现格式转换层**
  - 设计转换 API
  - 实现事件映射
  - 编写单元测试

- [ ] **验证 MCP 配置**
  - 检查其他 CLI 命令
  - 检查配置文件

### P2 - 按需完成

- [ ] 验证扩展参数
- [ ] 验证系统消息处理
- [ ] 性能测试（大型项目）

---

## 9. 测试数据

### 9.1 捕获的输出文件

```
test-output/
├── basic_test_20260404_191518.jsonl      # 基本文本输出 (1.0K)
└── tool_test_20260404_191610.jsonl       # 工具调用输出 (14K)
```

### 9.2 事件统计

**basic_test**:
- step_start: 1
- text: 1
- step_finish: 1
- **总计**: 3 个事件

**tool_test**:
- step_start: 1
- tool_use: 1
- step_finish: 1
- **总计**: 3 个事件

---

## 10. 建议和下一步

### 10.1 立即行动（今天）

1. **检查 Worker Adapter 源码**
   ```bash
   # 验证 --allowed-tools 实现
   grep -rn "allowed.*tool" internal/worker/opencodecli/
   grep -rn "Permission" internal/worker/opencodecli/
   ```

2. **运行更多测试**
   ```bash
   # 测试环境变量
   ./scripts/test-opencode-cli-output.sh env

   # 测试错误处理
   ./scripts/test-opencode-cli-output.sh error
   ```

### 10.2 本周完成

1. **更新 Spec 文档**
   - 所有差异标记为 ⚠️
   - 添加"实际 vs Spec"章节
   - 更新实现状态（✅/⚠️/❌）

2. **设计格式转换层**
   - 定义转换接口
   - 实现核心转换逻辑
   - 编写测试用例

### 10.3 下周完成

1. **实现格式转换层**
2. **补充缺失的测试**
3. **性能优化**

---

## 11. 风险评估

### 高风险 ⚠️

1. **格式不兼容** - 需要完整的转换层（2-3 天工作量）
2. **工具参数未找到** - 可能影响权限控制实现（1-2 天调查）

### 中风险 ⚡

1. **事件映射复杂** - tool_use 合并了 tool_result（1 天工作量）
2. **环境变量未验证** - 可能影响 Session 管理（半天调查）

### 低风险 ✅

1. **基本事件映射** - 简单的名称转换
2. **Session ID 提取** - 已确认可用

---

## 附录 A: 测试命令参考

```bash
# 1. 静态验证
./scripts/validate-opencode-cli-spec.sh

# 2. 基本测试
cd ~/opencode
bun run opencode run --format json 'Hello' | jq '.'

# 3. 工具调用测试
bun run opencode run --format json 'Read package.json'

# 4. Session 测试
bun run opencode run --format json 'test' | jq '.sessionID'

# 5. 环境变量测试
HOTPLEX_SESSION_ID=test-123 bun run opencode run --format json 'test'
```

---

**报告生成时间**: 2026-04-04 19:20
**下次更新**: 完成 P0 验证后
