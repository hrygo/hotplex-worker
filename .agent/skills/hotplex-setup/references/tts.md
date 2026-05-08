# TTS（文字转语音）详细配置

## 架构概览

HotPlex TTS 管道：

```
Voice input → STT (transcription) → AI response → TTS summary → Synthesizer → Platform
```

1. 用户发送语音消息 → STT 转录为文本
2. 文本送入 AI Worker 作为正常输入
3. AI 回复摘要至 ≤ `tts_max_chars` 字符
4. 摘要合成语音（Edge TTS → MP3，MOSS-TTS-Nano → WAV）
5. 音频按平台转码发送：
   - **飞书**：`ffmpeg → Opus`（24kHz mono，飞书要求 opus 格式上传）
   - **Slack**：`ffmpeg → MP3`（24kHz mono，Slack 原生支持 MP3 内联播放）

**当前默认**：`tts_enabled: true`, `tts_provider: edge`, `max_chars: 150`

## TTS 提供商

| 提供商 | 配置值 | 引擎 | 依赖 |
|--------|--------|------|------|
| Edge TTS | `edge` | 微软云端 Edge TTS（免费） | `ffmpeg` |
| MOSS-TTS-Nano | `moss` | 本地 CPU 推理（sidecar 进程） | `python3`, `ffmpeg`, 模型文件 |
| Edge + MOSS | `edge+moss` | Edge 优先，MOSS 本地回退 | 以上全部 |

### 依赖映射

| 依赖 | 用途 | 必需场景 |
|------|------|----------|
| ffmpeg | 合成器输出 → 平台格式转码（飞书→Opus，Slack→MP3） | **所有 TTS 场景**（`tts_enabled=true` 时） |
| python3 | MOSS-TTS-Nano sidecar 进程 | `tts_provider=moss` 或 `edge+moss` |
| MOSS 模型文件 | 本地推理权重 | `tts_provider=moss` 或 `edge+moss` |

## Edge TTS（默认，零额外依赖）

Edge TTS 使用微软免费云端服务，只需 `ffmpeg` 即可。无需本地 GPU 或模型文件。

### 可用语音

常用中文语音（完整列表见 `edge-tts --list-voices`）：

| 语音名称 | 风格 |
|----------|------|
| `zh-CN-XiaoxiaoNeural` | 通用女声（默认） |
| `zh-CN-YunxiNeural` | 通用男声 |
| `zh-CN-YunjianNeural` | 新闻男声 |

### 验证 Edge TTS

```bash
ffmpeg -version | head -1  # Edge TTS 的唯一依赖
```

## MOSS-TTS-Nano（可选，本地 CPU 推理）

MOSS-TTS-Nano 是 OpenMOSS 的开源 CPU 语音合成方案。HotPlex 通过 sidecar 模式运行：Gateway 启动 `python3 <modelDir>/app_onnx.py` 作为子进程，通过 HTTP API 通信。

**是否需要安装？**
- `tts_provider: edge`（默认）→ **不需要**
- `tts_provider: moss` 或 `edge+moss` → **需要**

**磁盘空间**：约 3GB（torch ~2GB + ONNX 模型 ~1GB + Python 脚本 ~200KB）

### 完整安装

#### 步骤 1：安装 Python 依赖

```bash
# 核心依赖（含 torch，约 2GB）
pip3 install numpy torch torchaudio sentencepiece 'onnxruntime>=1.20.0' \
  fastapi uvicorn python-multipart soundfile huggingface_hub

# 中国用户：使用镜像加速
pip3 install -i https://mirror.sjtu.edu.cn/pypi/web/simple \
  numpy torch torchaudio sentencepiece 'onnxruntime>=1.20.0' \
  fastapi uvicorn python-multipart soundfile huggingface_hub
```

验证：
```bash
python3 -c "import numpy, torch, sentencepiece, onnxruntime, fastapi, uvicorn, huggingface_hub" && echo "MOSS deps OK"
```

#### 步骤 2：准备模型目录和 Python 脚本

Gateway 启动 sidecar 时执行 `python3 <modelDir>/app_onnx.py --model-dir <modelDir>`，Python 脚本必须放在模型目录中。

