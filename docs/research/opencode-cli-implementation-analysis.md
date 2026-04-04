# OpenCode CLI 实现对比分析

**日期**: 2026-04-04
**OpenCode 源码**: `~/opencode/packages/opencode`
**Spec 文档**: `docs/specs/Worker-OpenCode-CLI-Spec.md`

---

## 1. CLI 参数对比

### 1.1 已实现参数 ✅

| 参数 | Spec 状态 | 实际状态 | 源码位置 | 说明 |
|------|----------|---------|---------|------|
| `run` | ✅ | ✅ | run.ts:222 | 主命令 |
| `--format json` | ✅ | ✅ | run.ts:263-268 | JSON 输出格式支持 |
| `--session` / `-s` | ✅ | ✅ | run.ts:241-245 | Session ID 参数 |
| `--continue` / `-c` | ✅ | ✅ | run.ts:236-240 | 继续最新会话 |
| `--fork` | ❌ | ✅ | run.ts:246-250 | Fork 会话（Spec 未提及） |
| `--share` | ❌ | ✅ | run.ts:251-254 | 分享会话（Spec 未提及） |
| `--model` / `-m` | ❌ | ✅ | run.ts:255-259 | 模型选择（Spec 未提及） |
| `--agent` | ❌ | ✅ | run.ts:260-263 | Agent 选择（Spec 未提及） |
| `--file` / `-f` | ❌ | ✅ | run.ts:269-275 | 文件附件（Spec 未提及） |
| `--title` | ❌ | ✅ | run.ts:276-279 | 会话标题（Spec 未提及） |
| `--attach` | ❌ | ✅ | run.ts:280-283 | 连接远程服务器（Spec 未提及） |
| `--password` / `-p` | ❌ | ✅ | run.ts:284-288 | Basic Auth 密码（Spec 未提及） |
| `--dir` | ❌ | ✅ | run.ts:289-292 | 工作目录（Spec 未提及） |
| `--port` | ❌ | ✅ | run.ts:293-296 | 服务器端口（Spec 未提及） |
| `--variant` | ❌ | ✅ | run.ts:297-300 | 模型变体（Spec 未提及） |
| `--thinking` | ❌ | ✅ | run.ts:301-305 | 显示思考块（Spec 未提及） |

### 1.2 Spec 中定义但未找到实现 ⚠️

| 参数 | Spec 章节 | 优先级 | 说明 |
|------|----------|--------|------|
| `--resume` | 2.2 | P0 | Spec 标记为"不支持"，但 CLI 代码中未找到 |
| `--allowed-tools` | 2.3 | P0 | **关键差异**：Spec 声称已实现，但代码中未找到 |
| `--disallowed-tools` | 2.3 | P1 | Spec 声称已实现，但代码中未找到 |
| `--dangerously-skip-permissions` | 2.3 | P1 | 需验证 |
| `--permission-mode` | 2.3 | P1 | 需验证 |
| `--system-prompt` | 2.4 | P1 | 需验证 |
| `--append-system-prompt` | 2.4 | P1 | 需验证 |
| `--mcp-config` | 2.5 | P1 | 需验证 |
| `--strict-mcp-config` | 2.5 | P1 | 需验证 |
| `--bare` | 2.6 | P2 | 需验证 |
| `--add-dir` | 2.6 | P2 | 需验证 |
| `--max-budget-usd` | 2.6 | P3 | 需验证 |
| `--json-schema` | 2.6 | P3 | 需验证 |
| `--include-hook-events` | 2.6 | P3 | 需验证 |
| `--include-partial-messages` | 2.6 | P3 | 需验证 |
| `--max-turns` | 2.6 | P2 | 需验证 |

---

## 2. 输出格式对比

### 2.1 JSON 输出结构

**实际实现**（run.ts:433-439）:
```json
{
  "type": "<event_type>",
  "timestamp": 1712234567890,
  "sessionID": "<session_id>",
  ...data
}
```

**Spec 定义**（Worker-OpenCode-CLI-Spec.md:4.1）:
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

