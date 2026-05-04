---
paths:
  - "scripts/stt_*.py"
  - "scripts/fix_onnx_model.py"
  - "internal/messaging/feishu/stt*.go"
  - "internal/assets/assets.go"
---

# STT 能力参考

HotPlex 集成两种 STT 引擎，通过 `stt_server.py` 统一对外提供 stdin/stdout JSON 行协议。

| 工具 | 场景 | 时间轴 |
|------|------|--------|
| whisper-cli (whisper.cpp) | 字幕/SRT/VTT | 段落+词级 |
| SenseVoice (funasr-onnx) | 最快中文识别 | 无（含情感/事件标签） |

## whisper-cli

whisper.cpp CLI 工具，支持量化模型（推荐 Q4_0 量化格式）。模型选择参考 whisper.cpp 官方文档。

```bash
whisper-cli -m <model_path> -f audio.mp3 -t 4              # 基础
whisper-cli -m <model_path> -f audio.mp3 -t 4 -l zh        # 指定中文
whisper-cli -m <model_path> -f audio.mp3 -t 4 --output-srt # SRT 字幕
whisper-cli -m <model_path> -f audio.mp3 -t 4 --output-vtt # WebVTT
whisper-cli -m <model_path> -f audio.mp3 -t 4 --output-json
whisper-cli -m <model_path> -f audio.mp3 -t 4 -ml 1 --output-srt  # 词级时间轴
whisper-cli -m <model_path> -f audio.mp3 -t 4 --no-timestamps      # 纯文本
```

## SenseVoice

原生中/英/日/韩/粤语，输出含情感/语音事件标签，无时间轴。

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
