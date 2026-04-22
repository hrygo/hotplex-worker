#!/usr/bin/env python3
"""Persistent STT server for hotplex-worker.

Reads JSON requests from stdin, writes JSON responses to stdout.
The model is loaded once at startup and stays in memory.

Protocol:
    stdin:  {"audio_path": "/tmp/.../audio.opus"}
    stdout: {"text": "transcription result", "error": ""}

    On error, error field contains a structured message:
        "CODE: human-readable detail"

    Error codes:
        DEP_MISSING       — required Python package not installed
        MODEL_LOAD_FAILED — model download or deserialization failure
        INFERENCE_FAILED  — runtime inference error
        INVALID_REQUEST   — malformed or missing audio_path

Exit codes (startup phase only):
    0  normal shutdown (stdin closed or idle timeout)
    1  startup error (dependency or model load failure)

All diagnostics go to stderr — stdout is reserved for JSON responses only.

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
import os
import re
import sys


def _setup_pdeathsig():
    """On Linux, request SIGTERM when parent process dies.

    This ensures the STT subprocess is automatically terminated if the
    gateway process crashes or is killed (SIGKILL), preventing orphan
    processes that would otherwise persist indefinitely.
    """
    if sys.platform != "linux":
        return
    try:
        import ctypes

        PR_SET_PDEATHSIG = 1
        SIGTERM = 15
        libc = ctypes.CDLL("libc.so.6", use_errno=True)
        libc.prctl(PR_SET_PDEATHSIG, SIGTERM)
    except Exception:
        pass


# ---------------------------------------------------------------------------
# Backends
# ---------------------------------------------------------------------------


def _suppress_stdout():
    """Redirect stdout to /dev/null, returning the original fd."""
    devnull = os.open(os.devnull, os.O_WRONLY)
    saved = os.dup(1)
    os.dup2(devnull, 1)
    os.close(devnull)
    return saved


def _restore_stdout(saved_fd):
    sys.stdout.flush()
    os.dup2(saved_fd, 1)
    os.close(saved_fd)


def create_funasr_backend(model_name: str):
    """Create a funasr-onnx SenseVoice backend."""
    try:
        from funasr_onnx import SenseVoiceSmall  # type: ignore
    except ImportError:
        _write_response(error="DEP_MISSING: funasr-onnx not installed. Run: pip install funasr-onnx")
        sys.exit(1)

    # Suppress stdout during model loading — modelscope/funasr print progress to stdout.
    saved = _suppress_stdout()
    try:
        # Auto-patch ONNX model if needed (Less node type mismatch).
        try:
            from modelscope.hub.snapshot_download import snapshot_download  # type: ignore
            from fix_onnx_model import patch_model_dir  # type: ignore

            model_dir = snapshot_download(model_name)
            for name in patch_model_dir(model_dir):
                print(f"[stt] patched ONNX: {name}", file=sys.stderr)
        except ImportError:
            pass  # onnx not installed — skip patching, let it fail naturally
        except Exception as e:
            print(f"[stt] ONNX patch warning: {type(e).__name__}: {e}", file=sys.stderr)

        try:
            model = SenseVoiceSmall(model_name, quantize=False)
        except Exception as e:
            _restore_stdout(saved)
            _write_response(error=f"MODEL_LOAD_FAILED: {type(e).__name__}: {e}")
            sys.exit(1)
    finally:
        _restore_stdout(saved)

    print(f"[stt] model loaded: {model_name}", file=sys.stderr)
    return lambda path: _funasr_transcribe(model, path)


_SENSEVOICE_TAG_RE = re.compile(r"<\|([^|]*)\|>")


def _parse_sensevoice_output(raw: str) -> dict:
    """Parse SenseVoice raw output into structured fields."""
    tags = _SENSEVOICE_TAG_RE.findall(raw)
    text = _SENSEVOICE_TAG_RE.sub("", raw).strip()
    language = tags[0] if len(tags) > 0 else ""
    emotion = tags[1] if len(tags) > 1 else ""
    event = tags[2] if len(tags) > 2 else ""
    return {"text": text, "language": language, "emotion": emotion, "event": event}


def _funasr_transcribe(model, audio_path: str) -> dict:
    try:
        result = model(audio_path)
    except Exception as e:
        return {"text": "", "error": f"INFERENCE_FAILED: {type(e).__name__}: {e}"}

    raw = result[0] if result else ""
    parsed = _parse_sensevoice_output(raw)
    return {**parsed, "error": ""}


def create_whisper_backend(model_name: str):
    """Create a faster-whisper backend."""
    try:
        from faster_whisper import WhisperModel  # type: ignore
    except ImportError:
        _write_response(error="DEP_MISSING: faster-whisper not installed. Run: pip install faster-whisper")
        sys.exit(1)

    try:
        model = WhisperModel(model_name, device="cpu", compute_type="int8")
    except Exception as e:
        _write_response(error=f"MODEL_LOAD_FAILED: {type(e).__name__}: {e}")
        sys.exit(1)

    print(f"[stt] model loaded: whisper/{model_name}", file=sys.stderr)

    def transcribe(audio_path: str) -> dict:
        try:
            segments, _ = model.transcribe(audio_path, language="zh")
            text = "".join(seg.text for seg in segments).strip()
        except Exception as e:
            return {"text": "", "error": f"INFERENCE_FAILED: {type(e).__name__}: {e}"}
        return {"text": text, "error": ""}

    return transcribe


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _write_response(text: str = "", error: str = "", **extra) -> None:
    """Write a JSON response to stdout (flushed immediately)."""
    obj = {"text": text, "error": error, **extra}
    sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.stdout.flush()


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------


def main():
    _setup_pdeathsig()

    parser = argparse.ArgumentParser(description="Persistent STT server")
    parser.add_argument("--backend", default="funasr", choices=["funasr", "whisper"])
    parser.add_argument("--model", default="iic/SenseVoiceSmall")
    args = parser.parse_args()

    if args.backend == "funasr":
        transcribe = create_funasr_backend(args.model)
    else:
        transcribe = create_whisper_backend(args.model)

    print("[stt] ready, waiting for requests on stdin", file=sys.stderr)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            req = json.loads(line)
        except json.JSONDecodeError as e:
            _write_response(error=f"INVALID_REQUEST: malformed JSON: {e}")
            continue

        audio_path = req.get("audio_path", "")
        if not audio_path:
            _write_response(error="INVALID_REQUEST: missing or empty audio_path")
            continue

        resp = transcribe(audio_path)
        _write_response(
            text=resp.get("text", ""),
            error=resp.get("error", ""),
            language=resp.get("language", ""),
            emotion=resp.get("emotion", ""),
            event=resp.get("event", ""),
        )


if __name__ == "__main__":
    main()
