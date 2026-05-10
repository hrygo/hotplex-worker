---
persona: developer
difficulty: intermediate
---

# 语音功能开发指南

HotPlex 内建完整的语音交互管线：语音输入（STT） -> AI 处理 -> 语音回复（TTS），让用户通过 Slack/飞书发送语音消息即可与 AI Agent 对话。

## 整体管线

```
用户语音消息
    |
    v
平台适配器（Slack/飞书）
    |
    v
STT 引擎 -> 文本
    |
    v
Session -> Worker（Claude Code / OpenCode Server）
    |
    v
AI 回复（长文本）
    |
    v
LLM 摘要（口语化改写，控制在 max_chars 以内）
    |
    v
TTS 引擎 -> 音频（MP3 / WAV）
    |
    v
ffmpeg 转码 -> Opus / MP3
    |
    v
平台音频消息回传
```

## STT（语音转文字）

### Provider 类型

| Provider | 引擎 | 依赖 | 说明 |
|----------|------|------|------|
| `feishu` | 飞书语音识别 API | ffmpeg | 云端转写，音频经 ffmpeg 转 PCM 后上传 |
| `local` | 本地子进程（stt_server.py） | python3 + funasr-onnx + onnxruntime | 常驻进程，JSON-over-stdio 协议，模型常驻内存 |
| `feishu+local` | 飞书优先 + local 兜底 | 上述全部 | 先调飞书 API，失败时回退本地 |

### 配置

```bash
# 全局默认（三级继承：全局 -> 平台 -> Bot）
HOTPLEX_MESSAGING_STT_PROVIDER=local              # feishu | local | feishu+local
HOTPLEX_MESSAGING_STT_LOCAL_CMD="python3 ~/.hotplex/scripts/stt_server.py"
HOTPLEX_MESSAGING_STT_LOCAL_IDLE_TTL=1h           # 空闲自动关闭
```

### 本地 STT 工作原理

`local` provider 使用 `PersistentSTT` 管理常驻 Python 子进程：

1. 首次请求时懒启动子进程（PGID 隔离）
2. 音频写入临时文件，通过 stdin 发送 JSON 请求：`{"audio_path": "/tmp/.../stt_123.opus"}`
3. 从 stdout 读取 JSON 响应：`{"text": "转录结果", "error": ""}`
4. 空闲超过 `IDLE_TTL` 自动关闭，下次请求自动重启
5. 崩溃自动检测 + 重启，Gateway 关闭时分层终止（close stdin -> SIGTERM -> 5s -> SIGKILL）

子进程部署链路：`scripts/*.py` -> `go:embed` -> `assets.InstallScripts()` -> `~/.hotplex/scripts/`

## TTS（文字转语音）

### Provider 类型

| Provider | 引擎 | 输出格式 | 依赖 |
|----------|------|----------|------|
| `edge` | Microsoft Edge TTS | MP3 | 无（免费 WebSocket API） |
| `moss` | MOSS-TTS-Nano | WAV | python3 + ONNX 模型 + ffmpeg |
| `edge+moss` | Edge 优先 + MOSS 兜底 | MP3 / WAV | 上述全部 |

### 配置

```bash
# 全局默认
HOTPLEX_MESSAGING_TTS_ENABLED=true
HOTPLEX_MESSAGING_TTS_PROVIDER=edge+moss
HOTPLEX_MESSAGING_TTS_VOICE=zh-CN-XiaoxiaoNeural    # Edge 音色
HOTPLEX_MESSAGING_TTS_MAX_CHARS=150                   # 摘要最大字符数

# MOSS 专用（仅 moss / edge+moss 时需要）
HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR=~/.hotplex/models/moss-tts-nano
HOTPLEX_MESSAGING_TTS_MOSS_VOICE=Xiaoyu
HOTPLEX_MESSAGING_TTS_MOSS_PORT=18083
HOTPLEX_MESSAGING_TTS_MOSS_IDLE_TIMEOUT=30m
HOTPLEX_MESSAGING_TTS_MOSS_CPU_THREADS=2
```

### Edge TTS 工作原理

原生 Go 实现（无第三方依赖），通过 WebSocket 连接 Microsoft Edge TTS 服务：

1. 生成 `Sec-MS-GEC` 认证 token（SHA-256 + Windows epoch）
2. 建立 WebSocket 连接，发送 SSML 语音合成请求
3. 流式接收二进制音频帧（24kHz mono MP3）
4. Edge TTS 默认音色 `zh-CN-XiaoxiaoNeural`，支持所有 Microsoft Neural 音色

