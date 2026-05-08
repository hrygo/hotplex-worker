#!/usr/bin/env python3
"""Fix SenseVoiceSmall ONNX model Less node type mismatch.

The pre-exported ONNX model from ModelScope has a bug: the Less operator
receives float and int64 inputs, which violates the ONNX spec. Newer
onnxruntime versions (1.19+) enforce strict type checking and refuse to
load the model.

This script inserts a Cast(int64 -> float) node before the Less node.

Usage:
    python3 fix_onnx_model.py [model_dir]

    model_dir defaults to ~/.cache/modelscope/hub/models/iic/SenseVoiceSmall
"""

import os
import sys
from pathlib import Path

import onnx
from onnx import TensorProto, helper


_MARKER_SUFFIX = ".patched"


def patch_onnx_file(model_path: str) -> bool:
    """Patch a single ONNX file. Returns True if file was patched."""
    model = onnx.load(model_path, load_external_data=True)
    patched = False

    for i, node in enumerate(model.graph.node):
        if node.op_type != "Less":
            continue

        vi_map: dict[str, int] = {}
        for vi in model.graph.value_info:
            vi_map[vi.name] = vi.type.tensor_type.elem_type
        for inp in model.graph.input:
            vi_map[inp.name] = inp.type.tensor_type.elem_type

        int64_input = next((n for n in node.input if vi_map.get(n) == TensorProto.INT64), None)
        if int64_input is None:
            continue

        cast_out = f"{int64_input}_casted_to_float"
        cast_node = helper.make_node(
            "Cast",
            inputs=[int64_input],
            outputs=[cast_out],
            name=f"fix_less_type_cast_{i}",
            to=TensorProto.FLOAT,
        )
        new_inputs = [cast_out if n == int64_input else n for n in node.input]
        while node.input:
            node.input.pop()
        node.input.extend(new_inputs)
        model.graph.value_info.append(
            helper.make_tensor_value_info(cast_out, TensorProto.FLOAT, None)
        )
        model.graph.node.insert(i, cast_node)
        patched = True

    if not patched:
        return False

    backup = model_path + ".bak"
    if not os.path.exists(backup):
        os.rename(model_path, backup)
    else:
        os.remove(model_path)
    onnx.save(model, model_path)
    return True


def patch_model_dir(model_dir: str) -> list[str]:
    """Patch all ONNX models in directory. Returns list of patched filenames.

    Uses a .patched marker file per ONNX file to skip already-patched models.
    Invalidates the marker if the model file was updated after the marker was
    created (e.g., modelscope re-downloaded the model), causing a re-patch.
    """
    patched = []
    for name in ("model_quant.onnx", "model.onnx"):
        path = os.path.join(model_dir, name)
        if not os.path.exists(path):
            continue
        marker = path + _MARKER_SUFFIX
        if os.path.exists(marker):
            # Invalidate stale marker: if model file is newer, re-patch.
            try:
                model_mtime = os.path.getmtime(path)
                marker_mtime = os.path.getmtime(marker)
                if model_mtime > marker_mtime:
                    os.remove(marker)
                else:
                    continue
            except OSError:
                continue
        if patch_onnx_file(path):
            patched.append(name)
            Path(marker).touch()
    return patched


def main() -> None:
    if len(sys.argv) > 1:
        model_dir = Path(sys.argv[1])
    else:
        model_dir = Path.home() / ".cache/modelscope/hub/models/iic/SenseVoiceSmall"

    for name in ("model_quant.onnx", "model.onnx"):
        path = model_dir / name
        if not path.exists():
            continue
        marker = str(path) + _MARKER_SUFFIX
        if os.path.exists(marker):
            # Same stale-marker check as patch_model_dir.
            try:
                if os.path.getmtime(str(path)) <= os.path.getmtime(marker):
                    print(f"{name}: already patched (marker valid)")
                    continue
                print(f"{name}: marker stale (model updated), re-patching")
                os.remove(marker)
            except OSError:
                continue
        if patch_onnx_file(str(path)):
            Path(marker).touch()
            print(f"Patched {name}")
        else:
            print(f"{name}: already correct (no Less type mismatch found)")


if __name__ == "__main__":
    main()