**差异**:
- ❌ **无 AEP envelope**：实际输出不包含 `version`, `id`, `seq` 字段
- ❌ **字段命名不同**：`sessionID` vs `session_id`
- ❌ **事件结构不同**：直接输出 `{ type, ...data }` 而非 `{ event: { type, data } }`

### 2.2 事件类型对比

| 实际事件类型 | Spec 事件类型 | 映射关系 | 说明 |
|-------------|--------------|---------|------|
| `step_start` | `step_start` | ✅ 一致 | 步骤开始 |
| `step_finish` | `step_end` | ⚠️ 命名不同 | 步骤结束 |
| `text` | `message` | ⚠️ 结构不同 | 完整文本消息 |
| - | `message.part.delta` | ❌ 未实现 | 流式增量 |
| `tool_use` | `tool_use` | ✅ 一致 | 工具调用 |
| - | `tool_result` | ❌ 未实现 | 工具结果（作为 tool_use 的一部分） |
| `error` | `error` | ✅ 一致 | 错误 |
| `reasoning` | - | ➕ 额外 | 思考过程（Spec 未提及） |
| - | `system` | ❌ 未找到 | 系统消息 |
| - | `session_created` | ❌ 未找到 | 会话创建事件 |

### 2.3 Part 类型系统（源码分析）

**源码位置**: `session/message-v2.ts`

实际支持的 Part 类型：
- `snapshot` - 快照
- `patch` - 补丁
- `text` - 文本 ✅
- `reasoning` - 推理/思考 ✅
- `file` - 文件
- `agent` - Agent 信息
- `compaction` - 压缩
- `subtask` - 子任务
- `retry` - 重试
- `step-start` - 步骤开始 ✅
- `step-finish` - 步骤结束 ✅
- `tool` - 工具调用 ✅

---

## 3. 环境变量对比

### 3.1 Spec 定义的环境变量

| 变量 | Spec 状态 | 实际状态 | 说明 |
|------|----------|---------|------|
| `OPENAI_API_KEY` | ✅ 白名单 | ❓ 未验证 | 需检查实际使用 |
| `OPENAI_BASE_URL` | ✅ 白名单 | ❓ 未验证 | 需检查实际使用 |
| `OPENCODE_API_KEY` | ✅ 白名单 | ❓ 未验证 | 需检查实际使用 |
| `OPENCODE_BASE_URL` | ✅ 白名单 | ❓ 未验证 | 需检查实际使用 |
| `HOTPLEX_SESSION_ID` | ✅ 注入 | ❓ 未验证 | 需检查实际使用 |
| `HOTPLEX_WORKER_TYPE` | ✅ 注入 | ❓ 未验证 | 需检查实际使用 |

### 3.2 实际使用的环境变量（源码分析）

**在 run.ts 中找到**:
- `OPENCODE_SERVER_PASSWORD` (run.ts:658)
- `OPENCODE_SERVER_USERNAME` (run.ts:659)
- `OPENCODE_AUTO_SHARE` (run.ts:399)

**需进一步调查**:
- Provider 相关环境变量
- Permission 相关环境变量

---

## 4. Session 管理对比

### 4.1 Session ID 提取

**Spec 章节**: 7.1
**Spec 描述**: 从 `step_start` 事件中提取 `session_id`

**实际实现** (run.ts:622-627):
```typescript
const sessionID = await session(sdk)
// session() 函数创建或复用会话
```

**差异**:
- ✅ Session ID 在第一步创建
- ❓ 是否在 `step_start` 事件中返回需验证

### 4.2 Resume 支持

**Spec 章节**: 7.3
**Spec 状态**: ❌ 不支持

**实际实现**:
- ✅ 支持 `--continue` 继续最新会话
- ✅ 支持 `--session <id>` 指定会话
- ✅ 支持 `--fork` fork 会话

**结论**: Spec 需更新，Resume 功能已实现

---

## 5. 权限管理对比

### 5.1 实际实现（run.ts:357-373）

```typescript
const rules: Permission.Ruleset = [
  {
    permission: "question",
    action: "deny",
    pattern: "*",
  },
  {
    permission: "plan_enter",
    action: "deny",
    pattern: "*",
  },
  {
    permission: "plan_exit",
    action: "deny",
    pattern: "*",
  },
]
```

