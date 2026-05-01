---
name: hotplex-update
description: HotPlex 二进制更新、发布和服务重启标准化流程。构建、安装、服务重启、验证，完整错误处理和回滚机制。**使用此 skill**：更新 HotPlex、安装新版本、重启服务、回滚版本、服务升级。支持用户级和系统级服务，跨平台兼容（Linux/macOS/Windows）。
---

# HotPlex 更新与服务重启工作流

## 概述

此 skill 提供**标准化、错误安全的工作流**，用于将 HotPlex 更新到新的二进制版本并重启服务。它确保部署最新编译的代码，最小化停机时间。

## 前置条件

- 已安装并配置 `hotplex` CLI
- 已安装 `make` 和 `go` 1.26+
- 已启用 systemd 用户级服务（`hotplex service install --level user`）
- 对 `/home/hotplex/.local/bin/` 的写入权限

## 何时使用此 Skill

在以下情况下调用此 skill：
- 用户说"安装新版本"、"更新二进制"、"部署最新代码"
- 用户说"用新更改重启服务"
- 在使用 `make build` 构建新代码后
- 从 git 拉取最新更改后
- **任何涉及二进制更新和服务重启的场景**

## 工作流步骤

### 步骤 1：构建新二进制

编译最新源代码：

```bash
make build
```

**预期输出：**
```
Building...
  ✓ bin/hotplex-linux-amd64
```

**错误处理：**
- 如果构建失败：先修复编译错误，然后重试
- 检查：`go build` 错误、缺少依赖、语法问题

---

### 步骤 2：验证二进制时间戳

确认刚构建的新二进制：

```bash
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**预期输出：**
- `./bin/hotplex-linux-amd64`：最近时间戳（刚刚）
- `/home/hotplex/.local/bin/hotplex`：较旧时间戳（先前版本）

**这告诉我们什么：**
- 确认我们有更新的二进制要部署
- 显示我们将要关闭的版本差距

---

### 步骤 3：停止服务

**关键：**必须在替换二进制之前停止服务以避免"Text file busy"错误。

```bash
hotplex service stop
```

**预期输出：**
```
✓ Stopped service (user)
```

**错误处理：**
- 如果服务未运行：继续到下一步（幂等）
- 如果服务停止失败：检查 `hotplex service status` 和 `journalctl --user -u hotplex`

**等待清理：**
```bash
sleep 2
```

**为什么等待：**Systemd 可能需要 1-2 秒来完全释放二进制文件锁。

---

### 步骤 4：替换二进制

将新构建的二进制复制到系统位置：

```bash
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**预期输出：**（成功时静默）

**错误处理：**
- 如果 `Text file busy`：服务未完全停止。返回步骤 3 并等待更长时间。
- 如果 `Permission denied`：检查对 `/home/hotplex/.local/bin/` 的写入权限

**验证替换：**
```bash
ls -lh /home/hotplex/.local/bin/hotplex
```

**确认：**时间戳应该是最近的（刚刚），而不是旧时间戳。

---

### 步骤 5：启动服务

使用新二进制启动服务：

```bash
hotplex service start
```

**预期输出：**
```
✓ Service started (user)
```

**错误处理：**
- 如果服务启动失败：使用 `hotplex service logs` 检查日志
- 常见问题：端口冲突（8888/9999）、配置错误、缺少依赖

---

### 步骤 6：验证服务状态

确认服务正在使用新二进制运行：

```bash
hotplex service status
```

**预期输出：**
```
✓ hotplex (user) active
    PID: <new PID>
    Unit: /home/hotplex/.config/systemd/user/hotplex.service
```

**关键指示器：**
- 状态：`active`（而非 `failed` 或 `inactive`）
- PID：与更新前 PID 不同（确认重启）
- Unit：正确的 systemd 用户服务路径

---

### 步骤 7：验证服务健康

检查服务日志以确保干净启动：

```bash
sleep 2 && hotplex service logs | tail -20
```

