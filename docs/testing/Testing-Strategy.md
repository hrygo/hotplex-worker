---
type: design
tags:
  - project/HotPlex
  - testing/strategy
  - quality/assurance
---

# Testing Strategy

> HotPlex v1.0 测试策略，基于行业最佳实践。
>
> ⚠️ **修正**（2026-04-21）：
> - PostgreSQL Store 是 stub（`ErrNotImplemented`），**生产使用 SQLite**；测试应使用内存 SQLite，无需 Testcontainers
> - 项目极少使用 `testify/mock`，建议改为 `testify/require` 做断言 + 实际依赖（最小化 mock）
> - 表驱动测试和 `t.Parallel()` 符合当前代码实践

---

## 1. 设计原则

### 1.1 行业最佳实践

| 来源 | 核心观点 |
|------|----------|
| Martin Fowler | 测试金字塔：底层单元测试多、高层 GUI 测试少 |
| Kent C. Dodds | 测试奖杯：集成测试的价值被低估 |
| Google Testing Blog | 覆盖率是**误导指标**，应关注高风险代码覆盖 |
| Go Blog | 表驱动测试 + `t.Parallel()` + `t.Errorf` 优于断言库 |

### 1.2 HotPlex 测试策略

```
┌─────────────────────────────────────────────────────────────┐
│  E2E / Smoke Tests (Playwright)                            │
│  • WebSocket 连接 + 消息收发                                │
│  • 会话生命周期                                              │
│  目标：10-20 个关键路径                                     │
└─────────────────────────────────────────────────────────────┘
                          ▲
┌─────────────────────────────────────────────────────────────┐
│  Integration Tests (go test + 实际 SQLite)                │
│  • Session Pool 并发安全                                    │
│  • PGID 进程隔离                                            │
│  • WebSocket 消息路由                                       │
│  目标：50-80 个集成测试                                     │
└─────────────────────────────────────────────────────────────┘
                          ▲
┌─────────────────────────────────────────────────────────────┐
│  Unit Tests (go test + testify/require，表驱动)              │
│  • WAF 正则检测器                                           │
│  • strutil 工具函数                                         │
│  • config 解析与验证                                        │
│  目标：200+ 单元测试                                         │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. 单元测试最佳实践

### 2.1 表驱动测试

**Go 官方推荐模式**：

```go
// 模式：测试用例在数据结构中，迭代执行
func TestReverse(t *testing.T) {
    tests := map[string]struct {
        input    string
        expected string
    }{
        "empty":       {input: "", expected: ""},
        "single":      {input: "a", expected: "a"},
        "two":         {input: "ab", expected: "ba"},
        "palindrome":   {input: "aba", expected: "aba"},
        "chinese":      {input: "你好", expected: "好你"},
    }

    for name, tc := range tests {
        t.Run(name, func(t *testing.T) {
            t.Parallel()  // 子测试级并行
            got := reverse(tc.input)
            if got != tc.expected {
                t.Errorf("reverse(%q) = %q; want %q", tc.input, got, tc.expected)
            }
        })
    }
}
```

**关键实践**：
- ✅ 使用 `t.Parallel()` 加速测试
- ✅ 使用 `t.Errorf` 而非断言——失败后继续执行，暴露同类错误
- ✅ 子测试名称包含输入描述
- ✅ Go < 1.22：循环内 `test := test` 避免闭包陷阱

### 2.2 Mock 框架选择

**推荐**：`testify/mock`

| 框架 | 特点 | 适用场景 | 推荐度 |
|------|------|----------|--------|
| **testify/mock** | 轻量、无代码生成、链式 API | 中小项目、快速原型 | ⭐⭐⭐⭐⭐ |
| **gomock** | 代码生成、编译时验证 | 大型项目 | ⭐⭐⭐⭐ |
| **minimock** | 简洁、注释驱动 | 简单接口 | ⭐⭐⭐ |

```go
import "github.com/stretchr/testify/mock"

type MockSessionManager struct {
    mock.Mock
}

func (m *MockSessionManager) GetSession(id string) (*Session, error) {
    args := m.Called(id)
    return args.Get(0).(*Session), args.Error(1)
}

func (m *MockSessionManager) CreateSession(ownerID, workerType string) (*Session, error) {
    args := m.Called(ownerID, workerType)
    return args.Get(0).(*Session), args.Error(1)
}

