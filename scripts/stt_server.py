#!/usr/bin/env python3
"""Persistent STT server for hotplex-worker.

Reads JSON requests from stdin, writes JSON responses to stdout.
The model is loaded once at startup and stays in memory.

Protocol:
    stdin:  {"audio_path": "/tmp/.../audio.opus"}
    stdout: {"text": "transcription result", "error": ""}

Usage with funasr-onnx (ONNX, recommended):
    python3 stt_server.py --model iic/SenseVoiceSmall

Usage with faster-whisper:
    python3 stt_server.py --backend whisper --model large-v3

Configuration (config.yaml):
    feishu:
      stt_provider: "local"
      stt_local_cmd: "python3 /path/to/stt_server.py --model iic/SenseVoiceSmall"
      stt_local_mode: "persistent"
      stt_local_idle_ttl: "10m"
"""

import argparse
import json
import sys

# ---------------------------------------------------------------------------
# Backends
# ---------------------------------------------------------------------------


def create_funasr_backend(model_name: str):
    """Create a funasr-onnx SenseVoice backend."""
    try:
        from funasr_onnx import SenseVoiceSmall  # type: ignore
    except ImportError:
        print(json.dumps({"text": "", "error": "funasr-onnx not installed"}), flush=True)
        sys.exit(1)

    # Auto-patch ONNX model if needed (Less node type mismatch).
    try:
        from modelscope.hub.snapshot_download import snapshot_download  # type: ignore
        from fix_onnx_model import patch_model_dir  # type: ignore
        model_dir = snapshot_download(model_name)
        for name in patch_model_dir(model_dir):
            print(json.dumps({"text": "", "error": f"patched {name}"}), flush=True)
    except ImportError:
        pass  # onnx not installed — skip patching, let it fail naturally

    model = SenseVoiceSmall(model_name, quantize=False)
    return lambda path: _funasr_transcribe(model, path)


def _funasr_transcribe(model, audio_path: str) -> dict:
    import re

    result = model(audio_path)
    text = result[0] if result else ""
    text = re.sub(r"<\|[^|]*\|>", "", text).strip()
    return {"text": text, "error": ""}


def create_whisper_backend(model_name: str):
    """Create a faster-whisper backend."""
    try:
        from faster_whisper import WhisperModel  # type: ignore
    except ImportError:
        print(json.dumps({"text": "", "error": "faster-whisper not installed"}), flush=True)
        sys.exit(1)

    model = WhisperModel(model_name, device="cpu", compute_type="int8")

    def transcribe(audio_path: str) -> dict:
        segments, _ = model.transcribe(audio_path, language="zh")
        text = "".join(seg.text for seg in segments).strip()
        return {"text": text, "error": ""}

    return transcribe


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="Persistent STT server")
    parser.add_argument("--backend", default="funasr", choices=["funasr", "whisper"])
    parser.add_argument("--model", default="iic/SenseVoiceSmall")
    args = parser.parse_args()

    if args.backend == "funasr":
        transcribe = create_funasr_backend(args.model)
    else:
        transcribe = create_whisper_backend(args.model)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = transcribe(req["audio_path"])
        except Exception as e:
            resp = {"text": "", "error": str(e)}
        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
