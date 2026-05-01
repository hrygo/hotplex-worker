# HotPlex 依赖安装指南

本文档详细说明 HotPlex 所需依赖的安装方法。

## Go 1.26+

### macOS

```bash
brew install go

# 验证
go version  # 应输出 go1.26 或更高
```

### Linux (Ubuntu/Debian)

```bash
sudo apt update
sudo apt install golang-go

# 验证
go version
```

### Linux (CentOS/RHEL)

```bash
sudo yum install golang

# 验证
go version
```

### 从源码安装（所有平台）

访问 https://go.dev/dl/ 下载对应平台的二进制包。

---

## Python 3.8+

### macOS

```bash
brew install python3

# 验证
python3 --version  # 应输出 3.8 或更高
```

### Linux (Ubuntu/Debian)

```bash
sudo apt update
sudo apt install python3 python3-pip

# 验证
python3 --version
```

### Linux (CentOS/RHEL)

```bash
sudo yum install python3 python3-pip

# 验证
python3 --version
```

### Windows

从 https://www.python.org/downloads/ 下载 Python 3.8+ 安装器。
安装时勾选 "Add Python to PATH"。

---

## Git

### macOS

```bash
brew install git

# 验证
git --version
```

### Linux

```bash
# Ubuntu/Debian
sudo apt install git

# CentOS/RHEL
sudo yum install git

# 验证
git --version
```

### Windows

从 https://git-scm.com/download/win 下载 Git for Windows 安装器。

---

## STT（语音转文字）依赖

### Python 包

```bash
# 标准安装
pip3 install -U funasr-onnx modelscope

# 中国用户推荐使用镜像加速
pip3 install -U funasr-onnx modelscope -i https://mirror.sjtu.edu.cn/pypi/web/simple

# 验证
python3 -c "import funasr_onnx, modelscope" && echo "✅ STT 包已安装"
```

### SenseVoice Small 模型（约 900MB）

**方法 1：首次使用自动下载**

HotPlex 会在首次使用 STT 时自动下载模型到 `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`。

**方法 2：预下载（推荐）**

```bash
# 下载模型（约 900MB，需要几分钟）
python3 -c "from modelscope.hub.snapshot_download import snapshot_download; snapshot_download('iic/SenseVoiceSmall', cache_dir='/home/hotplex/.cache/modelscope')"

# 验证
ls -lh ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/
```

**模型说明**：
- 大小：约 900MB
- 格式：ONNX FP32（非量化）
- 支持语言：中文、英文、粤语、日语、韩语
- 存储位置：`~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`

### STT 故障排查

详见 `references/stt.md`。

---

## Make（源码构建需要）

### macOS

```bash
# macOS 通常自带 make，验证
make --version
```

### Linux

```bash
# Ubuntu/Debian
sudo apt install build-essential

# CentOS/RHEL
sudo yum groupinstall "Development Tools"

# 验证
make --version
```

---

## 依赖检查总结

运行以下命令检查所有依赖：

```bash
#!/bin/bash
# 保存为 check-deps.sh，然后 bash check-deps.sh

echo "=== HotPlex 依赖检查 ==="

# Go
if command -v go &> /dev/null; then
    echo "✅ Go: $(go version)"
else
    echo "❌ Go 未安装"
fi

# Python
if command -v python3 &> /dev/null; then
    echo "✅ Python: $(python3 --version)"
else
    echo "❌ Python3 未安装"
fi

# Git
if command -v git &> /dev/null; then
    echo "✅ Git: $(git --version)"
else
    echo "❌ Git 未安装"
fi

# STT 包
if python3 -c "import funasr_onnx" 2>/dev/null; then
    echo "✅ funasr-onnx 已安装"
else
    echo "⚠️  funasr-onnx 未安装"
fi

if python3 -c "import modelscope" 2>/dev/null; then
    echo "✅ modelscope 已安装"
else
    echo "⚠️  modelscope 未安装"
fi

# STT 模型
if [ -d ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall ]; then
    echo "✅ SenseVoice 模型已下载"
else
    echo "⚠️  SenseVoice 模型未下载"
fi

# Make
if command -v make &> /dev/null; then
    echo "✅ Make: $(make --version | head -1)"
else
    echo "❌ Make 未安装"
fi
```

---

## 常见问题

### Q: Go 版本不符合要求怎么办？

**A**: 升级到 Go 1.26+：
- macOS: `brew upgrade go`
- Linux: 使用官方二进制包或包管理器升级

### Q: Python 版本太旧怎么办？

**A**: 安装 Python 3.8+（见上文）。如果系统 Python 是 2.x，需要安装 python3 并确保命令是 `python3`。

### Q: pip 安装 STT 包失败怎么办？

**A**: 尝试以下方法：
1. 升级 pip: `pip3 install --upgrade pip`
2. 使用镜像: `pip3 install -i https://mirror.sjtu.edu.cn/pypi/web/simple funasr-onnx modelscope`
3. 检查网络连接（modelscope 需要访问 GitHub）

### Q: 模型下载失败怎么办？

**A**: 详见 `references/stt.md` 的故障排查部分。

### Q: Windows 下如何安装依赖？

**A**:
1. Go: https://go.dev/dl/
2. Python: https://www.python.org/downloads/
3. Git: https://git-scm.com/download/win
4. Python 包: 在 PowerShell 中运行 `pip install funasr-onnx modelscope`
5. 模型: 运行 Python 下载命令（同上）
