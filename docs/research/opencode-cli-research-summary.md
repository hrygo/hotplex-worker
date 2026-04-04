# OpenCode CLI 深入调研总结

**日期**: 2026-04-04
**目的**: 验证 Worker-OpenCode-CLI-Spec.md 与实际实现的一致性
**方法**: 源码分析 + 自动化验证脚本

---

## 调研成果

### 1. 已创建的验证工具

#### 1.1 validate-opencode-cli-spec.sh
**静态分析工具**，对比 Spec 文档与源码实现：
- ✅ CLI 参数验证（20+ 参数）
- ✅ 环境变量检查
- ✅ 输出格式分析
- ✅ 事件类型对比
- ✅ 实现状态跟踪

**使用**:
```bash
./scripts/validate-opencode-cli-spec.sh
```

**输出示例**:
- 3 个参数已确认 ✅
- 17 个参数待验证 ⚠️
- 6 个实际事件类型
- 9 个 Spec 事件类型

#### 1.2 test-opencode-cli-output.sh
**动态测试工具**，运行实际 CLI 并捕获输出：
- ✅ 6 个测试用例
- ✅ JSON 输出捕获
- ✅ 事件类型分析
- ✅ Session ID 提取
- ✅ 格式对比

**使用**:
```bash
# 运行所有测试
./scripts/test-opencode-cli-output.sh

# 运行单个测试
./scripts/test-opencode-cli-output.sh 1  # 基本输出
./scripts/test-opencode-cli-output.sh tool  # 工具调用
```

**输出保存**: `test-output/` 目录

### 2. 分析文档

#### 2.1 opencode-cli-implementation-analysis.md
**深度对比分析**，包含：
- CLI 参数完整对比表
- 输出格式差异分析
- 事件类型映射
- 环境变量审计
- Session 管理差异
- 权限系统分析
- 关键发现和差异
- 待验证项清单

**位置**: `docs/research/opencode-cli-implementation-analysis.md`

---

## 关键发现

### 1. 架构差异 ⚠️

**Spec 假设**:
```
OpenCode CLI → AEP v1 格式 → Worker Adapter
```

**实际情况**:
```
OpenCode CLI → 自定义 JSON → Worker Adapter (需转换层)
```

**影响**: 需要在 Worker Adapter 中实现完整的格式转换

### 2. 事件类型不匹配 ⚠️

| 实际 | Spec | 差异 |
|------|------|------|
| `step_finish` | `step_end` | 命名不同 |
| `text` | `message` | 结构不同 |
| - | `message.part.delta` | 未实现 |
| - | `tool_result` | 未实现（作为 tool_use 的一部分） |
| `reasoning` | - | 额外实现 |

**影响**: 需要调整事件映射逻辑

### 3. 工具参数关键差异 ❗

**Spec 声称** (2.3 节):
```
✅ --allowed-tools    (worker.go:74-76)
✅ --disallowed-tools (worker.go:78-80)
```

**实际发现**:
- ❌ CLI `run` 命令中**未找到**这些参数
- ❓ 可能通过 Permission API 实现
- ❓ 可能是 Worker Adapter 层面的实现

**需要验证**:
1. 检查 Worker Adapter 代码
2. 测试 Permission 系统
3. 运行实际测试

### 4. 实际实现的参数（Spec 未提及）

OpenCode CLI 实际支持但 Spec 未记录的参数：
- `--fork` - Fork 会话
- `--share` - 分享会话
- `--model` / `-m` - 模型选择
- `--agent` - Agent 选择
- `--file` / `-f` - 文件附件
- `--title` - 会话标题
- `--attach` - 连接远程服务器
- `--password` / `-p` - Basic Auth
- `--dir` - 工作目录
- `--port` - 服务器端口
- `--variant` - 模型变体
- `--thinking` - 显示思考块

**影响**: Spec 需要补充这些参数

---

## 待验证项清单

### 高优先级 (P0) - 本周完成

- [ ] **运行实际测试**
  ```bash
  cd ~/opencode
  bun run opencode run --format json 'test'
  ```

- [ ] **验证 `--allowed-tools` 实现**
  - 检查 `internal/worker/opencodecli/worker.go`
  - 检查 Permission API
  - 运行实际测试