// 使用
func TestHandleInit(t *testing.T) {
    mockSM := new(MockSessionManager)

    mockSM.On("CreateSession", "user1", "claude-code").
        Return(&Session{ID: "sess_test"}, nil)

    handler := NewHandler(mockSM)
    result, err := handler.HandleInit("user1", "claude-code")

    require.NoError(t, err)
    assert.Equal(t, "sess_test", result.SessionID)
    mockSM.AssertExpectations(t)
}
```

### 2.3 不 Mock 的核心功能

> ⚠️ **HotPlex 的核心功能必须真实测试，不 Mock**：

| 核心功能 | 原因 | 测试方式 |
|----------|------|----------|
| `os/exec` + PGID | 进程隔离是核心价值 | 真实子进程集成测试 |
| WebSocket 连接 | I/O 多路复用逻辑 | 内嵌 mock server |
| Session Pool 并发 | 竞态条件检测 | 真实并发测试 |

```go
// ❌ 不要 Mock PGID 逻辑
func TestPGIDIsolation(t *testing.T) {
    // 使用真实子进程测试
    cmd := exec.Command("sleep", "10")
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    err := cmd.Start()
    require.NoError(t, err)

    pgid := syscall.Getpgid(cmd.Process.Pid)

    // 真实 Kill process group
    err = syscall.Kill(-pgid, syscall.SIGKILL)
    require.NoError(t, err)

    // 验证进程已终止
    ps, _ := os.FindProcess(cmd.Process.Pid)
    code, _ := ps.Wait()

    assert.True(t, code.Exited())
    assert.Equal(t, 1, code.ExitCode())
}
```

---

## 3. 集成测试

### 3.1 Testcontainers

**推荐用于**：PostgreSQL、Redis 等外部依赖。

```go
import "github.com/testcontainers/testcontainers-go"

func TestPostgresSessionStore(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    ctx := context.Background()

    req := testcontainers.ContainerRequest{
        Image:        "postgres:16-alpine",
        ExposedPorts: []string{"5432/tcp"},
        Env: map[string]string{
            "POSTGRES_PASSWORD": "secret",
            "POSTGRES_DB":       "testdb",
        },
    }

    container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: req,
        Started:           true,
    })
    require.NoError(t, err)
    defer container.Terminate(ctx)

    // 获取连接信息
    port, _ := container.MappedPort(ctx, "5432")
    host, _ := container.Host(ctx)

    dsn := fmt.Sprintf("host=%s port=%d user=postgres password=secret dbname=testdb", host, port)

    // 执行真实集成测试
    store, err := NewPostgresSessionStore(dsn)
    require.NoError(t, err)

    session, err := store.CreateSession("user1", "claude-code")
    require.NoError(t, err)
    assert.NotEmpty(t, session.ID)
}
```

### 3.2 WebSocket Mock Server

```go
// testutil/ws_mock_server.go

type MockWSServer struct {
    server   *httptest.Server
    upgrader *websocket.Upgrader
    handler  func(conn *websocket.Conn)
}

func NewMockWSServer(handler func(conn *websocket.Conn)) *MockWSServer {
    mux := http.NewServeMux()
    mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        upgrader := &websocket.Upgrader{}
        conn, _ := upgrader.Upgrade(w, r, nil)
        handler(conn)
    })

    return &MockWSServer{
        server: httptest.NewServer(mux),
    }
}

func TestWebSocketMessageRouting(t *testing.T) {
    server := NewMockWSServer(func(conn *websocket.Conn) {
        for {
            _, msg, err := conn.ReadMessage()
            if err != nil {
                return
            }
            // Echo back
            conn.WriteMessage(websocket.TextMessage, msg)
        }
    })
    defer server.Close()

    // 连接并测试
    conn, _, err := websocket.Dial(server.URL+"/ws", nil)
    require.NoError(t, err)
    defer conn.Close()

    // 发送消息
    err = conn.WriteMessage(websocket.TextMessage, []byte(`{"kind":"input"}`))

    // 接收响应
    _, resp, err := conn.ReadMessage()
    assert.JSONEq(t, `{"kind":"input"}`, string(resp))
}
```

---

## 4. E2E 测试

### 4.1 工具选择

| 工具 | 推荐度 | 原因 |
|------|--------|------|
| **Playwright** | ⭐⭐⭐⭐⭐ | 速度快、调试强、多语言支持 |
| Cypress | ⭐⭐⭐ | 成熟但较慢 |
| Selenium | ⭐⭐ | 古老、维护成本高 |

### 4.2 冒烟测试策略

```go
// e2e/smoke_test.go

