# 故障排除与常见问题

## CI 失败处理优先级

| 优先级 | 检查项 | 失败处理 | 原因 |
|--------|--------|----------|------|
| P0 | Test (三平台) | 必须修复 | 功能正确性 |
| P0 | Build (三平台) | 必须修复 | 编译成功 |
| P1 | Coverage Check | 通常修复 | 覆盖率要求 |

## 常见陷阱

### 跨平台兼容性

CI 在某个平台失败但在本地正常，通常是硬编码路径分隔符。使用 `filepath.Join()` 而非 `"dir/file"`。

### Fork 远程仓库检测失败

确保 `git remote -v` 中有一个远程仓库指向你的 fork：
```bash
git remote add fork https://github.com/<your-username>/hotplex.git
```

### CI 超时

```bash
gh run cancel <run-id> && gh run rerun <run-id>
```

### Lint 配置不一致

始终使用 `make lint` 而非直接 `golangci-lint`。

### 测试依赖缺失

```bash
go mod tidy
```

## 命令速查

| 命令 | 用途 |
|------|------|
| `make test` | 运行测试（含 -race） |
| `make lint` | golangci-lint 检查 |
| `make check` | 完整 CI: quality + build |
| `gh pr checks <N>` | 查看 PR CI 状态 |
| `gh run view <id> --log-failed` | 查看失败日志 |
