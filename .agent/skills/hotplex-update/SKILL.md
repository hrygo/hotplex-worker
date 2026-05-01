---
name: hotplex-update
description: 需要更新 HotPlex 二进制时使用此 skill。当你说"更新 hotplex"、"部署新版本"、"重启服务"、"安装最新构建"、"升级二进制"、"回滚版本"时触发。也适用于构建后部署、git pull 后更新、服务升级失败恢复等场景。提供完整工作流：构建、安装、服务重启、验证、错误处理和回滚机制。支持用户级和系统级服务，跨平台兼容（Linux/macOS/Windows）。
---

# HotPlex 更新与服务重启工作流

## 概述

此 skill 提供完整的 HotPlex 二进制更新工作流。它帮助你安全地部署新编译的代码，确保服务正常运行，并在出现问题时提供回滚机制。整个过程通常只需 3-5 秒停机时间。

## 为什么需要此 Skill

更新运行中的服务二进制看似简单，但有几个常见陷阱：
- **Text file busy 错误**：如果服务还在运行就无法替换二进制文件
- **服务启动失败**：新二进制可能有运行时错误
- **版本混淆**：不确定是否真的部署了新版本
- **无法回滚**：更新失败后没有快速恢复方案

此 skill 通过标准化流程避免这些问题，每次更新都成功。

## 前置条件

- 已安装并配置 `hotplex` CLI
- 已安装 `make` 和 `go` 1.26+
- 已启用 systemd 用户级服务（`hotplex service install --level user`）
- 对 `/home/hotplex/.local/bin/` 的写入权限

## 何时使用此 Skill

**直接触发场景**（直接调用此 skill）：
- 你说"更新 hotplex"、"部署新版本"、"重启服务"
- 你说"安装最新构建"、"升级二进制"、"回滚版本"
- 刚执行了 `make build` 需要部署
- 刚执行了 `git pull` 需要更新代码
- 服务升级失败需要恢复

**隐式触发场景**（工作流中自动使用）：
- 构建流程的最后一步
- CI/CD 部署阶段
- 版本回滚操作

## 工作流步骤

### 步骤 1：构建新二进制

首先编译最新源代码，这会创建一个包含你最新更改的二进制文件。

```bash
make build
```

**预期输出：**
```
Building...
  ✓ bin/hotplex-linux-amd64
```

**如果构建失败怎么办：**
- 检查编译错误信息并修复代码问题
- 确认 Go 版本是否满足要求（1.26+）
- 检查是否有缺少的依赖项
- 修复后重新运行 `make build`

---

### 步骤 2：验证二进制时间戳

确认刚构建的二进制确实比当前运行的版本新。

```bash
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**预期输出：**
- `./bin/hotplex-linux-amd64`：最近时间戳（刚刚构建的）
- `/home/hotplex/.local/bin/hotplex`：较旧时间戳（当前运行的版本）

**为什么要验证：**
- 确认构建成功生成了新文件
- 避免意外部署旧版本
- 建立更新前的基线对比

---

### 步骤 3：停止服务

在替换二进制之前需要停止服务，这是因为运行中的进程会锁定二进制文件，导致替换失败。

```bash
hotplex service stop
```

**预期输出：**
```
✓ Stopped service (user)
```

**如果服务未运行：**
- 这很正常，继续到下一步即可（命令是幂等的）

**如果服务停止失败：**
- 检查服务状态：`hotplex service status`
- 查看系统日志：`journalctl --user -u hotplex -n 20`
- 可能需要强制停止：`systemctl --user kill hotplex`

**等待清理：**
```bash
sleep 2
```

**为什么要等待：**
- Systemd 需要 1-2 秒完全释放文件句柄
- 避免后续复制时出现 "Text file busy" 错误
- 这是最常见的更新失败原因，等待可以避免

---

### 步骤 4：替换二进制

现在可以安全地将新二进制复制到系统位置。

```bash
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**预期输出：**（成功时静默，无消息）

**如果出现 "Text file busy" 错误：**
- 服务还没有完全停止
- 返回步骤 3，等待更长时间（可以尝试 `sleep 3`）
- 也可以检查进程：`ps aux | grep hotplex`

**如果出现 "Permission denied" 错误：**
- 检查目录权限：`ls -la /home/hotplex/.local/bin/`
- 确认你对目标目录有写入权限

**验证替换成功：**
```bash
ls -lh /home/hotplex/.local/bin/hotplex
```

**确认更新：**
- 时间戳应该显示为最近（刚刚）
- 文件大小应该与 `./bin/hotplex-linux-amd64` 一致

---

### 步骤 5：启动服务

二进制替换完成，现在启动服务使用新版本。

```bash
hotplex service start
```

**预期输出：**
```
✓ Service started (user)
```

**如果服务启动失败：**
- 立即检查日志：`hotplex service logs`
- 常见问题包括：
  - 端口 8888 或 9999 被占用
  - 配置文件有语法错误
  - 新代码有运行时错误
  - 缺少必要的依赖或环境变量

