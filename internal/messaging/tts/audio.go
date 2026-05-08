package tts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// MP3ToOpus converts MP3 audio bytes to Ogg/Opus format (24kHz mono)
// suitable for Feishu audio messages. Requires ffmpeg at runtime.
// Matches Edge TTS native output quality (24kHz).
func MP3ToOpus(ctx context.Context, mp3Data []byte) ([]byte, error) {
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
	cmd.Stdin = bytes.NewReader(mp3Data)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg mp3→opus: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg mp3→opus: empty output")
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
