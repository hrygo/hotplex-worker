---
paths:
  - "**/*_test.go"
  - "**/testutil/**/*.go"
---

# 测试规范

## 断言库
- 使用 `testify/require` 而非 `t.Fatal`（更细粒度的错误信息）
- 使用 `testify/mock` 管理 mock 对象
- 禁止 `t.Fatal` / `t.Fatalf` — 一律用 `require.*`

## Test Table 模式
```go
tests := []struct {
    name  string
    input string
    want  string
}{
    {"idle timeout", "30m", "TERMINATED"},
    {"max lifetime", "24h", "TERMINATED"},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()  // 无状态测试必须标记 Parallel
        got := process(tt.input)
        require.Equal(t, tt.want, got)
    })
}
```

## 并发测试
- 无共享状态的测试用例必须标记 `t.Parallel()`
- 需要独占资源（SQLite、端口）的测试不标记 Parallel

## Race 检测
所有测试通过 `go test -race ./...`，CI 强制开启，零容忍 data race。

## Coverage
```bash
make coverage   # go test -coverprofile + HTML 报告
```

## 资源清理
```go
// 使用 t.Cleanup() 确保资源释放
db, err := sql.Open("sqlite", ":memory:")
require.NoError(t, err)
t.Cleanup(func() { db.Close() })

// 临时目录
dir := t.TempDir()  // 自动清理，无需 t.Cleanup
```

## Gateway 测试工具
```
internal/gateway/testutil/  — WebSocket mock helpers
// 用于 hub/conn/bridge 的集成测试
// 提供：MockConn, WriteEnvelope, ReadEnvelope 等辅助函数
```

## Mock 适配器
```
internal/messaging/mock/  — Mock messaging adapter
// 用于 bridge/handler 的跨适配器集成测试
```

## E2E 测试
- `e2e/` 目录：端到端集成测试
- 测试文件排除部分 linters（见 `linting.md`）
- 需要 gateway 运行的测试用 `// +build e2e` 或短 flag 跳过

## 命令参考
```bash
make test           # 全量测试（含 -race，15 分钟超时）
make test-short     # 快速测试（-short，跳过耗时用例）
make coverage       # 覆盖率报告
```