**预期输出：**
```
HOTPLEX GATEWAY
Unified AI Coding Agent Access Layer
────────────────────────────────────────────────────────────
Version    v1.3.0
Gateway    http://:8888
Adapters   feishu ✓  slack ✗
{"time":"...","level":"INFO","msg":"feishu: starting WebSocket connection"...}
```

**成功指示器：**
- ✅ Banner 正确显示
- ✅ 最后 20 行中无错误消息
- ✅ Feishu 适配器显示"connected"（或"starting"）
- ✅ Gateway 在端口 8888 上监听

**错误指示器：**
- ❌ 日志中的"panic"、"fatal"、"error"
- ❌ 适配器连接失败
- ❌ 端口绑定错误

**遇到错误时：**
- 检查完整日志：`hotplex service logs -n 100`
- 检查系统日志：`journalctl --user -u hotplex -n 50`
- 回滚：在替换前保留旧二进制备份

---

### 步骤 8：功能验证（可选但推荐）

如果更新包括特定新功能，验证它们：

**对于安全策略更新：**
```bash
# 在 Feishu 中测试 cd 命令
/cd ~/.hotplex/workspace/hotplex
# 应该成功使用新的安全策略
```

**对于错误消息更新：**
```bash
# 在 Feishu 中测试无效目录
/cd /etc/myapp
# 应该显示详细错误消息
```

---

## 回滚程序（如果更新失败）

如果新二进制有问题，回滚到先前版本：

### 1. 停止服务
```bash
hotplex service stop
```

### 2. 恢复先前二进制
```bash
# 如果您有备份：
cp /path/to/backup/hotplex /home/hotplex/.local/bin/hotplex

# 或从先前 commit 重新构建：
git checkout <previous-commit>
make build
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### 3. 重启服务
```bash
hotplex service start
```

### 4. 验证回滚
```bash
hotplex service status
hotplex service logs | tail -20
```

---

## 最佳实践

### 1. 替换前备份
```bash
cp /home/hotplex/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)
```

### 2. 使用 `cp -f` 强制标志
防止服务未完全停止时的"Text file busy"错误。

### 3. 停止后始终等待
`service stop` 后的 `sleep 2` 可防止文件锁问题。

### 4. 验证时间戳
比较时间戳确认您正在部署正确的版本。

### 5. 启动后检查日志
不要假设成功 — 在日志中验证干净启动。

---

## 故障排除

### 问题：复制二进制时"Text file busy"
**原因：**服务仍在运行或文件锁未释放
**解决方案：**
```bash
hotplex service stop
sleep 3  # 等待更长时间
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### 问题：更新后服务启动失败
**原因：**新二进制有运行时错误
**解决方案：**检查日志，回滚到先前版本，修复问题，重新构建

### 问题：更新后旧版本仍在运行
**原因：**二进制替换失败或 systemd 缓存了旧二进制
**解决方案：**
```bash
# 验证二进制时间戳
ls -lh /home/hotplex/.local/bin/hotplex

# 如果时间戳是旧的，重复步骤 4
# 如果时间戳是新的但 PID 是旧的，重启 systemd
systemctl --user daemon-reload
hotplex service restart
```

### 问题：新功能不工作
**原因：**服务未完全重启或配置未加载
**解决方案：**
```bash
# 完全重启（不仅仅是启动）
hotplex service restart

# 验证配置已加载
hotplex service logs | grep "security\|allowed"
```

---

## 快速参考命令序列

对于有经验的用户，完整工作流：

```bash
# 构建
make build

# 验证
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# 停止并等待
hotplex service stop
sleep 2

# 替换
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# 启动
hotplex service start

# 验证
hotplex service status
sleep 2 && hotplex service logs | tail -20
```

---

## 注意事项

- **停机时间：**通常 3-5 秒（停止 + 替换 + 启动）
- **影响：**重启期间所有活动会话都将终止
- **安全性：**替换前始终备份先前的二进制
- **日志记录：**所有服务操作都记录到 systemd 日志
- **用户级：**使用 systemd 用户服务，不需要 root
