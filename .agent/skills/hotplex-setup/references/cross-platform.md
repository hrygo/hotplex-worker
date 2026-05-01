# HotPlex 跨平台兼容性指南

HotPlex 支持 **Linux、macOS、Windows** 三大平台。本文档说明各平台的特定差异和注意事项。

## 支持的平台

| 平台 | 架构 | 状态 | 备注 |
|------|------|------|------|
| Linux | x86_64/amd64 | ✅ 完全支持 | 推荐 Ubuntu 20.04+ / Debian 11+ |
| Linux | arm64/aarch64 | ✅ 完全支持 | 需要 ARM64 系统和工具链 |
| macOS | x86_64 | ✅ 完全支持 | macOS 10.15+ |
| macOS | arm64 (Apple Silicon) | ✅ 完全支持 | macOS 11+ (Big Sur+) |
| Windows | amd64/x86_64 | ✅ 完全支持 | Windows 10/11 |

**不支持**：
- ❌ ARMv7（32 位 ARM）
- ❌ 32 位系统（i386, i686）
- ❌ FreeBSD

---

## 安装差异

### Linux

**包管理器**：
- Ubuntu/Debian: `apt`
- CentOS/RHEL: `yum` / `dnf`
- Fedora: `dnf`

**系统服务**：使用 systemd

```bash
# 用户级服务（推荐）
hotplex service install

# 系统级服务
sudo hotplex service install --level system
```

**权限**：
- 用户级服务：无需 root
- 系统级服务：需要 sudo
- 端口 < 1024：需要 root（HotPlex 默认使用 8888/9999，无需 root）

### macOS

**包管理器**：推荐 Homebrew

```bash
# 安装依赖
brew install go python3 git
```

**系统服务**：使用 launchd

```bash
# 用户级服务（唯一选项）
hotplex service install
```

**权限**：
- macOS 仅支持用户级服务（LaunchAgents）
- 无需 sudo
- SIP（系统完整性保护）可能限制某些操作（如修改 /System 目录）

**Apple Silicon 特殊说明**：
- Rosetta 2 自动运行 x86_64 二进制
- 原生 arm64 二进制性能更好
- `uname -m` 输出 `arm64`

### Windows

**包管理器**：
- Chocolatey（可选）
- Scoop（可选）
- 手动下载安装器

**系统服务**：使用 SCM（Service Control Manager）

```powershell
# 需要管理员权限
hotplex service install
```

**权限**：
- 需要管理员权限安装服务
- 以管理员身份运行 PowerShell

**路径分隔符**：
- Windows 使用 `\`（反斜杠）
- 配置文件中建议使用 `/`（正斜杠），HotPlex 会自动转换
- 或使用 `filepath.Join()` 风格的相对路径

---

## 进程管理差异

### POSIX 信号处理（Linux/macOS）

HotPlex 使用 POSIX 信号管理进程：

```go
// 优雅终止（SIGTERM）
syscall.Kill(pid, syscall.SIGTERM)

// 强制终止（SIGKILL）
syscall.Kill(pid, syscall.SIGKILL)
```

**进程组隔离**：

```go
// 创建进程组
syscall.Setpgid(0, 0)

// 终止进程组（包括子进程）
syscall.Kill(-pgid, syscall.SIGTERM)
```

### Windows 进程管理

Windows 不支持 POSIX 信号，使用替代方法：

```go
// 终止进程
process.Kill(p)

// Job Object 隔离（替代进程组）
// Windows API 调用
```

**实现细节**：
- 代码中使用 `*_unix.go` 和 `*_windows.go` build tags 分离平台实现
- Windows 使用 `os/exec` 的 `Process.Kill()`

---

## 文件系统差异

### 路径分隔符

**推荐做法**：使用 `filepath.Join()` 而非硬编码

```go
// ✅ 正确（跨平台）
path := filepath.Join("dir", "file")

// ❌ 错误（仅 Linux/macOS）
path := "dir/file"

// ❌ 错误（仅 Windows）
path := "dir\\file"
```

### 用户主目录

```go
// ✅ 正确
homeDir, _ := os.UserHomeDir()

// ❌ 错误（硬编码）
homeDir := "/home/hotplex"  // Linux/macOS
homeDir := "C:\\Users\\hotplex"  // Windows
```

### 临时目录

```go
// ✅ 正确
tempDir := os.TempDir()

// ❌ 错误（硬编码）
tempDir := "/tmp"  // Linux/macOS
tempDir := "C:\\Temp"  // Windows
```

### 文件权限

**POSIX（Linux/macOS）**：

```go
// 创建目录（0755 = rwxr-xr-x）
os.MkdirAll(dir, 0755)
```

**Windows**：
- 文件权限通过 ACL（访问控制列表）管理
- `os.MkdirAll` 的权限参数在 Windows 上被忽略
- 不使用 `0700` 权限（macOS SIP 可能保护）

---

## 系统服务差异

### Linux (systemd)

**服务文件位置**：
- 用户级：`~/.config/systemd/user/hotplex.service`
- 系统级：`/etc/systemd/system/hotplex.service`

**管理命令**：

```bash
systemctl --user start hotplex      # 启动
systemctl --user stop hotplex       # 停止
systemctl --user restart hotplex    # 重启
systemctl --user status hotplex     # 状态
journalctl --user -u hotplex -f     # 日志
```

### macOS (launchd)

**服务文件位置**：`~/Library/LaunchAgents/hotplex.plist`

**管理命令**：

```bash
launchctl load ~/Library/LaunchAgents/hotplex.plist    # 启动
launchctl unload ~/Library/LaunchAgents/hotplex.plist  # 停止
launchctl list | grep hotplex                          # 状态
log show --predicate 'process == "hotplex"'            # 日志
```

### Windows (SCM)

**服务注册**：在 Windows 注册表中

**管理命令**（PowerShell）：

```powershell
Start-Service hotplex     # 启动
Stop-Service hotplex      # 停止
Restart-Service hotplex   # 重启
Get-Service hotplex       # 状态
```

HotPlex 提供 `hotplex service` 命令统一封装这些差异。

---

## 环境变量差异

### PATH 环境变量

**Linux/macOS**：

```bash
# 添加到 PATH
export PATH="$HOME/.local/bin:$PATH"