**自动拒绝**:
- `question` 权限
- `plan_enter` 权限
- `plan_exit` 权限

**权限请求处理** (run.ts:544-556):
- 监听 `permission.asked` 事件
- 自动拒绝所有权限请求
- 在非交互模式下不可用

### 5.2 Spec vs 实际

| 特性 | Spec 状态 | 实际状态 | 说明 |
|------|----------|---------|------|
| `--dangerously-skip-permissions` | ⚠️ 需验证 | ❓ 未找到 | 可能通过 Permission API 实现 |
| `--permission-mode` | ⚠️ 需验证 | ❓ 未找到 | 可能通过 Permission API 实现 |

---

## 6. 关键发现

### 6.1 架构差异

**Spec 假设**:
- OpenCode CLI 直接输出 AEP v1 格式
- Worker Adapter 需要做事件映射

**实际情况**:
- OpenCode CLI 输出自定义 JSON 格式
- 需要完整的格式转换层
- 事件系统更复杂（Part 类型系统）

### 6.2 工具参数差异

**Spec 声称** (2.3):
- ✅ `--allowed-tools` 已实现（worker.go:74-76）
- ✅ `--disallowed-tools` 已实现（worker.go:78-80）

**实际发现**:
- ❌ CLI 层面**未找到**这些参数
- ❓ 可能通过 Permission API 实现
- ❓ 可能是 Worker Adapter 层面的实现

**需要验证**:
1. Worker Adapter 代码 (`internal/worker/opencodecli/worker.go`)
2. Permission 系统
3. Tool Policy 系统

### 6.3 MCP 支持

**Spec 章节**: 2.5
**Spec 状态**: ⚠️ 需验证

**实际发现**:
- ❌ CLI `run` 命令中未找到 `--mcp-config` 参数
- ❓ 可能在其他命令或配置文件中

---

## 7. 待验证项清单

### 7.1 高优先级 (P0)

- [ ] **验证 `--allowed-tools` 实现**
  - 检查 Worker Adapter 代码
  - 检查 Permission 系统
  - 运行实际测试

- [ ] **验证事件映射**
  - 运行实际 CLI 命令
  - 捕获完整输出
  - 对比 Spec 定义

- [ ] **验证 Session ID 提取**
  - 测试 `step_start` 事件
  - 确认 sessionID 字段

### 7.2 中优先级 (P1)

- [ ] **验证环境变量白名单**
  - 检查 Provider 实现
  - 检查环境变量使用

- [ ] **验证 MCP 配置**
  - 检查其他 CLI 命令
  - 检查配置文件

- [ ] **验证 Resume 功能**
  - 测试 `--continue`
  - 测试 `--session`

### 7.3 低优先级 (P2-P3)

- [ ] 验证扩展参数（`--bare`, `--add-dir` 等）
- [ ] 验证流式增量输出
- [ ] 验证系统消息处理

---

## 8. Spec 更新建议

### 8.1 立即更新

1. **CLI 参数章节 (2)**
   - 添加实际实现的参数
   - 标记未实现的参数
   - 更新实现状态

2. **输出格式章节 (5)**
   - 更新实际输出格式
   - 添加格式转换需求
   - 更新事件类型映射

3. **Session 管理 (7)**
   - 更新 Resume 支持状态
   - 添加 `--continue` 和 `--session` 参数说明

### 8.2 需进一步调研

1. **工具参数实现**
   - 验证 Worker Adapter 代码
   - 确认实现路径

2. **环境变量**
   - 完整的环境变量审计

3. **MCP 支持**
   - 检查其他入口点

---

## 9. 下一步行动

1. **运行实际测试**
   ```bash
   cd ~/opencode
   bun run opencode run --format json 'test'
   ```

2. **更新 Spec 文档**
   - 标记所有差异
   - 更新实现状态
   - 添加待验证标记

3. **创建 Worker Adapter 验证脚本**
   - 测试 Worker 代码中的参数处理
   - 验证事件映射实现

4. **与 OpenCode 团队沟通**
   - 确认工具参数实现
   - 确认 MCP 配置路径