func TestSmokeWebSocketConnection(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping E2E test in short mode")
    }

    // 启动真实 Gateway
    gateway := startRealGateway(t)
    defer gateway.Close()

    // 建立 WebSocket 连接
    wsURL := fmt.Sprintf("ws://%s/gateway", gateway.Addr())
    conn, _, err := websocket.Dial(context.Background(), wsURL,
        websocket.WithHeader("Authorization", "Bearer "+testToken))
    require.NoError(t, err)
    defer conn.Close(websocket.StatusNormalClosure, "")

    // 发送 init
    init := Envelope{Kind: "init", Data: map[string]interface{}{
        "protocol_version": "aep/v1",
        "client_caps":      []string{"streaming"},
    }}
    sendEnvelope(conn, init)

    // 接收 init_ack
    ack := recvEnvelope(conn)
    assert.Equal(t, "init_ack", ack.Kind)
    assert.NotEmpty(t, ack.Data["session_id"])
}

func TestSmokeSessionLifecycle(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping E2E test in short mode")
    }

    // 创建 Session → 发送输入 → 接收输出 → 终止
    // ...
}
```

### 4.3 测试分层配置

```bash
# 快速检查（每次提交）
go test -short ./...

# 完整测试（含集成测试）
go test -tags=integration ./...

# E2E 测试（发布前）
go test -tags=e2e -timeout=30m ./e2e/...

# 竞态检测（必须通过）
go test -race ./...
```

---

## 5. 覆盖率目标

### 5.1 模块级覆盖率

CI 使用**按包分级阈值**检查覆盖率，而非单一全局百分比。各模块按可测试性分级：

| 模块 | 覆盖率目标 | 理由 |
|------|-----------|------|
| `internal/security/` | **85%+** | 安全核心，边界条件必须覆盖 |
| `pkg/events/`, `pkg/aep/` | **85%+** | 协议编解码，纯逻辑 |
| `internal/gateway/` | **75%+** | WebSocket 核心引擎，部分集成复杂度 |
| `internal/session/` | **70%+** | 状态机 + SQLite 持久化 |
| `internal/config/` | **70%+** | Viper 配置解析与热重载 |
| `internal/worker/base/` | **70%+** | 共享 worker 生命周期 |
| `internal/worker/` (registry) | **70%+** | Worker 注册与调度 |
| `internal/worker/claudecode/` | **60%+** | 外部进程适配器 |
| `` | **60%+** | 外部进程适配器 |
| `internal/worker/opencodeserver/` | **50%+** | 802 行重集成适配器 |
| `internal/worker/noop/` | **80%+** | 简单 no-op，易于完全覆盖 |
| `internal/messaging/` | **50%+** | 平台适配层，SDK/子进程/API 依赖重 |
| `internal/worker/proc/` | **排除** | PGID 进程隔离，通过集成测试覆盖 |
| `internal/worker/pi/` | **排除** | 13 行 stub 适配器 |
| `cmd/hotplex/` | **排除** | main 包 DI 组装，非单元测试目标 |
| `internal/admin/` | **不设门** | 基础设施 HTTP API |
| `internal/tracing/` | **不设门** | OpenTelemetry 初始化 |
| `internal/metrics/` | **不设门** | Prometheus 指标定义 |

### 5.2 覆盖率报告

```bash
# 生成覆盖率报告（与 CI 一致的排除规则）
make coverage

# 或手动生成 HTML 报告
go tool cover -html=coverage.out -o coverage.html
```

### 5.3 覆盖率检查（CI）

CI 使用按包分级阈值，每个包独立检查 pass/fail：
- 按包覆盖率低于对应阈值 → CI 失败
- 总体覆盖率仅作信息展示，不设门
- 详见 `.github/workflows/ci.yml` 中的 `coverage-check` job

---

## 6. 性能测试

### 6.1 k6 负载测试

```javascript
// k6/smoke.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  thresholds: {
    http_req_duration: ['p(95)<500'],  // 95% 请求 < 500ms
    http_req_failed: ['rate<0.01'],   // 失败率 < 1%
  },
};