```bash
MOSS_DIR=~/.hotplex/models/moss-tts-nano
mkdir -p "$MOSS_DIR"
cd "$MOSS_DIR"

# 克隆上游仓库（仅取 Python 脚本，不含模型权重）
git clone --depth 1 https://github.com/OpenMOSS/MOSS-TTS-Nano.git _scripts

# 复制所需的 Python 模块
cp _scripts/app_onnx.py _scripts/app.py \
   _scripts/onnx_tts_runtime.py _scripts/ort_cpu_runtime.py \
   _scripts/text_normalization_pipeline.py \
   _scripts/tts_robust_normalizer_single_script.py \
   _scripts/moss_tts_nano_runtime.py \
   .
cp -r _scripts/moss_tts_nano .
cp -r _scripts/assets .

# 清理
rm -rf _scripts
```

**所需文件清单**：
```
~/.hotplex/models/moss-tts-nano/
  app_onnx.py                          # FastAPI 入口（Gateway 调用此文件）
  app.py                               # Web 框架 + API 路由
  onnx_tts_runtime.py                  # ONNX 推理运行时
  ort_cpu_runtime.py                   # ONNX CPU 后端
  text_normalization_pipeline.py       # 文本预处理
  tts_robust_normalizer_single_script.py  # 鲁棒文本规范化
  moss_tts_nano_runtime.py             # PyTorch 运行时
  moss_tts_nano/                       # Python 包
  assets/                              # Demo 数据 + 参考音频
```

#### 步骤 3：下载 ONNX 模型权重

模型权重在 sidecar 首次启动时**自动下载**。手动预下载（推荐，避免首次请求超时）：

```bash
MOSS_DIR=~/.hotplex/models/moss-tts-nano
# 中国网络：先设置镜像
# export HF_ENDPOINT=https://hf-mirror.com
python3 -c "
from huggingface_hub import snapshot_download
snapshot_download('OpenMOSS-Team/MOSS-TTS-Nano-100M-ONNX',
  local_dir='$MOSS_DIR/MOSS-TTS-Nano-100M-ONNX',
  allow_patterns=['*.onnx', '*.data', '*.json', 'tokenizer.model'])
snapshot_download('OpenMOSS-Team/MOSS-Audio-Tokenizer-Nano-ONNX',
  local_dir='$MOSS_DIR/MOSS-Audio-Tokenizer-Nano-ONNX',
  allow_patterns=['*.onnx', '*.data', '*.json'])
"
```

#### 步骤 4：验证

```bash
# 检查 Python 脚本
test -f ~/.hotplex/models/moss-tts-nano/app_onnx.py && echo "MOSS scripts OK"

# 检查核心 Python 依赖
python3 -c "import numpy, torch, sentencepiece, onnxruntime, fastapi, uvicorn" && echo "MOSS deps OK"

# 手动启动 sidecar 测试（Ctrl+C 退出）
python3 ~/.hotplex/models/moss-tts-nano/app_onnx.py \
  --host 127.0.0.1 --port 18083 \
  --model-dir ~/.hotplex/models/moss-tts-nano
# 等待 "Uvicorn running on http://127.0.0.1:18083" 后测试：
curl -X POST http://127.0.0.1:18083/api/generate \
  -d "text=你好&voice=Xiaoyu&max_new_frames=50&enable_wetext=false" \
  -H "Content-Type: application/x-www-form-urlencoded"
```

### 磁盘空间预估

| 组件 | 大小 |
|------|------|
| torch + torchaudio | ~2.0 GB |
| ONNX 模型权重 | ~1.0 GB |
| Python 脚本 | ~200 KB |
| **总计** | **~3.0 GB** |

## TTS 配置参考

