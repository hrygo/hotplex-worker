package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
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

// encodePCMFloat32ToWAV converts raw float32 PCM samples to a WAV file bytes.
// The output is 16-bit PCM at the given sample rate, mono.
func encodePCMFloat32ToWAV(samples []float32) []byte {
	const sampleRate = 24000
	const numChannels = 1
	const bitsPerSample = 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(samples) * blockAlign

	// WAV header (44 bytes) + PCM data.
	buf := make([]byte, 44+dataSize)

	// RIFF header.
	copy(buf[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], []byte("WAVE"))

	// fmt chunk.
	copy(buf[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:20], 16) // chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], uint16(numChannels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))

	// data chunk.
	copy(buf[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	// Convert float32 samples to int16.
	for i, s := range samples {
		// Handle NaN/Inf from bad model output, then clamp.
		if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
			s = 0
		} else if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		val := int16(s * 32767)
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(val))
	}

	return buf
}
