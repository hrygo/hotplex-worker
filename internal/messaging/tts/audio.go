package tts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// MP3ToOpus converts audio bytes (MP3 or WAV) to Ogg/Opus format (16kHz mono)
// suitable for Feishu/Slack audio messages. Requires ffmpeg at runtime.
// ffmpeg auto-detects input format from stream header.
func MP3ToOpus(ctx context.Context, audioData []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-ar", "24000",
		"-ac", "1",
		"-acodec", "libopus",
		"-f", "ogg",
		"-hide_banner",
		"-loglevel", "error",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audioData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg audio→opus: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg audio→opus: empty output")
	}
	return out, nil
}

// EstimateAudioDuration estimates audio duration in seconds from audio bytes.
// Assumes 48kbps mono ≈ 6000 bytes/sec. Used for logging.
func EstimateAudioDuration(audioBytes int) int {
	if audioBytes <= 0 {
		return 1
	}
	secs := audioBytes / 6000
	if secs < 1 {
		return 1
	}
	return secs
}

// EstimateAudioDurationMs returns audio duration in milliseconds.
// Required by Feishu audio messages.
func EstimateAudioDurationMs(audioBytes int) int {
	return EstimateAudioDuration(audioBytes) * 1000
}