### 关键参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `tts_enabled` | `true` | 是否启用 TTS |
| `tts_provider` | `edge` | 语音引擎（`edge` / `moss` / `edge+moss`） |
| `tts_voice` | `zh-CN-XiaoxiaoNeural` | Edge TTS 语音名称 |
| `tts_max_chars` | `150` | Summary 最大字符数（约 37 秒语音，飞书 60 秒限制内） |
| `tts_moss_model_dir` | `~/.hotplex/models/moss-tts-nano` | MOSS 模型目录 |
| `tts_moss_voice` | `Xiaoyu` | MOSS 语音名称 |
| `tts_moss_port` | `18083` | MOSS sidecar 端口 |
| `tts_moss_idle_timeout` | `30m` | sidecar 空闲自动关闭时间 |
| `tts_moss_cpu_threads` | `0` | sidecar CPU 线程数（0 = 自动检测物理核心数） |

### 输入/输出区别

- **SummaryInputCap (2000)**：送给 LLM 做 summary 的**输入文本**截断长度
- **MaxChars (150)**：Summary **输出**的最大字符数（控制语音时长）

### MOSS sidecar 优化要点

- **cpu_threads=0**：让 ORT 自动检测物理核心数，比硬编码 2 更优
- **enable_wetext=false**：ONNX 路径默认禁用 WeTextProcessing，无需安装 pynini
- **max_new_frames=150**：HotPlex 发送合成请求时使用 150（而非上游默认 375），匹配 max_chars=150
- **内存**：约 600MB-1.2GB，30min idle TTL 合理
- **冷启动**：5-15s（SSD），预热推理 0.5-3s

## 故障排查

### ffmpeg 未找到

**症状**：TTS checker 报 `fail`，日志显示 "ffmpeg not found in PATH"

**解决**：安装 ffmpeg（见 `references/dependencies.md`），确保在 PATH 中。systemd 服务需确认服务环境包含 ffmpeg 路径。

### 语音消息过长被截断

**症状**：飞书语音消息被截断

**原因**：飞书限制 60 秒语音，`max_chars=150` 约对应 37 秒，已留余量。如果手动调大了 `max_chars`，可能导致超限。

**解决**：恢复 `max_chars=150` 或降低到更保守的值。

### MOSS-TTS-Nano sidecar 启动失败

**症状**：日志显示 "moss sidecar" 相关错误

**排查**：
1. 确认 `python3` 在 PATH 中
2. 确认模型目录存在且有文件：`ls ~/.hotplex/models/moss-tts-nano/`
3. 确认端口未被占用：`lsof -i :18083`
4. 运行 `hotplex doctor` 检查 TTS 依赖

### MOSS Python 依赖缺失

**症状**：sidecar 启动后立即退出，日志显示 `ModuleNotFoundError`

**排查**：
```bash
python3 -c "import numpy, torch, sentencepiece, onnxruntime, fastapi, uvicorn"
```

**解决**：安装缺失的包（见"步骤 1：安装 Python 依赖"）。

### WeTextProcessing / tn 模块缺失

**说明**：HotPlex 已通过 `enable_wetext=false` 参数禁用 WeTextProcessing，无需安装 pynini。
中文数字/符号的文本规范化由 HotPlex Go 侧在发送合成请求前预处理。

如果仍需启用（例如需要更精确的中文数字朗读）：
```bash
conda install -y -c conda-forge pynini
pip3 install --no-deps 'WeTextProcessing>=1.0.4.1'
```

### MOSS 模型权重下载失败

**症状**：sidecar 启动超时或报 `FileNotFoundError: browser_poc_manifest.json`

**排查**：
```bash
ls ~/.hotplex/models/moss-tts-nano/MOSS-TTS-Nano-100M-ONNX/browser_poc_manifest.json
ls ~/.hotplex/models/moss-tts-nano/MOSS-Audio-Tokenizer-Nano-ONNX/
```

**解决**：
1. 确认 `huggingface_hub` 已安装：`pip3 install huggingface_hub`
2. 中国网络设置镜像：`export HF_ENDPOINT=https://hf-mirror.com`
3. 手动预下载（见"步骤 3"）

### 禁用 TTS

```bash
HOTPLEX_MESSAGING_SLACK_TTS_ENABLED=false
HOTPLEX_MESSAGING_FEISHU_TTS_ENABLED=false
```

## 相关文档

- **依赖安装**：`references/dependencies.md`
- **STT 配置**：`references/stt.md`
- **主文档**：`SKILL.md`
