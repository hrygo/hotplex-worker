# HotPlex Windows 支持规范

**状态**: Proposed
**创建日期**: 2026-04-29
**追踪 issue**: [#64](https://github.com/hrygo/hotplex/issues/64)

---

## 概述

本规范定义 HotPlex Worker Gateway 对 Windows 平台的官方支持。核心 Go 逻辑可跨平台运行，主要工作集中在进程管理、路径处理和 CI/CD 的平台适配。

---

## 背景

HotPlex 当前仅支持 macOS (darwin) 和 Linux (linux)，在 `.github/workflows/release.yml` 的构建矩阵中硬编码。当前架构已有良好的跨平台基础（Go 标准库），但进程管理模块重度依赖 POSIX API。

**POSIX 特定依赖**：
- 进程组隔离 (`Setpgid`)
- 信号发送 (`syscall.Kill`)
- 进程状态获取 (`syscall.WaitStatus`)

---

## 目标

1. 提供 Windows amd64/arm64 原生二进制分发
2. 保持代码可维护性，避免平台特定逻辑污染核心逻辑
3. 确保 CI/CD 全流程自动化
4. 降级策略：若 OCS 不支持 Windows，仍支持 Claude Code Worker

---

## 非目标

- Windows Server 特定优化（暂不区分 Desktop/Server）
- CGO 交叉编译（保持 `CGO_ENABLED=0`）
- 32-bit Windows (`i386`) 支持

---

## 技术规范

### 1. 进程管理适配

**文件**: `internal/worker/proc/manager.go`

#### 1.1 进程组隔离

**当前实现** (POSIX):
```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid: true, // 创建独立进程组
}
```

**Windows 替代**:
```go
// Go 1.15+ 支持跨平台 Setpgid
if runtime.GOOS == "windows" {
    if err := cmd.Process.Setpgid(cmd.Process.Pid); err != nil {
        return nil, nil, nil, fmt.Errorf("proc: setpgid: %w", err)
    }
    m.pgid = cmd.Process.Pid // Windows 上 PGID = PID
} else {
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
    }
}
```

#### 1.2 进程终止

**当前实现**:
```go
// 向进程组发送 SIGTERM
syscall.Kill(-pgid, sig)
```

**Windows 替代**:
```go
func (m *Manager) killProcessGroup() error {
    if runtime.GOOS == "windows" {
        return m.killWindowsProcessTree(m.pgid)
    }
    return syscall.Kill(-m.pgid, syscall.SIGKILL)
}

// Windows: 递归终止进程树
func (m *Manager) killWindowsProcessTree(pid int) error {
    // 使用 golang.org/x/sys/windows 调用 EnumProcesses + TerminateProcess
    // 遍历找到所有子进程并终止
}
```

#### 1.3 内存限制

**当前实现**:
```go
if runtime.GOOS != "darwin" {
    syscall.Setrlimit(syscall.RLIMIT_AS, ...) // darwin 已跳过
}
```

**Windows 替代**: 使用 Windows Job Objects 或跳过

```go
if runtime.GOOS == "windows" {
    // Job Objects: 创建 job，限制内存
    // 或跳过：Go 在 Windows 上内存管理良好
} else if runtime.GOOS != "darwin" {
    syscall.Setrlimit(syscall.RLIMIT_AS, ...)
}
```

#### 1.4 进程状态

**当前实现**:
```go
if ws, ok := m.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
    m.exitCode = ws.ExitStatus()
}
```

**Windows 替代**:
```go
// Go 标准库已处理跨平台
m.exitCode = m.cmd.ProcessState.ExitCode()
```

---

### 2. Base Conn 适配

**文件**: `internal/worker/base/conn.go`

**当前实现**:
```go
func WriteAll(fd int, data []byte) error {
    nn, err := syscall.Write(fd, data[n:])
    // 处理 EAGAIN
}
```

**Windows 替代**:
```go
func WriteAll(fd int, data []byte) error {
    if runtime.GOOS == "windows" {
        // Windows 上 os.File.Write 已处理底层细节
        return writeAllNative(fd, data)
    }
    return writeAllSyscall(fd, data)
}

func writeAllNative(fd int, data []byte) error {
    f := os.NewFile(uintptr(fd), "")
    _, err := f.Write(data)
    return err
}
```

---

### 3. 路径安全验证

**文件**: `internal/security/path.go`

#### 3.1 禁止目录列表

**当前实现**: POSIX 路径硬编码

```go
var ForbiddenWorkDirs = []string{
    "/bin", "/sbin", "/usr", "/etc", "/System", "/dev", "/proc", "/sys", ...
}
```

**Windows 补充**:
```go
// 在 ValidateWorkDir 中根据 runtime.GOOS 返回对应列表
func GetForbiddenWorkDirs() []string {
    switch runtime.GOOS {
    case "windows":
        return []string{
            `C:\Windows`, `C:\Windows\System32`, `C:\Windows\SysWOW64`,
            `C:\Program Files`, `C:\Program Files (x86)`,
            `C:\System Volume Information`,
        }
    case "darwin":
        return []string{"/System", "/usr", "/bin", "/sbin", "/dev"}
    default: // linux
        return []string{"/bin", "/sbin", "/usr", "/etc", "/boot", "/lib",
                        "/lib64", "/root", "/home", "/dev", "/proc", "/sys", "/run", "/srv"}
    }
}
```

---

### 4. 配置路径适配

**文件**: `internal/config/config.go`

#### 4.1 HotplexHome()

**当前实现**:
```go
func HotplexHome() string {
    home, err := os.UserHomeDir()
    if err != nil || home == "" {
        return "/tmp/hotplex" // POSIX fallback
    }
    return filepath.Join(home, ".hotplex")
}
```

**Windows 替代**:
```go
func HotplexHome() string {
    switch runtime.GOOS {
    case "windows":
        if appdata := os.Getenv("APPDATA"); appdata != "" {
            return filepath.Join(appdata, "HotPlex")
        }
        // fallback to USERPROFILE
        if up := os.Getenv("USERPROFILE"); up != "" {
            return filepath.Join(up, "AppData", "Roaming", "HotPlex")
        }
    }

    home, err := os.UserHomeDir()
    if err != nil || home == "" {
        return "/tmp/hotplex"
    }
    return filepath.Join(home, ".hotplex")
}
```

#### 4.2 PID 目录

**Windows**: 使用 `HotplexHome()` 统一管理，不使用 POSIX `/tmp`

---

### 5. Worker 类型支持策略

| Worker 类型 | Windows 支持 | 说明 |
|-------------|--------------|------|
| Claude Code | ✅ 支持 | Claude Code 官方支持 Windows (WSL/原生) |
| OpenCode Server | ⚠️ 待确认 | 需验证 OCS 是否发布 Windows 二进制 |
| Pi-mono | ❌ 不支持 | 实验性协议，暂不考虑 |

**OCS 降级策略**:
```go
func (w *Worker) Start(...) error {
    if runtime.GOOS == "windows" && w.workerType == worker.TypeOpenCodeSrv {
        return fmt.Errorf("opencode-server: not supported on Windows")
    }
    // ... 正常启动逻辑
}
```

---

### 6. CI/CD 改动

**文件**: `.github/workflows/release.yml`

#### 6.1 构建矩阵

```yaml
jobs:
  build:
    strategy:
      matrix:
        os: [darwin, linux, windows]
        arch: [amd64, arm64]
    runs-on: ${{ matrix.os == 'windows' && 'windows-latest' || 'ubuntu-latest' }}
```

#### 6.2 Windows 构建步骤

```yaml
- name: Build Windows
  if: matrix.os == 'windows'
  shell: pwsh
  run: |
    $EXT = ".exe"
    $NAME = "hotplex-windows-${{ matrix.arch }}${EXT}"
    New-Item -ItemType Directory -Force -Path dist | Out-Null
    go build -ldflags="${LDFLAGS}" -o "dist/${NAME}" ./cmd/hotplex

- name: Checksum Windows
  if: matrix.os == 'windows'
  shell: pwsh
  run: |
    $EXT = ".exe"
    $NAME = "hotplex-windows-${{ matrix.arch }}${EXT}"
    (Get-FileHash "dist/${NAME}" -Algorithm SHA256).Hash.ToLower() + "  ${NAME}" | Add-Content dist/checksums.txt
```

---

### 7. 新增文件清单

| 文件路径 | 用途 |
|----------|------|
| `scripts/release-windows.ps1` | Windows Release 构建脚本 |
| `scripts/dev-windows.ps1` | Windows 开发环境脚本 |
| `internal/worker/proc/platform.go` | 平台特定进程管理抽象 |
| `internal/security/path_windows.go` | Windows 禁止目录 (build tag) |
| `internal/security/path_unix.go` | Unix 禁止目录 (build tag) |

---

### 8. 测试策略

#### 8.1 单元测试

```go
// internal/worker/proc/manager_test.go
// 现有测试需兼容 Windows (mock 进程调用)

// internal/security/path_test.go
func TestValidateWorkDir_Windows(t *testing.T) {
    if runtime.GOOS != "windows" {
        t.Skip("windows only")
    }
    // Windows 特定测试
}
```

#### 8.2 CI 集成测试

```yaml
- name: Windows Integration Tests
  if: matrix.os == 'windows'
  run: |
    go test -v -timeout 10m ./internal/...
  shell: pwsh
```

---

### 9. 文档更新

| 文档 | 更新内容 |
|------|----------|
| `README.md` | 添加 Windows 安装指南 |
| `docs/architecture/*.md` | 添加平台兼容性说明 |
| `CLAUDE.md` | 更新 COMMANDS 章节 |

---

## 实施计划

### Phase 1: 代码适配 (1-2天)

- [ ] 抽象进程管理接口，隔离 `runtime.GOOS` 判断
- [ ] 适配 `proc/manager.go` Windows 实现
- [ ] 适配 `base/conn.go` Windows 实现
- [ ] 更新路径安全验证
- [ ] 更新配置路径逻辑

### Phase 2: CI/CD (0.5天)

- [ ] 修改 `release.yml` 添加 Windows 矩阵
- [ ] 创建 PowerShell 构建脚本
- [ ] 验证 GitHub Actions Windows runner

### Phase 3: 测试验证 (0.5天)

- [ ] Windows 虚拟机/容器测试
- [ ] 端到端功能验证
- [ ] 性能基准对比

### Phase 4: 文档 (0.5天)

- [ ] 更新 README
- [ ] 更新架构文档
- [ ] 添加安装指南

---

## 风险与依赖

### 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| OCS 不支持 Windows | 中 | Claude Code Worker 仍可用 |
| 进程终止逻辑复杂化 | 低 | 抽象接口隔离 |
| 测试覆盖不足 | 中 | 添加 Windows CI |

### 依赖

| 依赖 | 状态 | 说明 |
|------|------|------|
| Go 1.26 | ✅ 就绪 | 支持所有所需特性 |
| Claude Code Windows | ✅ 就绪 | 官方支持 |
| OpenCode Server Windows | ⚠️ 待确认 | 需向官方确认 |

---

## 验收标准

1. `go build -o hotplex-windows-amd64.exe ./cmd/hotplex` 成功编译
2. Windows amd64 二进制通过 `go test ./...`
3. GitHub Release 生成 `hotplex-windows-amd64.exe` 和 `hotplex-windows-arm64.exe`
4. SHA256 checksum 验证通过
5. 基础功能 (WebSocket gateway, session 管理) 在 Windows 上正常运行
6. 文档更新完成

---

## 附录

### A. Windows 系统目录参考

| 路径 | 说明 |
|------|------|
| `C:\Windows` | Windows 系统根目录 |
| `C:\Windows\System32` | 64-bit 系统二进制 |
| `C:\Windows\SysWOW64` | 32-bit 兼容层 |
| `C:\Program Files` | 64-bit 程序 |
| `C:\Program Files (x86)` | 32-bit 程序 |
| `C:\System Volume Information` | 系统还原点 |

### B. Windows 环境变量

| 变量 | 说明 |
|------|------|
| `APPDATA` | 用户应用数据 |
| `USERPROFILE` | 用户目录 |
| `TEMP` / `TMP` | 临时目录 |
| `LOCALAPPDATA` | 本地应用数据 |
