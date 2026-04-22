# STT 语音转文字 — 安装配置手册

HotPlex Worker 支持将飞书语音消息自动转写为文字，发送给 AI Worker 处理。

本手册从零开始，涵盖所有安装和配置步骤。

---

## 目录

1. [工作原理](#1-工作原理)
2. [模式对比](#2-模式对比)
3. [快速开始：云端模式（零安装）](#3-快速开始云端模式零安装)
4. [本地 STT 安装](#4-本地-stt-安装)
   - [4.1 系统要求](#41-系统要求)
   - [4.2 安装 Python 依赖](#42-安装-python-依赖)
   - [4.3 下载模型（自动）](#43-下载模型自动)
   - [4.4 修补 ONNX 模型（自动）](#44-修补-onnx-模型自动)
5. [配置 Ephemeral 模式](#5-配置-ephemeral-模式)
6. [配置 Persistent 模式](#6-配置-persistent-模式)
7. [配置云端优先 + 本地降级](#7-配置云端优先--本地降级)
8. [验证安装](#8-验证安装)
9. [Docker 部署](#9-docker-部署)
10. [故障排查](#10-故障排查)

---

## 1. 工作原理

```
飞书用户发送语音消息
        │
        ▼
  Feishu Adapter 收到音频
        │
        ▼
  ┌─ stt_provider 判断 ─┐
  │                      │
  ▼                      ▼
云端 STT            本地 STT
(飞书 API)         (funasr-onnx)
  │                      │
  │    ┌──── 降级 ────┐  │
  │    │ (云端失败时)  │  │
  ▼    ▼              ▼  ▼
  转写文本 → 拼接到用户消息 → 发送给 Worker
```

**处理流程**：

1. 飞书用户发送语音消息，Adapter 收到音频二进制数据
2. 根据 `stt_provider` 配置，选择转写引擎
3. 转写成功后，文本拼接到用户消息中，发送给 AI Worker
4. 如果配置了 `feishu+local`，云端失败时自动降级到本地引擎

---

## 2. 模式对比

| | 云端 (feishu) | 本地 Ephemeral | 本地 Persistent |
|---|---|---|---|
| **配置值** | `feishu` | `local` | `local` |
| **需要安装** | 否 | 是 | 是 |
| **需要飞书权限** | 是（speech_to_text） | 否 | 否 |
| **冷启动** | 无 | 3-5 秒/次 | 仅首次 |
| **内存占用** | 0 | 瞬时 ~900MB | 常驻 ~900MB |
| **适用场景** | 已开通飞书权限 | 低频使用 | 高频使用 |
| **离线可用** | 否 | 是 | 是 |
| **支持语言** | 自动识别 | 中英日韩粤 | 中英日韩粤 |

**推荐配置**：`feishu+local`（默认值）—— 云端优先，失败时自动降级本地，兼顾速度和可靠性。

---

## 3. 快速开始：云端模式（零安装）

如果你已经在飞书开放平台开通了「语音转文字」权限，无需任何安装。

```yaml
# config.yaml
feishu:
  enabled: true
  stt_provider: "feishu"     # 仅使用飞书云端 STT
```

**飞书权限要求**：在应用后台 → 权限管理中添加 `speech_to_text` 权限。

验证：启动服务后发送一条语音消息，日志中出现 `feishu stt: transcribed` 即成功。

---

## 4. 本地 STT 安装

本地 STT 使用阿里达摩院开源的 **SenseVoice-Small** 模型，通过 `funasr-onnx` 运行 ONNX 推理。

### 4.1 系统要求

| 项目 | 要求 |
|------|------|
| Python | 3.9 或更高版本 |
| 磁盘空间 | ~3 GB（模型文件） |
| 内存 | 最低 1 GB 可用（推理时瞬时占用 ~900 MB） |
| 操作系统 | Linux / macOS |

检查 Python 版本：

```bash
python3 --version
# Python 3.9.x 或更高
```

### 4.2 安装 Python 依赖

```bash
pip3 install funasr-onnx modelscope onnx
```

| 包 | 用途 |
|---|------|
| `funasr-onnx` | SenseVoice ONNX 推理引擎 |
| `modelscope` | 模型自动下载（ModelScope Hub） |
| `onnx` | ONNX 模型修补工具 |

验证安装：

```bash
python3 -c "from funasr_onnx import SenseVoiceSmall; print('OK')"
# 输出: OK
```

### 4.3 下载模型（自动）

首次运行时，模型会自动从 ModelScope Hub 下载（~3 GB）。下载后缓存在本地，后续无需重复下载。

缓存位置：`~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`

手动预下载（可选，推荐在网络良好时执行）：

```bash
python3 -c "
from modelscope.hub.snapshot_download import snapshot_download
path = snapshot_download('iic/SenseVoiceSmall')
print(f'Model cached at: {path}')
"
```

### 4.4 修补 ONNX 模型（自动）

ModelScope 预导出的 ONNX 模型存在一个已知 bug：`Less` 算子接收了 float 和 int64 混合输入，违反 ONNX 规范。ONNX Runtime 1.19+ 会拒绝加载。

项目提供了自动修补脚本。使用 `persistent` 模式时修补会自动执行。也可以手动运行：

```bash
python3 scripts/fix_onnx_model.py
```

输出示例：

```
Patched model.onnx
model_quant.onnx: already correct
```

> 修补脚本会在原文件旁创建 `.bak` 备份，不会丢失原始模型。

---

## 5. 配置 Ephemeral 模式

每次语音消息启动一个新的 Python 进程进行转写，处理完毕后进程退出。

**优点**：简单、无状态、不常驻内存
**缺点**：每次冷启动 3-5 秒（加载模型），适合低频使用

### 5.1 配置

项目自带 `scripts/stt_once.py` 单次转写脚本，无需手动创建。

```yaml
# config.yaml
feishu:
  enabled: true
  stt_provider: "local"
  stt_local_cmd: "python3 scripts/stt_once.py {file}"
  stt_local_mode: "ephemeral"
```

`{file}` 会被替换为临时音频文件路径。脚本的标准输出作为转写结果。

### 5.2 验证

```bash
# 准备一个测试音频文件，然后运行：
python3 scripts/stt_once.py /path/to/test.opus
# 应输出转写文字
```

---

## 6. 配置 Persistent 模式

启动一个常驻子进程，模型常驻内存，零冷启动。

**优点**：零冷启动、高频使用性能最佳
**缺点**：常驻 ~900 MB 内存

### 6.1 配置

```yaml
# config.yaml
feishu:
  enabled: true
  stt_provider: "local"
  stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"
  stt_local_mode: "persistent"
  stt_local_idle_ttl: 1h       # 空闲 1 小时后自动关闭子进程
```

**参数说明**：

| 参数 | 说明 |
|------|------|
| `--model` | ModelScope 模型 ID，默认 `iic/SenseVoiceSmall` |
| `--backend funasr` | 使用 SenseVoice（默认，推荐） |
| `--backend whisper` | 使用 faster-whisper（需额外安装 `faster-whisper`） |

### 6.2 进程生命周期

```
Gateway 启动
    │
    ▼
收到第一条语音消息
    │
    ▼
PersistentSTT 懒启动子进程（加载模型 ~3-5s）
    │
    ▼
后续语音消息直接转写（<1s）
    │
    ▼
stt_local_idle_ttl 时间内无转写请求
    │
    ▼
子进程自动关闭，释放内存
    │
    ▼
下次语音消息到来时重新启动
```

- 子进程使用 PGID 隔离，Gateway 崩溃时自动终止
- Gateway 正常关闭时优雅终止子进程（SIGTERM → 5s → SIGKILL）
- `stt_local_idle_ttl: 0` 表示永不自动关闭

---

## 7. 配置云端优先 + 本地降级

这是默认推荐配置。云端 STT 速度快、不占本地资源，失败时自动降级到本地引擎。

```yaml
# config.yaml
feishu:
  enabled: true
  stt_provider: "feishu+local"
  stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"
  stt_local_mode: "persistent"
  stt_local_idle_ttl: 1h
```

**降级逻辑**：

1. 收到语音消息 → 先尝试飞书云端 API
2. 云端成功 → 直接返回文本（不启动本地进程）
3. 云端失败（权限未开通、配额耗尽、网络问题）→ 降级到本地引擎
4. 本地也失败 → 保存音频文件到磁盘，由 Worker 直接处理

> 如果 `stt_local_cmd` 为空，`feishu+local` 会自动退化为纯云端模式，只记录一条警告日志。

---

## 8. 验证安装

### 步骤 1：检查 Python 依赖

```bash
python3 -c "from funasr_onnx import SenseVoiceSmall; print('funasr-onnx: OK')"
python3 -c "from modelscope.hub.snapshot_download import snapshot_download; print('modelscope: OK')"
python3 -c "import onnx; print('onnx: OK')"
```

### 步骤 2：检查模型

```bash
ls -lh ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/model.onnx
# 应显示约 900MB 的模型文件
```

### 步骤 3：手动转写测试

```bash
# 使用 persistent 模式的 stt_server.py
echo '{"audio_path": "/path/to/test.opus"}' | python3 scripts/stt_server.py --model iic/SenseVoiceSmall
# 预期输出: {"text": "转写结果", "error": ""}
```

### 步骤 4：启动 Gateway 验证

```bash
make dev
```

在飞书中发送一条语音消息给机器人，观察日志：

```
feishu stt: transcribed text="你好世界" text_len=4
# 或 (persistent 模式首次):
persistent stt: started pid=12345 idle_ttl=1h0m0s
persistent stt: transcribed text="你好世界" text_len=4
```

---

## 9. Docker 部署

### Dockerfile 添加 STT 依赖

```dockerfile
FROM python:3.12-slim

# Install STT dependencies
RUN pip install --no-cache-dir funasr-onnx modelscope onnx

# Copy STT scripts
COPY scripts/stt_server.py /opt/hotplex/scripts/
COPY scripts/fix_onnx_model.py /opt/hotplex/scripts/
```

### Volume 挂载模型缓存

避免每次容器重建都重新下载 3GB 模型：

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

### 配置

```yaml
# config.yaml (Docker 环境)
feishu:
  stt_provider: "feishu+local"
  stt_local_cmd: "python3 /opt/hotplex/scripts/stt_server.py --model iic/SenseVoiceSmall"
  stt_local_mode: "persistent"
  stt_local_idle_ttl: 30m    # 容器环境建议缩短空闲超时
```

---

## 10. 故障排查

### `funasr-onnx not installed`

```
persistent stt: funasr-onnx not installed
```

**原因**：未安装 `funasr-onnx` 包。

**解决**：`pip3 install funasr-onnx`

---

### `onnxruntime` 加载模型失败

```
One or more operators in the model have type mismatch...
```

**原因**：ONNX 模型的 Less 节点类型不匹配，需要修补。

**解决**：

```bash
python3 scripts/fix_onnx_model.py
```

如果修补脚本也失败，删除缓存重新下载：

```bash
rm -rf ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall
# 重启 Gateway，模型会自动重新下载
```

---

### 云端 STT 返回空结果

```
feishu stt: transcribed text="" text_len=0
```

**可能原因**：

1. **飞书权限未开通**：在飞书开放平台 → 应用 → 权限管理，添加 `speech_to_text`
2. **音频格式不支持**：飞书 API 支持 PCM/WAV/OGG Opus，通过 `audioToPCM()` 自动转换
3. **音频太短**：极短的语音（<1s）可能无法识别

---

### Persistent 模式子进程频繁重启

**原因**：子进程崩溃后自动重启是正常行为，但频繁重启说明有问题。

**排查**：

```bash
# 查看子进程日志
grep "persistent stt" logs/gateway.log

# 常见原因：
# - 内存不足（OOM Kill）→ 增加系统内存或使用 ephemeral 模式
# - 模型文件损坏 → 删除缓存重新下载
# - Python 版本不兼容 → 确认 Python >= 3.9
```

---

### Ephemeral 模式响应慢（3-5 秒延迟）

这是正常现象。每次请求都需要重新加载 ~900MB 模型到内存。

**解决方案**：切换到 persistent 模式，或使用云端 STT 作为首选。

```yaml
# 从 ephemeral 切换到 persistent
stt_local_mode: "persistent"
stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"
```

---

### 模型下载慢或失败

ModelScope 服务器在国内，海外访问可能较慢。

**解决方案**：

```bash
# 设置 ModelScope 镜像（如果可用）
export MODELSCOPE_CACHE=~/.cache/modelscope

# 或手动下载后放到缓存目录
mkdir -p ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall
# 将模型文件放入此目录
```

---

## 配置速查

| 场景 | stt_provider | stt_local_cmd | stt_local_mode | stt_local_idle_ttl |
|------|-------------|---------------|----------------|-------------------|
| 仅云端 | `feishu` | — | — | — |
| 仅本地（低频） | `local` | `python3 scripts/stt_once.py {file}` | `ephemeral` | — |
| 仅本地（高频） | `local` | `python3 scripts/stt_server.py --model iic/SenseVoiceSmall` | `persistent` | `1h` |
| 云端 + 本地降级（推荐） | `feishu+local` | `python3 scripts/stt_server.py --model iic/SenseVoiceSmall` | `persistent` | `1h` |
| 禁用 STT | `""` (空) | — | — | — |
