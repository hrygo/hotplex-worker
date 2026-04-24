---
paths:
  - "**/cli/**/*.go"
---

# CLI 自服务规范

> Diagnostic checker framework + onboard wizard + terminal output
> 参考架构：`internal/cli/AGENTS.md`

## Checker 注册模式

```go
// checkers/<name>.go — 实现 Checker 接口，init() 中注册
func init() { DefaultRegistry.Register(&ConfigChecker{}) }

type Checker interface {
    Name() string
    Category() string
    Check(ctx context.Context) []Diagnostic
}
```

### Diagnostic 结果规范
- Status: `StatusPass` / `StatusWarn` / `StatusFail`
- **必须**提供 `FixHint`（Warn/Fail 时的人类可读修复指引）
- 可选 `FixFunc`（用于 `doctor --fix` 自动修复）

### 注册规则
- 所有 checker 必须在 `init()` 中注册到 `DefaultRegistry`
- 禁止运行时动态注册

## Onboard Wizard 模式

```go
// onboard/wizard.go — 交互式 CLI 向导
// 流程：平台选择 → 凭据输入 → 工作目录配置 → 生成 YAML 配置片段
```

### 模板生成
```go
// onboard/templates.go — YAML 模板构建器
// Slack/Feishu 配置模板，输出可直接写入 configs/
```

## Terminal 输出规范

```go
// 禁止直接 fmt.Println — 使用 output.Printer
p := output.NewPrinter(os.Stdout)
p.StatusIcon(output.StatusPass)  // ✓
p.Bold("标题")
p.Color(output.Red, "错误信息")
```

### Report 渲染
```go
// output/report.go — 按 category 分组渲染
// 摘要格式：✓ 12 passed  ⚠ 2 warnings  ✗ 1 failed
```
