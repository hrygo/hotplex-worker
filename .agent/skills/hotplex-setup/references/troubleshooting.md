# HotPlex 故障排查指南

`hotplex doctor` 报告异常时，按本文档排查。

## 快速排查流程

```bash
# 1. 运行完整诊断
hotplex doctor --json

# 2. 查看日志
hotplex service logs -n 50

# 3. 验证配置
hotplex config validate
```

---

## 按分类排查

### 端口冲突（runtime.port_available）

**症状**：8888 或 9999 端口被占用。

```bash
# 检测
lsof -i :8888  # Linux/macOS
netstat -ano | findstr ":8888"  # Windows

# 解决方式 1：停止占用进程
kill <PID>  # Linux/macOS
taskkill /PID <PID> /F  # Windows

# 解决方式 2：修改 HotPlex 端口
# 编辑 config.yaml:
# gateway:
#   addr: ":8889"
# admin:
#   addr: ":9998"
```

### 权限问题（security.file_permissions）

**config 文件权限过宽**：
```bash
chmod 600 ~/.hotplex/.env ~/.hotplex/config.yaml
chmod 700 ~/.hotplex
```

**二进制无执行权限**：
```bash
chmod +x $(which hotplex)
```

### 服务启动失败

**systemd (Linux)**：
```bash
systemctl --user status hotplex
journalctl --user -u hotplex -n 50
systemctl --user restart hotplex
```

**launchd (macOS)**：
```bash
launchctl list | grep hotplex
log show --predicate 'process == "hotplex"' --last 10m
launchctl unload ~/Library/LaunchAgents/hotplex.plist
launchctl load ~/Library/LaunchAgents/hotplex.plist
```

**SCM (Windows)**：
```powershell
Get-Service hotplex
Restart-Service hotplex
hotplex service logs -n 50
```

**常见原因**：
1. 配置文件错误 → `hotplex config validate`
2. 端口冲突 → 见上文
3. 环境变量缺失 → 检查 `.env` 文件
4. 二进制路径不在 PATH → `which hotplex`

### 消息平台连接失败

#### Slack（messaging.slack_creds）

**症状**：日志显示 "Slack: WebSocket connection failed"

**检查清单**：
1. Token 格式：Bot Token 以 `xoxb-` 开头，App Token 以 `xapp-` 开头
2. Token 有效性：
   ```bash
   curl -s -H "Authorization: Bearer <BOT_TOKEN>" "https://slack.com/api/auth.test"
   ```
3. Socket Mode 已启用（Slack App 配置）
4. 网络可达（防火墙/代理）

#### 飞书（messaging.feishu_creds）

**症状**：日志显示 "Feishu: WebSocket connection failed"

**检查清单**：
1. App ID 以 `cli_` 开头，Secret 非空
2. 凭据有效性：
   ```bash
   curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
     -H "Content-Type: application/json" \
     -d '{"app_id":"<APP_ID>","app_secret":"<APP_SECRET>"}'
   ```
3. 事件订阅已配置（飞书开放平台）
4. 必要权限已授予

### Worker 启动失败（dependencies.worker_binary）

**Claude Code 找不到**：
```bash
which claude
# 设置完整路径
export HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=/full/path/to/claude
```

**OpenCode Server 崩溃**：
```bash
pkill -f "opencode serve"
hotplex service restart
```

### STT 问题

详见 `references/stt.md`。

常见错误：
- 本地 STT：检查 funasr-onnx、modelscope、模型下载
- 飞书云端 STT：权限管理 → 搜索 `speech_to_text`

### TTS 问题

详见 `references/tts.md`。

常见错误：
- ffmpeg 未安装 → 安装后重试
- 语音消息过长 → 检查 `tts_max_chars` 设置

### 安全问题

| checker | 问题 | 修复 |
|---------|------|------|
| `jwt_strength` | JWT secret 太弱 | `openssl rand -base64 48` 重新生成 |
| `admin_token` | 弱默认值 | 替换为强随机值 |
| `env_in_git` | .env 被 git 追踪 | `git rm --cached .env` |

---

## 跨平台特定问题

详见 `references/cross-platform.md`。

---

## 获取帮助

如果以上方法都无法解决：

1. 运行完整诊断：`hotplex doctor -v`
2. 导出日志：`hotplex service logs -n 200 > /tmp/hotplex-debug.log`
3. 提交 Issue，附上：
   - 版本：`hotplex version`
   - 系统：`uname -a`
   - 诊断输出
   - 日志（敏感信息已脱敏）
