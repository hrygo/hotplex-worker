#!/usr/bin/env python3
"""One-shot STT transcription for ephemeral mode.

Usage:
    python3 stt_once.py <audio_file>

Output protocol (stdout):
    Single line JSON: {"text":"...","language":"zh","emotion":"NEUTRAL","event":"Speech","error":""}

Exit codes:
    0  success
    1  usage error (no audio file argument)
    2  dependency missing (funasr-onnx not installed)
    3  model load failure
    4  inference failure

All diagnostics go to stderr — stdout is reserved for JSON response only.
"""

import json
import os
import re
import sys

# Pattern: <|lang|><|EMOTION|><|EVENT|><|woitn|>text
_SENSEVOICE_TAG_RE = re.compile(r"<\|([^|]*)\|>")


def _parse_sensevoice_output(raw: str) -> dict:
    """Parse SenseVoice raw output into structured fields."""
    tags: list[str] = _SENSEVOICE_TAG_RE.findall(raw)
    text = _SENSEVOICE_TAG_RE.sub("", raw).strip()
    # Tag order: language, emotion, event, [woitn]
    language = tags[0] if len(tags) > 0 else ""
    emotion = tags[1] if len(tags) > 1 else ""
    event = tags[2] if len(tags) > 2 else ""
    return {"text": text, "language": language, "emotion": emotion, "event": event}


def _write_json(obj: dict) -> None:
    sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def _suppress_stdout():
    """Redirect stdout to /dev/null during model loading."""
    devnull = os.open(os.devnull, os.O_WRONLY)
    saved = os.dup(1)
    os.dup2(devnull, 1)
    os.close(devnull)
    return saved


def _restore_stdout(saved_fd):
    sys.stdout.flush()
    os.dup2(saved_fd, 1)
    os.close(saved_fd)


def main():
    if len(sys.argv) < 2:
        print("STT_ERROR\tusage: stt_once.py <audio_file>", file=sys.stderr)
        sys.exit(1)

    audio_path = sys.argv[1]

    try:
        from funasr_onnx import SenseVoiceSmall  # type: ignore
    except ImportError:
        print(
            "STT_ERROR\tcode=DEP_MISSING\tfunasr-onnx not installed. Run: pip install funasr-onnx",
            file=sys.stderr,
        )
        sys.exit(2)

    saved = _suppress_stdout()
    try:
        model = SenseVoiceSmall("iic/SenseVoiceSmall", quantize=False)
    except Exception as e:
        _restore_stdout(saved)
        print(
            f"STT_ERROR\tcode=MODEL_LOAD_FAILED\t{type(e).__name__}: {e}",
            file=sys.stderr,
        )
        sys.exit(3)
    finally:
        _restore_stdout(saved)

    try:
        result = model(audio_path)
    except Exception as e:
        print(
            f"STT_ERROR\tcode=INFERENCE_FAILED\t{type(e).__name__}: {e}",
            file=sys.stderr,
        )
        sys.exit(4)

    raw = result[0] if result else ""
    parsed = _parse_sensevoice_output(raw)
    _write_json({**parsed, "error": ""})


if __name__ == "__main__":
    main()