**调试提示：**
- 查看详细日志：`hotplex service logs -n 50`
- 检查系统日志：`journalctl --user -u hotplex -n 50`
- 如果问题严重，可以回滚到旧版本（见下文回滚程序）

---

### 步骤 6：验证服务状态

确认服务正在运行，并且使用的是新二进制。

```bash
hotplex service status
```

**预期输出：**
```
✓ hotplex (user) active
    PID: <new PID>
    Unit: /home/hotplex/.config/systemd/user/hotplex.service
```

**关键检查点：**
- **状态**：应该显示 `active`（不是 `failed` 或 `inactive`）
- **PID**：应该与更新前不同（证明服务确实重启了）
- **Unit 路径**：确认是正确的 systemd 用户服务

**如果状态不是 active：**
- `failed`：服务启动时遇到错误，检查日志
- `inactive`：服务没有启动，手动尝试启动
- `activating`：服务正在启动，等待几秒后重新检查

---

### 步骤 7：验证服务健康

服务状态显示 active 并不意味着一切正常，还需要检查日志确认服务真正启动成功。

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

**成功的标志：**
- ✅ Gateway banner 正确显示
- ✅ 最后 20 行中没有错误消息
- ✅ 至少一个适配器（Feishu）显示连接成功或正在连接
- ✅ Gateway 显示在端口 8888 上监听

**需要注意的问题：**
- ❌ 日志中出现 "panic"、"fatal"、"error" 字样
- ❌ 适配器连接失败（Feishu 显示 ✗）
- ❌ 端口绑定错误（"address already in use"）

**如果发现错误：**
- 查看更多日志：`hotplex service logs -n 100`
- 查看系统日志：`journalctl --user -u hotplex -n 50`
- 考虑回滚到旧版本（如果你有备份）

---

### 步骤 8：功能验证（可选但推荐）

如果更新包含特定功能改进，最好验证它们确实按预期工作。

**对于安全策略更新：**
```bash
# 在 Feishu 中测试 cd 命令
/cd ~/.hotplex/workspace/hotplex
# 应该使用新的安全策略验证
```

**对于错误消息改进：**
```bash
# 在 Feishu 中测试无效目录
/cd /etc/myapp
# 应该显示改进后的详细错误消息
```

**对于新功能：**
- 根据具体更新内容设计测试用例
- 验证新功能在真实场景中正常工作
- 确认没有破坏现有功能

---

## 回滚程序（更新失败时）

如果新版本有问题，可以快速回滚到之前的版本。

### 1. 停止服务
```bash
hotplex service stop
```

### 2. 恢复先前二进制

**如果你有备份**（推荐做法）：
```bash
cp /tmp/hotplex.backup.<timestamp> /home/hotplex/.local/bin/hotplex
```

**或者从 Git 历史重新构建**：
```bash
# 找到之前的 commit
git log --oneline -5

# 切换到之前的 commit
git checkout <previous-commit-hash>

# 重新构建
make build

# 复制旧版本
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### 3. 重启服务
```bash
hotplex service start
```

### 4. 验证回滚成功
```bash
hotplex service status
hotplex service logs | tail -20
```

**回滚后别忘了：**
- 如果切换了 git commit，记得返回到正确的分支
- 记录问题原因以便后续修复
- 在开发环境测试修复后再部署

---

## 最佳实践

### 1. 替换前备份（强烈推荐）
```bash
cp /home/hotplex/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)
```

**为什么重要：**
- 如果新版本有严重问题，可以立即回滚
- 无需重新构建或查找 git commit
- 只需几秒钟，但能节省大量故障排除时间

### 2. 使用 `cp -f` 强制标志
```bash
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**为什么使用 `-f`：**
- 覆盖目标文件而不提示
- 在自动化脚本中很重要
- 配合 `service stop` 确保替换成功

### 3. 停止后等待清理
```bash
hotplex service stop
sleep 2  # 重要！
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**为什么不能省略等待：**
- Systemd 需要时间释放文件句柄
- 进程可能需要几秒才完全终止
- 这是最常见的 "Text file busy" 错误原因

### 4. 始终验证时间戳
```bash
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

**为什么要比较：**
- 确认你真的在部署新版本
- 避免意外重新部署旧版本
- 建立更新前后对比基线

### 5. 启动后检查日志
```bash
hotplex service start
sleep 2 && hotplex service logs | tail -20
```

**为什么不能假设成功：**
- 服务状态显示 `active` 不代表服务正常
- 可能有初始化错误或配置问题
- 日志会显示真实的服务健康状况

---

## 常见问题和故障排除

### 问题 1：复制二进制时出现 "Text file busy"

**症状：**
```
cp: cannot create regular file '/home/hotplex/.local/bin/hotplex': Text file busy
```

**原因：**
- 服务还没有完全停止
- 文件仍被进程锁定
- Systemd 还在释放文件句柄