### MOSS-TTS-Nano 工作原理

本地 CPU 推理引擎，以 FastAPI sidecar 进程运行：

1. 首次请求时懒启动（`python3 app_onnx.py --host 127.0.0.1 --port 18083`）
2. 等待 `/api/warmup-status` 健康检查通过（最长 60s）
3. 通过 HTTP POST `/api/generate` 调用合成（返回 base64 编码 WAV）
4. 空闲超时自动关闭，崩溃自动重启
5. 支持引用计数共享（多平台复用同一 sidecar）

### 音频转码

所有 TTS 输出经 ffmpeg 转码为平台所需格式：

- **飞书**：MP3/WAV -> Opus（24kHz mono，`ToOpus`）
- **Slack**：WAV -> MP3（24kHz mono，`ToMP3`），已是 MP3 则跳过

### 摘要生成

AI 回复通常很长，TTS 前需通过 LLM 改写为口语播报稿：

1. 输入截断至 `SummaryInputCap`（2000 字符）
2. 调用 Brain LLM，使用专用 TTS system prompt（语音播报编辑人设）
3. 输出经 `SanitizeForSpeech` 清洗（移除 Markdown、代码块、文件路径、大数字中文化）
4. 控制在 `max_chars` 字符以内（默认 150）

## 各 Provider 安装指南

### Edge TTS（推荐，零配置）

无需安装任何依赖。Gateway 内建原生 WebSocket 客户端，开箱即用。

```bash
# 最简配置 — 只用 Edge TTS
HOTPLEX_MESSAGING_TTS_ENABLED=true
HOTPLEX_MESSAGING_TTS_PROVIDER=edge
```

### MOSS-TTS-Nano（离线 / 低延迟）

```bash
# 1. 安装 Python 依赖
pip3 install numpy sentencepiece onnxruntime fastapi uvicorn python-multipart soundfile huggingface_hub

# 2. 下载模型和脚本到指定目录
mkdir -p ~/.hotplex/models/moss-tts-nano
# 将 MOSS-TTS-Nano 的 app_onnx.py 和模型文件放入该目录

# 3. 配置
HOTPLEX_MESSAGING_TTS_PROVIDER=moss
HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR=~/.hotplex/models/moss-tts-nano
```

### 本地 STT

```bash
# 1. 安装 Python 依赖
pip3 install funasr-onnx onnxruntime onnx

# 2. 首次启动 gateway 时自动部署 stt_server.py 到 ~/.hotplex/scripts/

# 3. 配置
HOTPLEX_MESSAGING_STT_PROVIDER=local
HOTPLEX_MESSAGING_STT_LOCAL_CMD="python3 ~/.hotplex/scripts/stt_server.py"
```

### 飞书云端 STT

仅限飞书平台，无需额外安装。Gateway 使用飞书 `speech_to_text` API，自动将音频转为 PCM 后上传。需配置飞书 App ID 和 App Secret。

## 依赖检查

使用 `hotplex doctor` 自动检测语音功能所需依赖：

```bash
hotplex doctor
```

检查项包括：

| 检查器 | 检测内容 |
|--------|----------|
| `stt.runtime` | python3、funasr-onnx、onnxruntime、onnx、stt_server.py 部署状态、ffmpeg |
| `tts.runtime` | ffmpeg、python3（MOSS）、MOSS Python 包、模型目录和入口脚本 |

检测逻辑根据实际配置动态判断：仅配置了 `edge` 的 TTS 不检查 Python，仅配置了 `feishu` 的 STT 不检查本地 STT 包。

## 相关源码

| 模块 | 路径 |
|------|------|
| STT 核心 | `internal/messaging/stt/stt.go` |
| 飞书 STT | `internal/messaging/feishu/stt.go` |
| TTS 核心 | `internal/messaging/tts/tts.go` |
| Edge TTS | `internal/messaging/tts/edge.go` |
| MOSS TTS | `internal/messaging/tts/moss.go` + `moss_process.go` |
| 音频转码 | `internal/messaging/tts/audio.go` |
| 摘要生成 | `internal/messaging/tts/prompt.go` |
| 环境检查 | `internal/cli/checkers/stt.go` + `tts.go` |
| 配置模板 | `configs/env.example` |
