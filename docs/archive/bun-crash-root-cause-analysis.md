# Bun 崩溃根本原因分析与修复

**日期**: 2026-05-01
**状态**: ✅ 已修复
**Commit**: 8f31eb9

## 问题描述

Claude Code 在 HotPlex 中启动 worker 时立即崩溃，错误信息 "Illegal instruction"，但直接在服务器上运行 `claude` 命令不会崩溃。

## 根本原因

**HotPlex 对 Claude Code worker 设置了 2GB 虚拟地址空间限制 (RLIMIT_AS)**

### 技术细节

1. **现代 JIT 运行时内存特性**:
   - Bun v1.3.14 (Claude Code 内置) 需要预留 **~73GB 虚拟地址空间**
   - 用途: JIT 代码缓存 + 堆预分配 + WebAssembly 线性内存
   - 实际物理内存 (RSS): 仅 ~350MB

2. **RLIMIT_AS 机制**:
   - 限制的是**虚拟地址空间**，不是物理内存
   - Claude Code: 73GB VSZ > 2GB 限制 → **立即崩溃**
   - 直接运行: unlimited → 正常

3. **历史演变**:
   - **commit 031eab2** (最初): 512MB + Bug（限制 gateway 自己）
   - **commit f3b830d** (第一次修复): 2GB + `unix.Prlimit`（仍不够）
   - **commit 8f31eb9** (本次修复): 完全禁用

## 修复方案

### 代码变更

`internal/worker/proc/memlimit_linux.go`:
```go
func setMemoryLimit(pid int, log *slog.Logger) {
    // DISABLED: Modern JIT runtimes require large VA space
    // See detailed comments in file
    log.Debug("proc: RLIMIT_AS disabled (modern JIT requires large VA space)")
}
```

### 为什么禁用而不是增加限制

1. **虚拟地址空间≠物理内存**: 73GB 预留不会消耗实际 RAM
2. **系统资源充足**: 7GB 总内存，3GB 可用
3. **更好的替代方案**:
   - **Linux**: cgroups v2 (`memory.max`) 精确控制 RSS
   - **容器**: Docker/Kubernetes 内存限制
   - **监控**: Prometheus alerts on `hotplex_worker_memory_bytes`

## 验证结果

### 修复前
- ❌ 每次 worker 启动 → 立即崩溃
- ❌ 崩溃循环: gateway 自动重启 → 再次崩溃
- ❌ 飞书消息无法处理

### 修复后
- ✅ Worker 内存限制: `unlimited`
- ✅ 9+ Claude Code worker 稳定运行
- ✅ Gateway 运行 2+ 分钟，零崩溃
- ✅ 60 秒监控期内: 无 Bun 崩溃
- ✅ 飞书适配器正常工作

## 技术洞察

### RLIMIT_AS 的局限性

在现代系统上，RLIMIT_AS 过于严格：
- **JIT 编译器**: 需要大块连续虚拟地址空间
- **64位系统**: 虚拟地址空间充足（128TB），不应限制
- **OS 页面扫描器**: 更适合管理物理内存压力

### 生产环境建议

对于需要内存隔离的场景：
1. **cgroups v2**: `memory.max` 限制实际物理内存使用
2. **容器化**: Docker/Kubernetes 内存限制
3. **监控告警**: Prometheus + Grafana 监控 RSS

## 相关文件

- `internal/worker/proc/memlimit_linux.go` - 内存限制实现
- `internal/worker/proc/manager.go` - 进程管理器
- `internal/worker/claudecode/worker.go` - Claude Code 适配器
- `configs/config.yaml` - worker 配置

## 参考资料

- [Linux prlimit(2) manual](https://man7.org/linux/man-pages/man2/prlimit.2.html)
- [cgroups v2 memory controller](https://docs.kernel.org/admin-guide/cgroup-v2.html)
- Issue: Bun crashes in HotPlex but not standalone

---

**作者**: Claude Sonnet 4.6
**审查**: Sisyphus 🏔️
**部署**: 已部署到生产环境 (PID 211451)
