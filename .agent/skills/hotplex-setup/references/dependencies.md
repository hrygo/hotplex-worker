# HotPlex 依赖安装指南

`hotplex doctor` 报告依赖缺失时，按本文档安装。

## 快速安装

```bash
# macOS（Homebrew）
brew install go python3 git ffmpeg

# Linux (Ubuntu/Debian)
sudo apt update && sudo apt install -y golang-go python3 python3-pip git ffmpeg build-essential

# Windows (PowerShell, 管理员)
choco install go python3 git ffmpeg -y
```

---

## Go 1.26+

源码构建必需。二进制安装不需要。

### macOS
```bash
brew install go
go version  # 应输出 go1.26+
```

### Linux
```bash
# Ubuntu/Debian
sudo apt install golang-go

# CentOS/RHEL
sudo yum install golang
```

### 从源码安装（所有平台）
https://go.dev/dl/

---

## Python 3.10+

STT 和 MOSS-TTS-Nano 功能必需。不使用语音功能可以跳过。

### macOS
```bash
brew install python3
python3 --version  # 需要 >= 3.10
```

### Linux
```bash
sudo apt install python3 python3-pip python3-venv  # Ubuntu/Debian
sudo yum install python3 python3-pip               # CentOS/RHEL
```

### Windows
从 https://www.python.org/downloads/ 下载，安装时勾选 "Add Python to PATH"。

---

## Git

源码构建必需。二进制安装不需要。

### macOS
```bash
brew install git
```

### Linux
```bash
sudo apt install git     # Ubuntu/Debian
sudo yum install git     # CentOS/RHEL
```

### Windows
从 https://git-scm.com/download/win 下载。

---

## ffmpeg

TTS 语音回复**必需**。合成器输出（MP3 或 WAV）需经 ffmpeg 转码为平台所需格式（飞书→Opus，Slack→MP3）。只要 `tts_enabled=true`，ffmpeg 就是必需的。

### macOS
```bash
brew install ffmpeg
ffmpeg -version | head -1
```

### Linux
```bash
sudo apt install -y ffmpeg
ffmpeg -version | head -1
```

### Windows
```powershell
choco install ffmpeg -y
# 或: winget install Gyan.FFmpeg
ffmpeg -version
```

**systemd 服务注意**：确认服务环境 PATH 包含 ffmpeg。可用 `systemctl --user edit hotplex` 添加 `Environment="PATH=/usr/local/bin:/usr/bin:$PATH"`。

---

## STT 依赖（可选）

详见 `references/stt.md`。

```bash
# 国际用户
pip3 install -U funasr-onnx modelscope

# 中国用户（镜像加速）
pip3 install -U funasr-onnx modelscope -i https://mirror.sjtu.edu.cn/pypi/web/simple
```

验证：
```bash
python3 -c "import funasr_onnx, modelscope" && echo "STT OK"
test -d ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall && echo "Model OK"
```

---

## MOSS-TTS-Nano 依赖（可选）

详见 `references/tts.md`。

仅在 `tts_provider` 含 `moss` 时需要（约 3GB 磁盘空间）：

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

---

## Make（源码构建需要）

```bash
# macOS：通常自带
make --version

# Linux
sudo apt install build-essential           # Ubuntu/Debian
sudo yum groupinstall "Development Tools"  # CentOS/RHEL
```

---

## 依赖总览

| 依赖 | 用途 | 何时需要 |
|------|------|---------|
| Go 1.26+ | 源码构建 | `make build` |
| Python 3.10+ | 本地 STT / MOSS-TTS | `stt_provider=local` 或 `tts_provider=moss` |
| Git | 源码构建 | `git clone` + `make` |
| ffmpeg | TTS 音频转码 | `tts_enabled=true` |
| funasr-onnx + modelscope | 本地 STT | `stt_provider=local` |
| torch + onnxruntime | MOSS-TTS-Nano | `tts_provider=moss` 或 `edge+moss` |
| Make | 源码构建 | `make` 命令 |

## 相关文档

- **STT 配置**：`references/stt.md`
- **TTS 配置**：`references/tts.md`
- **故障排查**：`references/troubleshooting.md`
- **主文档**：`SKILL.md`
