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

## Python 3.8+

STT 功能必需。不使用语音转文字可以跳过。

### macOS
```bash
brew install python3
python3 --version
```

### Linux
```bash
sudo apt install python3 python3-pip  # Ubuntu/Debian
sudo yum install python3 python3-pip  # CentOS/RHEL
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

TTS 语音回复**必需**。Slack 和飞书均使用 Edge TTS 生成 MP3，再通过 ffmpeg 转码为 Opus。只要 `tts_enabled=true`，ffmpeg 就是必需的。

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

## TTS 可选依赖（Kokoro 本地推理）

详见 `references/tts.md`。

仅在 `tts_provider=edge+kokoro` 时需要：
- onnxruntime v1.17+（推理后端）
- espeak-ng（文本转音素 G2P）

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
| Python 3.8+ | 本地 STT | `stt_provider=local` 或 `feishu+local` |
| Git | 源码构建 | `git clone` + `make` |
| ffmpeg | TTS 音频转码 | `tts_enabled=true` |
| funasr-onnx + modelscope | 本地 STT | `stt_provider=local` |
| onnxruntime | Kokoro TTS | `tts_provider=edge+kokoro` |
| espeak-ng | Kokoro TTS | `tts_provider=edge+kokoro` |
| Make | 源码构建 | `make` 命令 |

## 相关文档

- **STT 配置**：`references/stt.md`
- **TTS 配置**：`references/tts.md`
- **故障排查**：`references/troubleshooting.md`
- **主文档**：`SKILL.md`
