package tts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// ToOpus converts audio bytes (MP3 or WAV) to Ogg/Opus format (24kHz mono)
// suitable for Feishu audio messages. Requires ffmpeg at runtime.
// ffmpeg auto-detects input format from stream header.
func ToOpus(ctx context.Context, audioData []byte) ([]byte, error) {
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

// ToMP3 converts audio bytes (WAV or any format) to MP3 format (24kHz mono)
// suitable for Slack audio messages. Requires ffmpeg at runtime.
// If input is already MP3 (detected by ID3 or MPEG sync header), returns unchanged.
func ToMP3(ctx context.Context, audioData []byte) ([]byte, error) {
	if isMP3(audioData) {
		return audioData, nil
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-ar", "24000",
		"-ac", "1",
		"-acodec", "libmp3lame",
		"-b:a", "48k",
		"-f", "mp3",
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
		return nil, fmt.Errorf("ffmpeg audio→mp3: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg audio→mp3: empty output")
	}
	return out, nil
}

// isMP3 detects MP3 audio by checking for ID3v2 header or MPEG audio sync word.
func isMP3(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	// ID3v2 header: "ID3"
	if data[0] == 0x49 && data[1] == 0x44 && data[2] == 0x33 {
		return true
	}
	// MPEG audio sync word: 0xFF followed by 0xE0 mask (bits 7-5 set).
	if data[0] == 0xFF && len(data) >= 2 && (data[1]&0xE0) == 0xE0 && data[1] != 0xFF {
		return true
	}
	return false
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
