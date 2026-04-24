package scripts

import "embed"

// FS contains only the essential Python scripts needed at runtime.
//
//go:embed stt_server.py stt_once.py fix_onnx_model.py
var FS embed.FS
