---
paths:
  - "scripts/stt_*.py"
  - "scripts/fix_onnx_model.py"
  - "internal/messaging/feishu/stt*.go"
  - "internal/assets/assets.go"
---

# STT 能力参考

> 4 核 Xeon / 7GB RAM / 无 GPU

| 工具 | 场景 | 时间轴 |
|------|------|--------|
| whisper-cli `~/.local/bin/` | 字幕/SRT/VTT | 段落+词级 |
| SenseVoice (funasr-onnx) | 最快中文 RTF 0.19x | 无 |
| stt_server.py | HotPlex 飞书/Slack 集成 | 无 |

## whisper-cli 模型 (`~/.local/share/whisper/`)

| 模型 | 磁盘 | RAM | RTF | 推荐 |
|------|------|-----|-----|------|
| small Q4_0 | 139M | 526M | 0.31x | 快速草稿 |
| medium Q4_0 | 424M | 1,076M | 0.85x | - |
| **large-v3-turbo Q4_0** | **453M** | **817M** | **0.85x** | **首选** |

> x86_64 上 Q4_0 最快。large-v3-turbo 优于 medium（4 层 decoder vs 24 层）。

```bash
M=~/.local/share/whisper/ggml-large-v3-turbo-q4_0.bin
whisper-cli -m $M -f audio.mp3 -t 4              # 基础
whisper-cli -m $M -f audio.mp3 -t 4 -l zh        # 指定中文
whisper-cli -m $M -f audio.mp3 -t 4 --output-srt # SRT 字幕
whisper-cli -m $M -f audio.mp3 -t 4 --output-vtt # WebVTT
whisper-cli -m $M -f audio.mp3 -t 4 --output-json
whisper-cli -m $M -f audio.mp3 -t 4 -ml 1 --output-srt  # 词级时间轴
whisper-cli -m $M -f audio.mp3 -t 4 --no-timestamps      # 纯文本
```

## SenseVoice

RTF 0.19x，原生中/英/日/韩/粤语，输出含情感/语音事件标签，无时间轴。

```python
model = AutoModel(model='iic/SenseVoiceSmall', device='cpu', disable_update=True)
result = model.generate(input='audio.mp3', language='auto')
# <|zh|><|HAPPY|><|Speech|><|woitn|>转录文本
```

## stt_server.py 协议

部署链路：`scripts/*.py → go:embed → assets.InstallScripts() → ~/.hotplex/scripts/`

```bash
# stdin/stdout JSON 行协议
echo '{"audio_path":"/tmp/audio.opus"}' | python3 stt_server.py
# → {"text":"...","error":"","language":"zh","emotion":"HAPPY","event":"Speech"}
```

错误码：`DEP_MISSING`（包未安装）/ `MODEL_LOAD_FAILED`（模型加载）/ `INFERENCE_FAILED`（推理）/ `INVALID_REQUEST`（请求格式）
