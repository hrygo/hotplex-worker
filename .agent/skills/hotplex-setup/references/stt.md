# STT（语音转文字）详细配置

## 飞书云端 STT 权限申请

### 权限申请链接
```
https://open.feishu.cn/app/cli_a954eab23678dbb5/auth?q=speech_to_text:speech&op_from=openapi&token_type=tenant
```

### 申请步骤
1. 点击链接进入飞书应用权限管理
2. 找到 `speech_to_text:speech` 权限
3. 点击"申请"并开通
4. 权限生效后重启 Gateway：
   ```bash
   systemctl --user restart hotplex.service
   ```

### 云端 STT 优势
- 无需本地下载模型（节省 900MB 空间）
- 转换速度更快（云端处理）
- 支持更多方言和语言
- 无需本地 Python 环境

## 本地 STT 依赖安装

### 安装 Python 包
```bash
# 国际用户
pip3 install -U funasr-onnx modelscope

# 中国用户（推荐镜像加速）
pip3 install -U funasr-onnx modelscope -i https://mirror.sjtu.edu.cn/pypi/web/simple
```

### 下载 SenseVoice 模型
```bash
# 方式 1：Python 下载（推荐）
python3 -c "from modelscope.hub.snapshot_download import snapshot_download; snapshot_download('iic/SenseVoiceSmall', cache_dir='/home/hotplex/.cache/modelscope')"

# 方式 2：首次使用自动下载（Gateway 自动触发）
# 模型会自动下载到 ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/
```

### 模型信息
- **大小**：约 900MB
- **存储位置**：`~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`
- **支持语言**：中文、英文、粤语、日语、韩语
- **模型类型**：ONNX FP32（非量化）

### 验证安装
```bash
# 检查 Python 包
python3 -c "import funasr_onnx, modelscope" && echo "✅ STT 包已安装"

# 检查模型
test -d ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall && echo "✅ 模型已下载"

# 查看模型大小
du -sh ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall
```

## STT 脚本部署

### 脚本位置
```bash
~/.hotplex/scripts/stt_once.py   # 临时模式（按请求启动）
~/.hotplex/scripts/stt_server.py  # 持久化模式（常驻子进程）
~/.hotplex/scripts/fix_onnx_model.py  # ONNX 模型修补脚本
```

### 部署脚本
```bash
# 检查脚本
ls -lh ~/.hotplex/scripts/stt_*.py

# 如不存在，从项目复制
cp scripts/stt_once.py scripts/stt_server.py scripts/fix_onnx_model.py ~/.hotplex/scripts/
chmod +x ~/.hotplex/scripts/stt_*.py
```

## STT 配置参数

### 环境变量

**Slack（仅本地）**：
```bash
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=ephemeral
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_CMD=python3 ~/.hotplex/scripts/stt_once.py
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_IDLE_TTL=1h
```

**飞书（云端 + 本地降级）**：
```bash
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_CMD=python3 ~/.hotplex/scripts/stt_server.py
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_IDLE_TTL=1h
```

### 本地模式选项

| 模式 | 说明 | 优势 | 劣势 |
|------|------|------|------|
| `ephemeral` | 按请求启动进程 | 节省内存 | 首次转写有延迟 |
| `persistent` | 常驻子进程 | 预热后延迟更低 | 占用内存 |

### 推荐配置
- **低频使用**：`ephemeral`（默认）
- **高频使用**：`persistent`（延迟降低 50%）

## STT 测试验证

### 测试脚本
```bash
# 测试 STT（需要音频文件）
python3 ~/.hotplex/scripts/stt_once.py /path/to/audio.mp3

# 预期输出（JSON）：
# {"text":"转换结果","language":"zh","emotion":"NEUTRAL","event":"Speech","error":""}
```

### Gateway 日志验证
```bash
# 查看 STT 日志
journalctl --user -u hotplex.service -f | grep -E "stt|STT"

# 成功日志示例：
# feishu: stt transcription successful, text="..."
# slack: local stt result received

# 失败日志示例：
# stt: primary failed, trying fallback
# feishu: stt failed, saving audio to disk
```

## 常见问题

### Q: 飞书云端 STT 失败？
**A**: 检查权限是否已申请：
```bash
# 错误日志：code=99991672 msg=Access denied
# 解决：申请 speech_to_text:speech 权限
```

### Q: 本地 STT 模型下载慢？
**A**: 使用镜像加速：
```bash
pip3 install -U modelscope -i https://mirror.sjtu.edu.cn/pypi/web/simple
```

### Q: 本地 STT 转换失败？
**A**: 检查依赖和模型：
```bash
# 1. 检查 Python 包
python3 -c "import funasr_onnx, modelscope"

# 2. 检查模型
test -d ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall

# 3. 检查脚本
ls -lh ~/.hotplex/scripts/stt_once.py
```

### Q: STT 占用内存过高？
**A**: 使用 `ephemeral` 模式：
```bash
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral
```

### Q: 如何禁用 STT？
**A**: 设置 `stt_provider` 为空：
```bash
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=
```
