---
type: spec
tags:
  - project/HotPlex
date: 2026-05-07
status: draft
progress: 15
---
# TTS 混合引擎实现规范 (v1.0)

**最后更新**: 2026-05-07
**状态**: 草案 / 待实现
**主要目标**: 实现 Edge-TTS + Kokoro-82M (CPU) 的高可靠语音合成方案。

---

## 1. 架构概览

为了兼顾音质、成本和稳定性，本系统采用双引擎兜底架构：

1.  **主引擎 (Primary)**: **Edge-TTS**
    - 来源：通过微软 Edge 浏览器内部接口获取。
    - 优势：免费、无需 API Key、音质极高（神经网络语音）。
    - 风险：非官方接口，可能受网络波动或协议变更影响。
2.  **备选/兜底引擎 (Fallback)**: **Kokoro-82M**
    - 来源：本地部署的开源模型（StyleTTS2 架构）。
    - 优势：完全离线、100% 掌控、2026 年 SOTA 级轻量化模型。
    - 性能：支持 CPU 推理，RTF (Real Time Factor) 极低。

---

## 2. 技术规格

### 2.1 Edge-TTS 实现
- **库**: `github.com/lib-x/edgetts`
- **默认配置**:
    - 语音: `zh-CN-XiaoxiaoNeural` (中文) / `en-US-AvaNeural` (英文)
    - 超时: 3s
- **错误处理**: 任何网络异常、WebSocket 握手失败或 4xx/5xx 响应均立即触发 Fallback。

### 2.2 Kokoro-82M (Local CPU) 实现
- **推理后端**: `onnxruntime-go` (CGO 绑定)。
- **模型文件**:
    - 模型: `assets/models/kokoro-v1.0.onnx`
    - 音色向量: `assets/voices/*.bin` (如 `af_heart.bin`)
    - 字典: `assets/config/vocab.json`
- **流水线 (Pipeline)**:
    1.  **Text Clean**: 标准化文本，处理数字和缩写。
    2.  **G2P (Grapheme-to-Phoneme)**: 使用 `espeak-ng` 将文本转换为 IPA 音素。
    3.  **Tokenization**: 将 IPA 音素映射为模型 `input_ids`。
    4.  **Inference**: ONNX 执行推理。
    5.  **Post-process**: 处理 PCM Float32 数据，必要时进行重采样 (24kHz -> 目标) 或格式封装。

---

## 3. 接口设计 (Go)

```go
// Synthesizer 定义统一的合成接口
type Synthesizer interface {
    // Synthesize 返回音频字节流（默认 PCM 或目标格式）
    Synthesize(ctx context.Context, text string) ([]byte, error)
}

// FallbackSynthesizer 包装逻辑
type FallbackSynthesizer struct {
    Primary   Synthesizer
    Secondary Synthesizer
}
```

---

## 4. 关键指标 (KPI)

- **稳定性**: 系统可用性需达到 99.99%（依靠本地兜底）。
- **性能**: 
    - Kokoro 在 4 核 CPU 上的 RTF 需控制在 0.2 以内。
    - 内存占用在模型加载后应稳定在 500MB - 800MB 之间。
- **音质**: 
    - Edge-TTS MOS 评分期望 > 4.2。
    - Kokoro-82M MOS 评分期望 > 4.1。

---

## 5. 依赖项与安装

1.  **系统库**: 
    - `onnxruntime` (v1.17+)
    - `espeak-ng`
2.  **模型资产**: 需手动下载模型权重并放置于 `assets/` 目录。
3.  **环境变量**: `LD_LIBRARY_PATH` 必须包含 onnxruntime 库路径。

---

## 6. 后续路线图

1.  [ ] 重构 `internal/messaging/tts` 接口。
2.  [ ] 集成 `lib-x/edgetts` 实现。
3.  [ ] 实现基于 CGO 的 Kokoro ONNX 推理器。
4.  [ ] 压力测试与异常模拟演练。
