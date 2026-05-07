package tts

import "runtime"

func onnxRuntimeSearchPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/usr/local/lib/libonnxruntime.dylib",
			"/opt/homebrew/lib/libonnxruntime.dylib",
		}
	case "linux":
		return []string{
			"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
			"/usr/local/lib/libonnxruntime.so",
			"/usr/lib/aarch64-linux-gnu/libonnxruntime.so",
		}
	case "windows":
		return []string{
			"C:\\Program Files\\onnxruntime\\lib\\onnxruntime.dll",
		}
	default:
		return nil
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
