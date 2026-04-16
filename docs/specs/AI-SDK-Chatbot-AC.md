---
type: spec
tags:
  - project/HotPlex
  - integration/ai-sdk
  - acceptance-criteria
spec_id: AI-SDK-001
date: 2026-04-03
status: draft
progress: 0
version: v1.0
---

# AI SDK Chatbot 集成验收标准

> **Spec ID**: AI-SDK-001
> **版本**: v1.0
> **日期**: 2026-04-03
> **状态**: Draft

**图例:**
- 状态: ⬜ TODO | 🟦 IN_PROGRESS | 🟩 PASS | 🟥 FAIL | ⬛ N/A
- 优先级: 🔴 P0 = MVP 必须 | 🟡 P1 = 重要 | ⚪ P2 = 增强

---

## 目录

1. [Transport 适配层](#transport-适配层)
2. [React Hook 集成](#react-hook-集成)
3. [消息处理](#消息处理)
4. [错误处理与重连](#错误处理与重连)
5. [性能与优化](#性能与优化)
6. [安全](#安全)
7. [文档与示例](#文档与示例)
8. [测试覆盖](#测试覆盖)

---

## Transport 适配层 (15 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 1 | **TR-001** | 实现 ChatTransport 接口 (sendMessage, onData, onFinish, onError) | 🔴 P0 | ⬜ TODO | - | TypeScript 接口实现 |
| 2 | **TR-002** | AEP message.start → 创建消息缓冲区 | 🔴 P0 | ⬜ TODO | - | Map<message_id, buffer> |
| 3 | **TR-003** | AEP message.delta → AI SDK onData 回调 | 🔴 P0 | ⬜ TODO | - | 流式文本推送 |
| 4 | **TR-004** | AEP message.end → AI SDK onFinish 回调 | 🔴 P0 | ⬜ TODO | - | 完整消息聚合 |
| 5 | **TR-005** | AEP error → AI SDK onError 回调 | 🔴 P0 | ⬜ TODO | - | 错误码映射 |
| 6 | **TR-006** | AEP done 事件处理 (success/failure) | 🔴 P0 | ⬜ TODO | - | Turn 完成检测 |
| 7 | **TR-007** | AEP tool_call → AI SDK onData (type: tool-call) | 🟡 P1 | ⬜ TODO | - | 工具调用显示 |
| 8 | **TR-008** | AEP permission_request → UI 对话框 | 🟡 P1 | ⬜ TODO | - | 权限请求处理 |
| 9 | **TR-009** | 复用 HotPlexClient WebSocket 连接 | 🔴 P0 | ⬜ TODO | - | 避免重复实现 |
| 10 | **TR-010** | Transport 生命周期管理 (connect/disconnect) | 🔴 P0 | ⬜ TODO | - | 清理资源防止泄漏 |
| 11 | **TR-011** | 消息缓冲区溢出保护 | 🟡 P1 | ⬜ TODO | - | 最大 10MB 限制 |
| 12 | **TR-012** | 背压丢弃检测 (done.dropped=true) | 🟡 P1 | ⬜ TODO | - | UI Reconciliation 触发 |
| 13 | **TR-013** | 多消息并发处理 (多个 message_id 同时存在) | 🟡 P1 | ⬜ TODO | - | 并发消息缓冲 |
| 14 | **TR-014** | 类型安全的 TypeScript 定义 | 🔴 P0 | ⬜ TODO | - | 完整接口定义 |
| 15 | **TR-015** | 编译时类型检查无错误 | 🔴 P0 | ⬜ TODO | - | `tsc --noEmit` 通过 |

**验证方法:**
- 单元测试: `src/__tests__/hotplex-transport.test.ts`
- 集成测试: `src/__tests__/integration.test.ts`

---

## React Hook 集成 (10 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 16 | **HK-001** | useHotPlexChat Hook 返回 { messages, sendMessage, status, error } | 🔴 P0 | ⬜ TODO | - | AI SDK useChat 兼容 |
| 17 | **HK-002** | 自动连接管理 (首次调用时连接) | 🔴 P0 | ⬜ TODO | - | useMemo + useEffect |
| 18 | **HK-003** | 组件卸载时自动断开连接 | 🔴 P0 | ⬜ TODO | - | useEffect cleanup |
| 19 | **HK-004** | 连接状态同步 (connected/disconnected/reconnecting) | 🔴 P0 | ⬜ TODO | - | 实时状态更新 |
| 20 | **HK-005** | 消息状态管理 (id, role, content, timestamp) | 🔴 P0 | ⬜ TODO | - | AI SDK message 格式 |
| 21 | **HK-006** | loading 状态指示 (status === 'loading') | 🔴 P0 | ⬜ TODO | - | 输入禁用控制 |
| 22 | **HK-007** | 错误状态管理 (error 对象) | 🔴 P0 | ⬜ TODO | - | 用户友好提示 |
| 23 | **HK-008** | 支持多实例 (多个 Hook 实例隔离) | 🟡 P1 | ⬜ TODO | - | 不同 sessionId |
| 24 | **HK-009** | 支持会话恢复 (sessionId 配置) | 🟡 P1 | ⬜ TODO | - | Resume 现有会话 |
| 25 | **HK-010** | 支持 React 18+ 并发特性 (StrictMode 兼容) | 🟡 P1 | ⬜ TODO | - | 无副作用重复 |

**验证方法:**
- Hook 测试: `src/__tests__/useHotPlexChat.test.ts`
- React Testing Library 集成测试

---

## 消息处理 (12 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 26 | **MSG-001** | 用户消息正确发送到 HotPlex | 🔴 P0 | ⬜ TODO | - | sendInput(content) |
| 27 | **MSG-002** | AI 响应流式接收并实时渲染 | 🔴 P0 | ⬜ TODO | - | delta → UI 更新 |
| 28 | **MSG-003** | 消息聚合正确 (delta → 完整消息) | 🔴 P0 | ⬜ TODO | - | 缓冲区管理 |
| 29 | **MSG-004** | 消息 ID 唯一且稳定 | 🔴 P0 | ⬜ TODO | - | 使用 AEP message.id |
| 30 | **MSG-005** | 消息角色正确 (user/assistant) | 🔴 P0 | ⬜ TODO | - | AEP role 映射 |
| 31 | **MSG-006** | 消息时间戳正确 (createdAt) | 🟡 P1 | ⬜ TODO | - | Unix ms → Date |
| 32 | **MSG-007** | 消息顺序正确 (用户 → AI → 用户 → AI) | 🔴 P0 | ⬜ TODO | - | 时序保证 |
| 33 | **MSG-008** | 空消息过滤 (不允许空内容) | 🟡 P1 | ⬜ TODO | - | 输入验证 |
| 34 | **MSG-009** | 超长消息处理 (> 32KB) | 🟡 P1 | ⬜ TODO | - | 提示用户分段 |
| 35 | **MSG-010** | 多行文本正确渲染 | 🟡 P1 | ⬜ TODO | - | whitespace-pre-wrap |
| 36 | **MSG-011** | Markdown 格式支持 (可选) | ⚪ P2 | ⬜ TODO | - | react-markdown |
| 37 | **MSG-012** | 代码块高亮 (可选) | ⚪ P2 | ⬜ TODO | - | prism.js |

**验证方法:**
- 消息流测试: 发送 → 接收 → 验证
- UI 快照测试: 确保渲染正确

---

## 错误处理与重连 (10 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 38 | **ERR-001** | 连接失败错误提示清晰 | 🔴 P0 | ⬜ TODO | - | 网络错误 → 用户提示 |
| 39 | **ERR-002** | 认证失败处理 (UNAUTHORIZED) | 🔴 P0 | ⬜ TODO | - | 重新登录流程 |
| 40 | **ERR-003** | 会话忙重试 (SESSION_BUSY) | 🔴 P0 | ⬜ TODO | - | 自动延迟重试 |
| 41 | **ERR-004** | 会话不存在处理 (SESSION_NOT_FOUND) | 🔴 P0 | ⬜ TODO | - | 创建新会话 |
| 42 | **ERR-005** | 自动重连机制 (指数退避) | 🔴 P0 | ⬜ TODO | - | 1s → 2s → 4s → ... |
| 43 | **ERR-006** | 最大重连次数限制 (10 次) | 🟡 P1 | ⬜ TODO | - | 防止无限重试 |
| 44 | **ERR-007** | 重连状态指示 (reconnecting attempt N/M) | 🔴 P0 | ⬜ TODO | - | 用户可见 |
| 45 | **ERR-008** | 服务端请求重连处理 (control: reconnect) | 🟡 P1 | ⬜ TODO | - | 按 delay_ms 重连 |
| 46 | **ERR-009** | Session 失效处理 (session_invalid) | 🟡 P1 | ⬜ TODO | - | 清理状态并重连 |
| 47 | **ERR-010** | 错误边界包装 (React Error Boundary) | 🟡 P1 | ⬜ TODO | - | 降级 UI |

**验证方法:**
- 错误场景模拟: Mock 各种错误
- 重连逻辑测试: 断开 → 重连 → 验证

---

## 性能与优化 (8 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 48 | **PERF-001** | 流式渲染流畅度 ≥ 30 FPS | 🟡 P1 | ⬜ TODO | - | requestAnimationFrame 批量更新 |
| 49 | **PERF-002** | 首次连接时间 ≤ 2 秒 | 🟡 P1 | ⬜ TODO | - | 本地网络环境 |
| 50 | **PERF-003** | 消息延迟 ≤ 100ms (P95) | 🟡 P1 | ⬜ TODO | - | 输入 → 首个 delta |
| 51 | **PERF-004** | 内存占用 ≤ 50MB (长会话) | 🟡 P1 | ⬜ TODO | - | 消息缓冲限制 |
| 52 | **PERF-005** | 消息批量更新 (100ms window) | 🟡 P1 | ⬜ TODO | - | 减少 re-render |
| 53 | **PERF-006** | 虚拟滚动 (消息 > 100 条) | ⚪ P2 | ⬜ TODO | - | react-window |
| 54 | **PERF-007** | Bundle 大小 ≤ 60KB (gzip ≤ 20KB) | 🟡 P1 | ⬜ TODO | - | 依赖优化 |
| 55 | **PERF-008** | 无内存泄漏 (长时间运行) | 🔴 P0 | ⬜ TODO | - | 清理监听器 + 定时器 |

**验证方法:**
- 性能基准测试: Chrome DevTools Performance
- 内存分析: Heap Snapshots

---

## 安全 (5 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 56 | **SEC-001** | authToken 不暴露到客户端日志 | 🔴 P0 | ⬜ TODO | - | 敏感信息过滤 |
| 57 | **SEC-002** | WebSocket URL 使用 WSS (生产环境) | 🔴 P0 | ⬜ TODO | - | TLS 加密 |
| 58 | **SEC-003** | XSS 防护 (消息内容转义) | 🔴 P0 | ⬜ TODO | - | React 默认转义 |
| 59 | **SEC-004** | CORS 配置正确 | 🔴 P0 | ⬜ TODO | - | AllowedOrigins |
| 60 | **SEC-005** | 会话隔离 (不同用户看不到彼此消息) | 🔴 P0 | ⬜ TODO | - | JWT user_id 验证 |

**验证方法:**
- 安全审计: OWASP Top 10 检查
- 渗透测试: 模拟攻击场景

---

## 文档与示例 (8 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 61 | **DOC-001** | Transport 接口 API 文档 | 🔴 P0 | ⬜ TODO | - | TSDoc 注释 |
| 62 | **DOC-002** | useHotPlexChat Hook 文档 | 🔴 P0 | ⬜ TODO | - | 使用示例 |
| 63 | **DOC-003** | 快速开始指南 (5 分钟集成) | 🔴 P0 | ⬜ TODO | - | README.md |
| 64 | **DOC-004** | 完整示例应用 (Next.js) | 🟡 P1 | ⬜ TODO | - | webchat |
| 65 | **DOC-005** | 错误码映射表 | 🟡 P1 | ⬜ TODO | - | AEP → 用户提示 |
| 66 | **DOC-006** | 性能优化指南 | ⚪ P2 | ⬜ TODO | - | 最佳实践 |
| 67 | **DOC-007** | 故障排查指南 | 🟡 P1 | ⬜ TODO | - | 常见问题 FAQ |
| 68 | **DOC-008** | 迁移指南 (从旧版本升级) | ⚪ P2 | ⬜ TODO | - | Breaking Changes |

**验证方法:**
- 文档审查: 技术写作评审
- 用户测试: 新用户按文档操作

---

## 测试覆盖 (7 条)

| # | ID | 描述 | 优先级 | 状态 | 验证人 | 备注 |
|---|----|------|--------|------|--------|------|
| 69 | **TEST-001** | 单元测试覆盖率 ≥ 80% | 🔴 P0 | ⬜ TODO | - | Vitest + @testing-library |
| 70 | **TEST-002** | Transport 适配器单元测试 | 🔴 P0 | ⬜ TODO | - | 事件映射测试 |
| 71 | **TEST-003** | Hook 单元测试 | 🔴 P0 | ⬜ TODO | - | renderHook + act |
| 72 | **TEST-004** | 集成测试 (E2E) | 🟡 P1 | ⬜ TODO | - | Playwright/Cypress |
| 73 | **TEST-005** | 错误场景测试 | 🔴 P0 | ⬜ TODO | - | 断网/超时/认证失败 |
| 74 | **TEST-006** | 性能回归测试 | ⚪ P2 | ⬜ TODO | - | Benchmark CI |
| 75 | **TEST-007** | 可访问性测试 (a11y) | ⚪ P2 | ⬜ TODO | - | axe-core |

**验证方法:**
- CI 自动化测试: GitHub Actions
- 覆盖率报告: vitest --coverage

---

## 总结

### 统计

| 类别 | 总数 | P0 | P1 | P2 |
|------|------|----|----|----|
| Transport 适配 | 15 | 10 | 4 | 1 |
| React Hook | 10 | 7 | 3 | 0 |
| 消息处理 | 12 | 6 | 4 | 2 |
| 错误处理 | 10 | 6 | 4 | 0 |
| 性能优化 | 8 | 1 | 6 | 1 |
| 安全 | 5 | 5 | 0 | 0 |
| 文档 | 8 | 3 | 3 | 2 |
| 测试 | 7 | 4 | 1 | 2 |
| **总计** | **75** | **42** | **25** | **8** |

### MVP 范围 (P0)

**必须完成**: 42 条 P0 验收标准
**预计工时**: 16 小时 (2 天)
**关键路径**: TR-001~006, HK-001~004, MSG-001~007, ERR-001~005

### 完整版范围 (P0 + P1)

**建议完成**: 67 条 P0+P1 验收标准
**预计工时**: 24 小时 (3 天)
**关键路径**: MVP + PERF-001~004, SEC-001~005, DOC-001~007, TEST-001~005

---

## 验收流程

### Phase 1: MVP 验收 (第 1-2 天)

1. **Transport 核心功能** (TR-001 ~ TR-010)
   - [ ] 所有单元测试通过
   - [ ] 集成测试验证消息流

2. **Hook 基础功能** (HK-001 ~ HK-007)
   - [ ] React 组件测试通过
   - [ ] 状态管理验证

3. **消息处理** (MSG-001 ~ MSG-007)
   - [ ] 流式渲染正常
   - [ ] 消息聚合正确

4. **错误处理** (ERR-001 ~ ERR-005)
   - [ ] 重连机制工作
   - [ ] 错误提示清晰

### Phase 2: 完整验收 (第 3 天)

1. **性能优化** (PERF-001 ~ PERF-005)
   - [ ] 性能基准测试通过
   - [ ] 内存泄漏检测

2. **安全加固** (SEC-001 ~ SEC-005)
   - [ ] 安全审计通过
   - [ ] 渗透测试无高危漏洞

3. **文档完善** (DOC-001 ~ DOC-007)
   - [ ] API 文档完整
   - [ ] 示例应用可运行

4. **测试覆盖** (TEST-001 ~ TEST-005)
   - [ ] 覆盖率 ≥ 80%
   - [ ] CI 流水线通过

---

## 签署

| 角色 | 姓名 | 日期 | 签名 |
|------|------|------|------|
| **前端负责人** | _待定_ | _TBD_ | _TBD_ |
| **后端负责人** | _待定_ | _TBD_ | _TBD_ |
| **技术负责人** | _待定_ | _TBD_ | _TBD_ |
| **QA 负责人** | _待定_ | _TBD_ | _TBD_ |

---

**文档维护者**: Frontend Team
**最后更新**: 2026-04-03
**下次审查**: 2026-04-10