export default function () {
  // Health check
  const health = http.get('http://localhost:9999/health');
  check(health, { 'status is 200': (r) => r.status === 200 });

  // WebSocket test
  // ...

  sleep(1);
}
```

### 6.2 并发会话测试

```go
func BenchmarkConcurrentSessions(b *testing.B) {
    gateway := startRealGateway(b)
    defer gateway.Close()

    b.ResetTimer()

    var wg sync.WaitGroup
    for i := 0; i < b.N; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()

            conn, _, _ := websocket.Dial(context.Background(),
                fmt.Sprintf("ws://%s/gateway", gateway.Addr()))

            for j := 0; j < 10; j++ {
                conn.WriteJSON(Envelope{Kind: "input", Data: map[string]interface{}{
                    "content": fmt.Sprintf("msg-%d", j),
                }})
            }

            conn.Close(websocket.StatusNormalClosure, "")
        }()
    }

    wg.Wait()
}
```

---

## 7. 安全测试

### 7.1 命令注入测试

```go
func TestCommandInjectionPayloads(t *testing.T) {
    dangerousInputs := []string{
        "; rm -rf /",
        "| cat /etc/passwd",
        "&& whoami",
        "$(id)",
        "`id`",
        "test\nnewline",
    }

    for _, input := range dangerousInputs {
        t.Run(input, func(t *testing.T) {
            cmd := exec.Command("claude", "--dir", "/tmp", "--prompt", input)

            // 执行
            output, err := cmd.CombinedOutput()

            // 验证：命令不应执行危险操作
            assert.NotContains(t, string(output), "root")
            assert.NotContains(t, string(output), "etc/passwd")

            // 验证：错误码应为 0（命令正常执行，未注入）
            assert.Equal(t, 0, cmd.ProcessState.ExitCode())
        })
    }
}
```

### 7.2 模糊测试（Fuzzing）

```go
func FuzzEnvelopeValidation(f *testing.F) {
    f.Add(`{"version":"aep/v1","kind":"input","timestamp":123}`)
    f.Add(`{"version":"aep/v1","kind":"init","timestamp":456}`)

    f.Fuzz(func(t *testing.T, data string) {
        err := ValidateEnvelope([]byte(data))

        // 验证：无效输入应返回错误，不应 panic
        if err == nil {
            // 验证：有效输入应通过基本结构检查
            var env Envelope
            json.Unmarshal([]byte(data), &env)
        }
    })
}
```

---

## 8. CI/CD 集成

### 8.1 GitHub Actions

```yaml
name: Test
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Unit & Integration Tests
        run: |
          go test -race -coverprofile=coverage.out ./...
          go tool cover -func=coverage.out | grep "^total:"

      - name: Lint
        run: golangci-lint run

      - name: E2E Smoke Tests
        if: github.event_name == 'pull_request'
        run: |
          make build
          make test-e2e

      - name: Coverage Report
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage.out
          fail_ci_if_error: true
          threshold: 80%
```

### 8.2 本地开发

```makefile
.PHONY: test test-unit test-integration test-e2e test-race

test:
	go test -short -race ./...

test-unit:
	go test -race ./internal/... ./protocol/... ./security/...

test-integration:
	go test -tags=integration -race ./...

test-e2e:
	go test -tags=e2e -timeout=30m ./e2e/...

test-race:
	go test -race -count=1 ./...

test-fuzz:
	go test -fuzz=FuzzEnvelopeValidation -fuzztime=60s ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
```

---

## 9. 实施路线图

| 阶段 | 任务 | 目标 |
|------|------|------|
| **Phase 1** | 补全单元测试 | strutil、config、security 模块 ≥90% |
| **Phase 2** | 建设集成测试 | Session Pool、WebSocket mock server |
| **Phase 3** | E2E 测试 | Playwright 冒烟测试 |
| **Phase 4** | 性能测试 | k6 负载测试 + 基准 |
| **Phase 5** | Fuzzing | WAF 正则检测器模糊测试 |

---

## 10. 参考资料

- [Martin Fowler: Practical Test Pyramid](https://martinfowler.com/articles/practical-test-pyramid.html)
- [Kent C. Dodds: Testing Trophy](https://kentcdodds.com/blog/the-testing-trophy-and-classifying-tests)
- [Go Wiki: Table Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Go Blog: Testing](https://go.dev/blog/tests)
- [Google Testing Blog: Coverage](https://testing.googleblog.com/2020/08/testing-cov.html)
- [Testcontainers](https://testcontainers.com)
- [Playwright](https://playwright.dev)
- [k6](https://grafana.com/docs/k6/latest/)