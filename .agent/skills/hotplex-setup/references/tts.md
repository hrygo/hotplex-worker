# TTS（文字转语音）详细配置

## 架构概览

HotPlex TTS 管道：文本 → LLM Summary → Edge TTS (MP3) → ffmpeg (Opus) → 发送语音消息

**当前默认**：`tts_enabled: true`, `tts_provider: edge`, `max_chars: 150`

### 依赖映射

| 依赖 | 用途 | 必需场景 |
|------|------|----------|
| Edge TTS | Go 内置库，文本转 MP3 | `tts_enabled=true`（默认） |
| ffmpeg | MP3 → Opus 转码 | **Slack 和飞书均需要**（`tts_enabled=true` 时） |
| onnxruntime v1.17+ | Kokoro 本地推理 | `tts_provider=edge+kokoro`（可选） |
| espeak-ng | Kokoro G2P 文本转音素 | `tts_provider=edge+kokoro`（可选） |

## ffmpeg 安装

ffmpeg 是 TTS 的**必需依赖**——Edge TTS 生成 MP3 后必须转码为 Opus 格式才能发送为语音消息。

### macOS
```bash
brew install ffmpeg
ffmpeg -version | head -1
```

### Linux (Ubuntu/Debian)
```bash
sudo apt install -y ffmpeg
ffmpeg -version | head -1
```

### Windows (PowerShell)
```powershell
choco install ffmpeg -y
# 或: winget install Gyan.FFmpeg
ffmpeg -version
```

### 验证

```bash
which ffmpeg
ffmpeg -version | head -1
```

## TTS 配置

### 环境变量

**Slack**：
```bash
HOTPLEX_MESSAGING_SLACK_TTS_ENABLED=true
HOTPLEX_MESSAGING_SLACK_TTS_MAX_CHARS=150
```

**飞书**：
```bash
HOTPLEX_MESSAGING_FEISHU_TTS_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_TTS_MAX_CHARS=150
```

### 关键参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `tts_enabled` | `true` | 是否启用 TTS |
| `tts_provider` | `edge` | 语音引擎（edge / edge+kokoro） |
| `tts_max_chars` | `150` | Summary 最大字符数（约 37 秒语音，飞书 60 秒限制内） |
| `tts_summary_input_cap` | `2000` | 输入文本截断长度（送给 LLM 做 summary 的最大字符数） |
| `tts_voice` | 平台默认 | 语音音色选择 |

### 输入/输出区别

- **SummaryInputCap (2000)**：送给 LLM 做 summary 的**输入文本**截断长度
- **MaxChars (150)**：Summary **输出**的最大字符数（控制语音时长）

## Kokoro TTS（可选，本地 CPU 推理）

当需要离线或降低延迟时，可使用 Kokoro-82M 本地推理作为 Edge TTS 的补充。

### onnxruntime 安装

**macOS**：
```bash
brew install onnxruntime
pkg-config --modversion libonnxruntime
```

**Linux**：
```bash
sudo apt install -y libonnxruntime-dev
pkg-config --modversion libonnxruntime
```

**Windows**：
```powershell
choco install onnxruntime -y
where onnxruntime
```

手动安装（所有平台）：https://github.com/microsoft/onnxruntime/releases

Linux 需设置：`export LD_LIBRARY_PATH=/path/to/onnxruntime/lib:$LD_LIBRARY_PATH`

### espeak-ng 安装

**macOS**：
```bash
brew install espeak-ng
espeak-ng --version
```

**Linux**：
```bash
sudo apt install -y espeak-ng
espeak-ng --version
```

**Windows**：
```powershell
choco install espeak-ng -y
espeak-ng --version
```

### Kokoro 模型资产

| 文件 | 说明 | 大小 |
|------|------|------|
| `kokoro-v1.0.onnx` | 模型权重 | ~82MB |
| `voices/*.bin` | 音色向量 | 几 KB 每个 |
| `config/vocab.json` | 音素→Token 映射 | ~100KB |

模型文件首次使用时自动下载，或手动放置到 `~/.hotplex/assets/`。

### 配置 Kokoro

```bash
HOTPLEX_MESSAGING_SLACK_TTS_PROVIDER=edge+kokoro
# 或
HOTPLEX_MESSAGING_FEISHU_TTS_PROVIDER=edge+kokoro
```

## 故障排查

### ffmpeg 未找到

**症状**：TTS checker 报 `fail`，日志显示 "ffmpeg not found in PATH"

**解决**：安装 ffmpeg（见上文），确保在 PATH 中。systemd 服务需确认服务环境包含 ffmpeg 路径。

### 语音消息过长被截断

**症状**：飞书语音消息被截断

**原因**：飞书限制 60 秒语音，`max_chars=150` 约对应 37 秒，已留余量。如果手动调大了 `max_chars`，可能导致超限。

**解决**：恢复 `max_chars=150` 或降低到更保守的值。

### Kokoro onnxruntime CGO 找不到库

**症状**：运行时报 CGO 链接错误

**解决**：
```bash
# Linux
export LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH

# macOS
export DYLD_LIBRARY_PATH=/usr/local/lib:$DYLD_LIBRARY_PATH
```

### 禁用 TTS

```bash
HOTPLEX_MESSAGING_SLACK_TTS_ENABLED=false
HOTPLEX_MESSAGING_FEISHU_TTS_ENABLED=false
```

## 相关文档

- **依赖安装**：`references/dependencies.md`
- **STT 配置**：`references/stt.md`
- **主文档**：`SKILL.md`