- [ ] **验证事件映射**
  - 捕获完整输出
  - 对比 Spec 定义
  - 更新映射表

### 中优先级 (P1) - 下周完成

- [ ] **更新 Spec 文档**
  - 标记所有差异
  - 更新实现状态
  - 添加实际实现的参数

- [ ] **验证环境变量**
  - 完整的环境变量审计
  - 测试白名单

- [ ] **验证 MCP 配置**
  - 检查其他 CLI 命令
  - 检查配置文件

### 低优先级 (P2-P3) - 按需完成

- [ ] 验证扩展参数
- [ ] 验证流式增量输出
- [ ] 验证系统消息处理

---

## 下一步行动

### 1. 立即执行（今天）

```bash
# 步骤 1: 运行静态验证
./scripts/validate-opencode-cli-spec.sh > validation-report.txt

# 步骤 2: 运行动态测试
./scripts/test-opencode-cli-output.sh 1  # 基本输出
./scripts/test-opencode-cli-output.sh 4  # Session 管理

# 步骤 3: 分析输出
cd test-output
jq '.' basic_output_*.jsonl | head -50
jq 'select(.type == "step_start")' *.jsonl
```

### 2. 深入验证（本周）

**检查 Worker Adapter 代码**:
```bash
# 查看 Worker 实现
code internal/worker/opencodecli/worker.go

# 搜索 allowed-tools
grep -r "allowed.*tool\|AllowedTools" internal/worker/opencodecli/

# 搜索 permission
grep -r "permission\|Permission" internal/worker/opencodecli/
```

**运行实际测试**:
```bash
# 测试 allowed-tools
cd ~/opencode
bun run opencode run --format json --allowed-tools read 'test'

# 测试环境变量
HOTPLEX_SESSION_ID=test-123 bun run opencode run --format json 'test'
```

### 3. 更新文档（本周）

**更新 Spec 文档**:
- 修改 CLI 参数章节（添加实际实现的参数）
- 更新输出格式章节（实际格式 + 转换需求）
- 更新实现状态标记（✅/⚠️/❌）
- 添加"实际 vs Spec"章节

**更新 README**:
- 添加验证脚本文档（已完成 ✅）
- 添加测试命令示例

### 4. 实现调整（下周）

如果发现重大差异：
1. 调整 Worker Adapter 实现
2. 实现格式转换层
3. 更新事件映射
4. 补充测试用例

---

## 文件清单

### 新创建的文件

```
scripts/
├── validate-opencode-cli-spec.sh       # 静态验证脚本
└── test-opencode-cli-output.sh         # 动态测试脚本

docs/research/
└── opencode-cli-implementation-analysis.md  # 深度分析文档

test-output/                            # 测试输出目录（gitignored）
├── basic_output_*.jsonl
├── tool_usage_*.jsonl
└── ...
```

### 更新的文件

```
scripts/README.md                       # 添加验证脚本文档
```

---

## 关键结论

### ✅ 已确认

1. OpenCode CLI 支持 `--format json` 输出
2. 基本事件类型已实现（step_start, text, tool_use, error）
3. Session 管理已实现（但命名不同）
4. JSON 输出格式可用（但不是 AEP v1）

### ⚠️ 需注意

1. **格式差异**：需要格式转换层
2. **事件映射**：部分事件命名和结构不同
3. **工具参数**：关键参数未找到 CLI 层面的实现
4. **环境变量**：需进一步验证

### ❓ 待验证

1. Worker Adapter 层面的工具参数实现
2. Permission API 的实际使用
3. MCP 配置的支持路径
4. 流式增量输出的可用性

---

## 预期时间线

- **今天**: 运行测试脚本，捕获实际输出
- **本周**: 完成所有 P0 验证项
- **下周**: 更新 Spec 文档，调整实现（如需要）
- **2 周后**: 完整的验证报告和更新后的 Spec

---

## 联系和协作

如需与 OpenCode 团队沟通：
- 确认工具参数实现路径
- 确认 MCP 配置方式
- 讨论格式差异的解决方案

**文档位置**:
- Spec: `docs/specs/Worker-OpenCode-CLI-Spec.md`
- 分析: `docs/research/opencode-cli-implementation-analysis.md`
- 脚本: `scripts/validate-*.sh`, `scripts/test-*.sh`
