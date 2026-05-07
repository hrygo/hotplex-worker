package tts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// MP3ToOpus converts MP3 audio bytes to Ogg/Opus format (16kHz mono)
// suitable for Feishu audio messages. Requires ffmpeg at runtime.
func MP3ToOpus(ctx context.Context, mp3Data []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-ar", "16000",
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

// EstimateAudioDurationMs estimates audio duration in milliseconds from Opus bytes.
// Opus at 16kbps mono ≈ 2000 bytes/sec. Required by Feishu audio messages.
func EstimateAudioDurationMs(opusBytes int) int {
	if opusBytes <= 0 {
		return 1000
	}
	ms := opusBytes * 1000 / 2000
	if ms < 1000 {
		return 1000
	}
	return ms
}

// EstimateAudioDuration estimates audio duration in seconds from Opus bytes.
// Opus at 16kbps mono ≈ 2000 bytes/sec. Used for logging only.
func EstimateAudioDuration(opusBytes int) int {
	if opusBytes <= 0 {
		return 1
	}
	secs := opusBytes / 2000
	if secs < 1 {
		return 1
	}
	return secs
}