# 持久化（添加到 ~/.bashrc 或 ~/.zshrc）
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
```

**Windows**：

```powershell
# 添加到 PATH（当前会话）
$env:Path += ";C:\Users\hotplex\AppData\Local\Microsoft\WindowsApps"

# 持久化（系统设置）
# 系统属性 → 高级 → 环境变量 → 编辑 Path
```

### .env 文件

**所有平台**：HotPlex 使用 `.env` 文件管理配置

**位置**：
- 优先：`~/.hotplex/.env`（用户级）
- 备选：`./configs/.env`（项目级）

**格式**：跨平台一致

```bash
KEY=value
# 注释
KEY2="value with spaces"
```

---

## 网络差异

### 防火墙

**Linux (iptables/firewalld/ufw)**：

```bash
# Ubuntu (ufw)
sudo ufw allow 8888/tcp
sudo ufw allow 9999/tcp

# CentOS (firewalld)
sudo firewall-cmd --add-port=8888/tcp --permanent
sudo firewall-cmd --add-port=9999/tcp --permanent
sudo firewall-cmd --reload
```

**macOS (pf/AppFirewall)**：

```bash
# 系统偏好设置 → 安全性与隐私 → 防火墙
# 或使用 pfctl
```

**Windows (Windows Defender)**：

```powershell
# Windows Defender 防火墙 → 允许应用通过防火墙
# 或使用 PowerShell
New-NetFirewallRule -DisplayName "HotPlex Gateway" -Direction Inbound -LocalPort 8888 -Protocol TCP -Action Allow
New-NetFirewallRule -DisplayName "HotPlex Admin" -Direction Inbound -LocalPort 9999 -Protocol TCP -Action Allow
```

### 代理设置

**Linux/macOS**：

```bash
export HTTP_PROXY=http://proxy.example.com:8080
export HTTPS_PROXY=http://proxy.example.com:8080
```

**Windows**：

```powershell
$env:HTTP_PROXY="http://proxy.example.com:8080"
$env:HTTPS_PROXY="http://proxy.example.com:8080"
```

---

## 测试和 CI

### GitHub Actions

HotPlex CI 在所有三大平台上运行测试：

```yaml
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v3
      - name: Run tests
        run: make test
```

### 本地测试

**推荐流程**：

```bash
# 1. 代码质量检查
make quality

# 2. 构建
make build

# 3. 测试
make test

# 4. 运行网关（开发模式）
make dev
```

---

## 平台特定限制

### macOS

**SIP（系统完整性保护）**：
- 保护 `/System`、`/usr`、`/bin`、`/sbin` 目录
- 不能写入这些目录（即使有 root 权限）
- 解决方案：使用 `~/.hotplex/` 或 `/usr/local/`

**代码签名**：
- 未签名的二进制可能触发安全警告
- 解决方案：右键点击 → "打开" → "仍要打开"

### Windows

**路径长度限制**：
- MAX_PATH = 260 字符（传统限制）
- Windows 10+ 支持长路径（需要注册表配置）
- 解决方案：使用相对路径或缩短路径

**换行符**：
- Windows 使用 `\r\n` (CRLF)
- Linux/macOS 使用 `\n` (LF)
- Git 配置：`git config --global core.autocrlf true`

**WSL（Windows Subsystem for Linux）**：
- WSL1：行为类似 Linux，但使用 Windows 网络
- WSL2：完整 Linux 内核，独立网络
- HotPlex 在 WSL 中运行：使用 Linux 版本

---

## 调试技巧

### 查看 OS 和架构

```bash
# Linux/macOS/Windows (Git Bash)
uname -sm
# 输出示例：
# Linux x86_64
# Darwin arm64
# MINGW64_NT-10.0-19045 x86_64

# 查看 Go 感知的 OS 和架构
go env GOOS GOARCH
# 输出示例：
# linux amd64
# darwin arm64
# windows amd64
```

### 查看平台特定信息

```bash
# Linux
cat /etc/os-release

# macOS
sw_vers

# Windows
systeminfo | findstr /B /C:"OS Name" /C:"OS Version"
```

---

## 最佳实践

### 开发

1. **使用主开发平台**：Linux 或 macOS
2. **定期在 Windows 上测试**：确保跨平台兼容
3. **使用 `filepath.Join()`**：而非硬编码路径分隔符
4. **使用 `os.UserHomeDir()`**：而非硬编码主目录

### 部署

1. **Linux**：推荐用于生产环境
2. **macOS**：适合开发环境
3. **Windows**：适合开发环境，生产需谨慎

### CI/CD

1. **在所有三大平台上测试**：确保代码变更不破坏跨平台兼容
2. **使用 GitHub Actions matrix**：并行测试多平台
3. **使用 build tags**：分离平台特定代码

---

## 相关文档

- **依赖安装**：`references/dependencies.md`
- **故障排查**：`references/troubleshooting.md`
- **STT 配置**：`references/stt.md`
- **主文档**：`SKILL.md`
