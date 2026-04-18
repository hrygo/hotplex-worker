# golangci-lint 配置规范

> 基于 v1.64.8 + 最佳实践，配置文件 `.golangci.yml`
> 所有 lint 操作必须使用 `make lint` / `make fmt`

## 已启用的 Linters

| Linter | 类别 | 用途 |
|--------|------|------|
| errcheck | 正确性 | 未检查的错误返回值 |
| gosimple | 正确性 | 简化代码建议 |
| govet | 正确性 | Go vet 全套检查 |
| ineffassign | 正确性 | 无效赋值 |
| staticcheck | 正确性 | 高级静态分析 |
| unused | 正确性 | 未使用的代码 |
| gofmt | 格式 | 代码格式化 |
| goimports | 格式 | import 排序（含 local-prefixes 分组） |
| misspell | 格式 | 拼写检查 |
| unconvert | 风格 | 不必要的类型转换 |
| unparam | 风格 | 未使用的参数/返回值 |
| errorlint | 正确性 | 错误包装/比较最佳实践 |
| gocritic | 诊断+风格 | 代码改进建议 |

## 已禁用的检查

| 检查 | 原因 |
|------|------|
| fieldalignment (govet) | 低 ROI，仅对高频热路径结构体有意义 |
| shadow (govet) | err 变量遮蔽在顺序代码块中是惯用写法 |
| builtinShadow (gocritic) | Go 1.21+ 的 max/cap 遮蔽是常见模式 |
| unnamedResult (gocritic) | 命名所有返回值过于冗长 |
| typeDefFirst (gocritic) | 需要移动大型类型定义 |
| ifElseChain (gocritic) | if-else 链有时比 switch 更清晰 |
| typeAssertChain (gocritic) | 小函数中的 if-else 类型检查足够 |

## 测试文件排除

测试文件（`_test.go`、`e2e/`）排除以下 linters：
- `errcheck` — 测试中 os.Setenv/类型断言等忽略错误是标准做法
- `gocritic` — 测试代码风格宽松
- `unparam` — 测试 helper 参数固定值是正常的
- `errorlint` — 测试中直接比较错误可接受
- `govet` — 测试中变量遮蔽不影响正确性

## 常见修复模式

### errcheck
```go
// Best-effort 清理操作：用 _ = 显式忽略
_ = resp.Body.Close()
_ = rows.Close()             // defer 中用：defer func() { _ = rows.Close() }()

// 真正需要处理的错误：添加错误处理
if err := json.Unmarshal(data, &v); err != nil {
    return fmt.Errorf("unmarshal: %w", err)
}

// 类型断言：nolint + 原因说明
m["count"] = m["count"].(int) + 1 //nolint:errcheck // guaranteed by filter logic
```

### errorlint
```go
// 错误比较：用 errors.Is 替代 ==
if errors.Is(err, io.EOF) { ... }

// 类型断言：用 errors.As 替代 .(*Type)
var ssrfErr *SSRFProtectionError
if errors.As(err, &ssrfErr) { ... }

// 错误包装：用 %w 替代 %v
return fmt.Errorf("session %s: %w", id, err)
```

### goimports
```bash
# 必须带 -local 参数，和 golangci.yml 的 local-prefixes 一致
goimports -local github.com/hotplex/hotplex-worker -w file.go
```

## 命令参考

```bash
make lint          # 运行 lint
make lint-verbose  # 详细输出
make fmt           # gofmt + goimports
make test          # 测试（含 -race）
make quality       # fmt + vet + lint + test
make check         # 完整 CI 流程
```
