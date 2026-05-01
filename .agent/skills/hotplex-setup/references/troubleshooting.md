# HotPlex 故障排查指南

本文档详细说明 HotPlex 安装和使用过程中的常见问题及解决方案。

## 端口冲突

### 问题

Gateway 端口 8888 或 Admin API 端口 9999 已被占用。

### 检测方法

```bash
# Linux/macOS
netstat -tuln | grep -E ":(8888|9999)"
# 或
ss -tuln | grep -E ":(8888|9999)"

# Windows
netstat -ano | findstr ":8888"
netstat -ano | findstr ":9999"
```

### 解决方案

**方案 1：停止占用端口的进程**

```bash
# Linux/macOS - 找到进程 PID
lsof -i :8888
# 然后 kill <PID>

# Windows - 找到进程 PID
netstat -ano | findstr ":8888"
# 然后 taskkill /PID <PID> /F
```

**方案 2：修改 HotPlex 配置使用其他端口**

编辑 `~/.hotplex/config.yaml`：

```yaml
gateway:
  addr: ":8889"  # 改为其他端口

admin:
  addr: ":9998"  # 改为其他端口
```

---

## 权限问题

### 问题 1：无 ~/.hotplex 目录写入权限

**检测**：

```bash
touch ~/.hotplex/test-write 2>&1
```

**解决方案**：

```bash
# 修复目录权限
chmod 755 ~/.hotplex

# 或重新创建目录
mkdir -p ~/.hotplex
chmod 755 ~/.hotplex
```

### 问题 2：无服务安装权限（systemd/launchd）

**症状**：`hotplex service install` 失败，提示权限不足。

**解决方案**：

使用 **用户级服务**（无需 root）：

```bash
hotplex service install  # 默认用户级
```

而非系统级：

```bash
sudo hotplex service install --level system  # 需要 root
```

### 问题 3：二进制文件无执行权限

**症状**：`hotplex: permission denied`

**解决方案**：

```bash
chmod +x ~/.local/bin/hotplex
# 或
chmod +x ./bin/hotplex-$(go env GOOS)-$(go env GOARCH)
```

---

## 服务启动失败

### 问题 1：服务无法启动（systemd）

**检测**：

```bash
systemctl --user status hotplex
# 或
journalctl --user -u hotplex -n 50
```

**常见原因**：

1. **配置文件错误**：运行 `hotplex config validate` 检查
2. **端口冲突**：见上文
3. **环境变量缺失**：检查 `.env` 文件是否存在
4. **二进制路径错误**：`which hotplex` 确认在 PATH 中

**解决方案**：

```bash
# 1. 验证配置
hotplex config validate

# 2. 检查日志
hotplex service logs -n 50

# 3. 重启服务
hotplex service restart
```

### 问题 2：服务无法启动（launchd on macOS）

**检测**：

```bash
launchctl list | grep hotplex
# 或查看日志
log show --predicate 'process == "hotplex"' --last 10m
```

**解决方案**：

```bash
# 重新加载服务
launchctl unload ~/Library/LaunchAgents/hotplex.plist
launchctl load ~/Library/LaunchAgents/hotplex.plist

# 查看状态
hotplex service status
```

### 问题 3：服务无法启动（SCM on Windows）

**检测**：

```powershell
# PowerShell
Get-Service hotplex
```

**解决方案**：

```powershell
# 重新启动服务
Restart-Service hotplex

# 或手动启动
Start-Service hotplex

# 查看日志
hotplex service logs -n 50
```

---

## 消息平台连接失败

### Slack 连接失败

**症状**：日志显示 "Slack: WebSocket connection failed"

**检查清单**：

1. **Token 验证**：

```bash
curl -s -H "Authorization: Bearer <BOT_TOKEN>" "https://slack.com/api/auth.test"
```

应返回 `{"ok":true,...}`。

2. **App Level Token**：确保 `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` 以 `xapp-` 开头。

3. **Socket Mode 已启用**：在 Slack App 配置中确认 Socket Mode 已启用。

**解决方案**：

- Token 无效：重新生成 Bot Token 和 App Level Token
- Socket Mode 未启用：在 Slack App 设置中启用
- 网络问题：检查防火墙和代理设置

### 飞书连接失败

**症状**：日志显示 "Feishu: WebSocket connection failed"

**检查清单**：

1. **App ID/Secret 验证**：

```bash
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<APP_ID>","app_secret":"<APP_SECRET>"}'
```

应返回 `{"code":0,"tenant_access_token":"..."}`。

2. **事件订阅已配置**：在飞书开放平台确认事件订阅已启用。

3. **权限已授予**：确认应用已获得必要权限。

**解决方案**：

- 凭据无效：检查 APP_ID 和 APP_SECRET 是否正确
- 权限不足：在飞书开放平台授予必要权限
- 事件订阅未配置：按照飞书文档配置事件订阅

---

## STT（语音转文字）问题

详见 `references/stt.md`。

---

## Worker 启动失败

### Claude Code 找不到

**症状**：日志显示 "claude: command not found"

**解决方案**：

1. **检查 claude 是否在 PATH**：

```bash
which claude
```

2. **设置完整路径**：

```bash
export HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=/full/path/to/claude
```

或在 `.env` 中设置：

```
HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=/full/path/to/claude
```

### OpenCode Server 单例进程崩溃

**症状**：日志显示 "OCS singleton process died"

**解决方案**：

```bash
# 手动终止旧的单例进程
pkill -f "opencode serve"

# 重启 HotPlex 服务
hotplex service restart
```

---

## 配置文件问题

### .env 文件不存在

**症状**：`hotplex config validate` 失败，提示找不到配置

**解决方案**：

```bash
# 从示例复制
cp configs/env.example .env

# 编辑配置
nano .env  # 或使用其他编辑器
```

### 配置验证失败

**症状**：`hotplex config validate` 报错

**常见错误**：

1. **密钥格式错误**：确保 JWT_SECRET 和 ADMIN_TOKEN_1 是有效的 base64 字符串
2. **Token 格式错误**：
   - Slack Bot Token: 以 `xoxb-` 开头
   - Slack App Token: 以 `xapp-` 开头
   - 飞书 App ID: 以 `cli_` 开头
3. **工作目录不存在**：确保 WORK_DIR 指向的目录存在

**解决方案**：

运行 `hotplex config validate` 查看具体错误信息，逐一修复。

---

## 跨平台特定问题

详见 `references/cross-platform.md`。

---

## 日志查看

### 开发模式

```bash
make dev  # 日志直接输出到终端
```

### 服务模式

```bash
# 查看最新 50 行日志
hotplex service logs -n 50

# 实时查看日志
hotplex service logs -f

# Linux systemd 详细日志
journalctl --user -u hotplex -n 100

# macOS launchd 日志
log show --predicate 'process == "hotplex"' --last 10m

# Windows SCM 日志
hotplex service logs -n 50
```

---

## 获取帮助

如果以上方法都无法解决问题：

1. **运行诊断**：

```bash
hotplex doctor
```

2. **查看完整日志**：

```bash
hotplex service logs -n 200 > /tmp/hotplex-debug.log
```

3. **提交 Issue**：在 GitHub 上提交 Issue，附上：
   - HotPlex 版本：`hotplex version`
   - 操作系统：`uname -a`
   - 错误日志
   - 配置文件（敏感信息已脱敏）