**解决方案：**
```bash
# 1. 确认服务已停止
hotplex service status

# 2. 如果还在运行，强制停止
systemctl --user stop hotplex

# 3. 等待更长时间
sleep 3

# 4. 检查进程
ps aux | grep hotplex

# 5. 如果仍有进程，杀死它
pkill -9 hotplex

# 6. 再次尝试复制
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
```

### 问题 2：更新后服务启动失败

**症状：**
```
✗ Failed to start service
```
或状态显示 `failed`

**可能原因：**
- 新二进制有运行时错误
- 配置文件格式错误
- 端口被其他进程占用
- 缺少必要的依赖或环境变量

**解决方案：**
```bash
# 1. 检查详细日志
hotplex service logs -n 50

# 2. 检查系统日志
journalctl --user -u hotplex -n 50

# 3. 如果问题严重，立即回滚
hotplex service stop
cp /tmp/hotplex.backup.<timestamp> /home/hotplex/.local/bin/hotplex
hotplex service start

# 4. 在开发环境调试并修复问题
# 5. 修复后重新构建并部署
```

### 问题 3：更新后旧版本仍在运行

**症状：**
- 服务状态显示 active
- 但新功能不工作
- 版本号或行为与更新前相同

**可能原因：**
- 二进制替换失败（权限、路径错误）
- Systemd 缓存了旧二进制
- 服务实际上没有重启

**解决方案：**
```bash
# 1. 验证二进制时间戳
ls -lh /home/hotplex/.local/bin/hotplex

# 2. 如果时间戳是旧的，重新替换
hotplex service stop
sleep 2
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# 3. 如果时间戳是新的，强制重新加载 systemd
systemctl --user daemon-reload
hotplex service restart

# 4. 验证 PID 已改变
hotplex service status
```

### 问题 4：服务启动成功但新功能不工作

**症状：**
- 服务状态 active
- 日志中没有错误
- 但新功能表现异常或不存在

**可能原因：**
- 代码更改没有正确构建
- 服务需要完全重启（不仅仅是 start）
- 配置文件没有重新加载
- 缓存问题

**解决方案：**
```bash
# 1. 完全重启服务
hotplex service restart

# 2. 清除可能的缓存
rm -rf /home/hotplex/.hotplex/cache/*

# 3. 验证配置已加载
hotplex service logs | grep -i "version\|config\|security"

# 4. 如果还不行，重新构建并部署
make build
hotplex service stop
sleep 2
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex
hotplex service restart
```

### 问题 5：更新后服务频繁重启

**症状：**
- 服务状态在 active 和 inactive 之间切换
- 日志显示重复的启动/停止循环
- PID 不断变化

**可能原因：**
- 代码有 panic 或 fatal 错误
- 资源耗尽（内存、文件描述符）
- 健康检查失败
- 依赖服务不可用

**解决方案：**
```bash
# 1. 立即停止服务防止继续循环
hotplex service stop

# 2. 查看完整日志找出崩溃原因
hotplex service logs -n 200

# 3. 回滚到稳定版本
cp /tmp/hotplex.backup.<timestamp> /home/hotplex/.local/bin/hotplex
hotplex service start

# 4. 分析日志修复问题
# 5. 修复后重新部署
```

---

## 快速参考命令序列

对于熟悉流程的用户，这里是完整的工作流命令序列：

```bash
# 1. 构建新版本
make build

# 2. 验证时间戳
ls -lh ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# 3. 停止服务并等待
hotplex service stop
sleep 2

# 4. 替换二进制
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex

# 5. 启动服务
hotplex service start

# 6. 验证状态和日志
hotplex service status
sleep 2 && hotplex service logs | tail -20
```

**带备份的版本（推荐用于生产环境）：**

```bash
# 备份当前版本
cp /home/hotplex/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)

# 构建并部署
make build && \
hotplex service stop && \
sleep 2 && \
cp -f ./bin/hotplex-linux-amd64 /home/hotplex/.local/bin/hotplex && \
hotplex service start && \
sleep 2 && hotplex service logs | tail -20
```

---

## 重要注意事项

### 停机时间和影响
- **停机时间**：通常 3-5 秒（停止 + 替换 + 启动）
- **用户影响**：重启期间所有活动会话都将终止
- **数据安全**：重启不会丢失数据，但会中断正在进行的请求

### 安全性建议
- **始终备份**：替换前备份当前二进制
- **测试环境**：先在开发环境测试，再部署到生产
- **版本标记**：使用 Git tag 或 commit hash 追踪版本

### 日志和调试
- **服务日志**：`hotplex service logs` 查看应用日志
- **系统日志**：`journalctl --user -u hotplex` 查看 systemd 日志
- **日志级别**：可以通过配置调整日志详细程度

### 权限和服务级别
- **用户级服务**：使用 systemd 用户服务，不需要 root 权限
- **服务位置**：`~/.config/systemd/user/hotplex.service`
- **二进制位置**：`~/.local/bin/hotplex`

### 跨平台兼容性
- **Linux**：完全支持，使用 systemd 用户服务
- **macOS**：支持，使用 launchd 替代 systemd
- **Windows**：支持，使用 nssm 或 srvany 作为服务包装器
