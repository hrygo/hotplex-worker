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

本地 STT 使用阿里达摩院开源的 **SenseVoice-Small** 模型，通过 `funasr-onnx` 运行 ONNX 推理，支持中英日韩粤五语种。

#### 系统要求

| 项目 | 要求 |
|------|------|
| Python | 3.9 或更高版本 |
| 磁盘空间 | ~3 GB（模型文件） |
| 内存 | 最低 512 MB 可用（INT8 量化推理约 ~400 MB） |
| 操作系统 | Linux / macOS |

```bash
python3 --version   # 确认 Python >= 3.9
```

#### Python 依赖安装

```bash
pip3 install funasr-onnx modelscope onnx
```

| 包 | 用途 |
|---|------|
| `funasr-onnx` | SenseVoice ONNX 推理引擎 |
| `modelscope` | 模型自动下载（ModelScope Hub） |
| `onnx` | ONNX 模型修补工具 |

> **注意**：`funasr-onnx` 仅依赖 `onnxruntime`（CPU 版本）。如果环境中误安装了 `torch`、`nvidia-*`、`cuda-*` 等 GPU 包，它们不会被使用但会占用大量内存和磁盘。可通过 `pip uninstall torch nvidia-cublas nvidia-cufft nvidia-cudnn-cu13` 等移除。

验证安装：

```bash
python3 -c "from funasr_onnx import SenseVoiceSmall; print('OK')"
# 输出: OK
```

#### 模型下载

首次运行时，模型会自动从 ModelScope Hub 下载（~3 GB），缓存在 `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`。手动预下载（可选）：

```bash
python3 -c "
from modelscope.hub.snapshot_download import snapshot_download
path = snapshot_download('iic/SenseVoiceSmall')
print(f'Model cached at: {path}')
"
```

#### ONNX 模型修补

ModelScope 预导出的 ONNX 模型存在已知 bug：`Less` 算子接收了 float 和 int64 混合输入，ONNX Runtime 1.19+ 会拒绝加载。使用 `persistent` 模式时修补会自动执行，也可手动运行：

```bash
python3 scripts/fix_onnx_model.py
```

输出示例：

```
Patched model.onnx
model_quant.onnx: already correct
```

> 修补脚本会在原文件旁创建 `.bak` 备份，不会丢失原始模型。

#### 配置

Ephemeral 模式（低频使用，每次启动新进程）：

```yaml
stt_provider: "local"
stt_local_cmd: "python3 scripts/stt_once.py {file}"
```

Persistent 模式（高频使用，常驻进程零冷启动，推荐）：

```yaml
stt_provider: "local"
stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"
stt_local_idle_ttl: 15m   # 空闲 15 分钟后自动关闭子进程
```

#### 测试验证

**步骤 1 — 检查 Python 依赖**：

```bash
python3 -c "from funasr_onnx import SenseVoiceSmall; print('funasr-onnx: OK')"
python3 -c "from modelscope.hub.snapshot_download import snapshot_download; print('modelscope: OK')"
python3 -c "import onnx; print('onnx: OK')"
```

**步骤 2 — 检查模型文件**：

```bash
ls -lh ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/model.onnx
# 应显示约 900MB 的模型文件
```

**步骤 3 — 手动转写测试**：

```bash
echo '{"audio_path": "/path/to/test.opus"}' | python3 scripts/stt_server.py --model iic/SenseVoiceSmall
# 预期输出: {"text": "转写结果", "error": ""}
```

**步骤 4 — 启动 Gateway 验证**：

```bash
make dev
```

飞书：发送语音消息，日志应出现 `feishu stt: transcribed text="你好世界"`。
Slack：发送语音消息，日志应出现 `persistent stt: transcribed text="hello world"`。

#### Docker 部署

Dockerfile 添加 STT 依赖：

```dockerfile
FROM python:3.12-slim

# Install STT dependencies
RUN pip install --no-cache-dir funasr-onnx modelscope onnx

# Copy STT scripts
COPY scripts/stt_server.py /opt/hotplex/scripts/
COPY scripts/fix_onnx_model.py /opt/hotplex/scripts/
COPY scripts/stt_once.py /opt/hotplex/scripts/
```

Volume 挂载模型缓存（避免每次重建都下载 3GB）：

```yaml
# docker-compose.yaml
services:
  gateway:
    volumes:
      - stt-models:/root/.cache/modelscope
      - ./scripts:/opt/hotplex/scripts

volumes:
  stt-models:
```

飞书配置（Docker 环境）：

```yaml
feishu:
  stt_provider: "feishu+local"
  stt_local_cmd: "python3 /opt/hotplex/scripts/stt_server.py --model iic/SenseVoiceSmall"
  stt_local_idle_ttl: 30m
```

Slack 配置（Docker 环境）：

```yaml
slack:
  stt_provider: "local"
  stt_local_cmd: "python3 /opt/hotplex/scripts/stt_server.py --model iic/SenseVoiceSmall"
  stt_local_idle_ttl: 30m
```

#### 故障排查

| 问题 | 日志关键词 | 解决方案 |
|------|-----------|---------|
| funasr-onnx 未安装 | `funasr-onnx not installed` | `pip3 install funasr-onnx` |
| ONNX 模型加载失败 | `type mismatch` | `python3 scripts/fix_onnx_model.py`，或删除缓存重新下载 |
| 云端 STT 返回空 | `transcribed text=""` | 检查飞书 `speech_to_text` 权限是否开通 |
| Slack 语音未被识别 | `user shared a file` | 确认 `slack.stt_provider` 为 `"local"` 且 `stt_local_cmd` 非空 |
| Persistent 子进程频繁重启 | `persistent stt` 多条 start 日志 | 检查内存是否不足（OOM Kill）、模型文件是否损坏 |
| Ephemeral 模式延迟 3-5s | — | 正常现象，切换到 persistent 模式 |
| 模型下载慢/失败 | — | ModelScope 服务器在国内，海外可能较慢；可手动下载后放入 `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/` |

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
