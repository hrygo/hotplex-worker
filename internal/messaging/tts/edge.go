package tts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Edge TTS protocol constants.
const (
	edgeTrustedToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	edgeWSSURL       = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1?TrustedClientToken=" + edgeTrustedToken

	edgeChromiumVersion  = "134.0.3124.66"
	edgeChromiumMajor    = "134"
	edgeSecMSGECVersion  = "1-" + edgeChromiumVersion
	edgeAudioFormat      = "audio-24khz-48kbitrate-mono-mp3"
	edgeHandshakeTimeout = 30 * time.Second
	edgeDefaultVoice     = "zh-CN-XiaoxiaoNeural"

	// Windows epoch offset: seconds between 1601-01-01 and 1970-01-01.
	winEpochSeconds = 11644473600
)

// generateSecMSGec generates the Sec-MS-GEC token required by Microsoft Edge TTS.
// Algorithm: floor timestamp to 5-min boundary → convert to Windows 100ns ticks → SHA-256(ticks + token).
func generateSecMSGec() string {
	now := time.Now().UTC().Unix()
	// Floor to nearest 300-second (5-minute) boundary.
	floored := now - (now % 300)
	// Convert to 100-nanosecond intervals since Windows epoch (1601-01-01).
	winTicks := (floored + winEpochSeconds) * 10_000_000

	strToHash := fmt.Sprintf("%d%s", winTicks, edgeTrustedToken)
	hash := sha256.Sum256([]byte(strToHash))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

// edgeDialHeaders returns HTTP headers that mimic Edge browser WebSocket requests.
func edgeDialHeaders() http.Header {
	h := make(http.Header, 8)
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/"+edgeChromiumMajor+".0.0.0 Safari/537.36 Edg/"+edgeChromiumMajor+".0.0.0")
	h.Set("Accept-Encoding", "gzip, deflate, br")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Pragma", "no-cache")
	h.Set("Cache-Control", "no-cache")
	h.Set("Origin", "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold")
	return h
}

// edgeConnectionID returns a UUID without dashes for the ConnectionId parameter.
func edgeConnectionID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// synthesizeEdge connects to Microsoft Edge TTS via WebSocket and returns MP3 audio bytes.
func synthesizeEdge(ctx context.Context, text, voice string) ([]byte, error) {
	if voice == "" {
		voice = edgeDefaultVoice
	}
	// Sanitize voice: strip any characters that could break SSML attribute quoting.
	voice = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' {
			return r
		}
		return -1
	}, voice)

	// Build WebSocket URL with authentication tokens.
	wsURL := fmt.Sprintf("%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=%s&ConnectionId=%s",
		edgeWSSURL, generateSecMSGec(), edgeSecMSGECVersion, edgeConnectionID())

	dialer := websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  edgeHandshakeTimeout,
		EnableCompression: true,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, edgeDialHeaders())
	if err != nil {
		return nil, fmt.Errorf("tts edge dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Read messages in a goroutine; collect audio data.
	type result struct {
		audio []byte
		err   error
	}
	done := make(chan result, 1)

	go func() {
		var audio []byte
		for {
			msgType, data, readErr := conn.ReadMessage()
			if readErr != nil {
				// Normal close or error — return whatever audio we have.
				r := result{err: readErr}
				if audio != nil {
					r = result{audio: audio}
				}
				select {
				case done <- r:
				default:
					// Caller already returned (context cancelled).
				}
				return
			}

			switch msgType {
			case websocket.TextMessage:
				// Check for turn.end signal.
				if bytes.Contains(data, []byte("Path:turn.end")) {
					select {
					case done <- result{audio: audio}:
					default:
					}
					return
				}
			case websocket.BinaryMessage:
				// Wire format: [2-byte big-endian header length][headers...][audio data...]
				if len(data) < 2 {
					continue
				}
				headerLen := binary.BigEndian.Uint16(data[:2])
				if int(headerLen)+2 <= len(data) {
					audio = append(audio, data[2+headerLen:]...)
				}
			}
		}
	}()

	// Send speech.config message.
	ts := time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)")
	configMsg := fmt.Sprintf(
		"X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n"+
			`{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"true"},"outputFormat":"%s"}}}}`,
		ts, edgeAudioFormat,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(configMsg)); err != nil {
		return nil, fmt.Errorf("tts edge send config: %w", err)
	}

	// Send SSML message.
	reqID := edgeConnectionID()
	ssml := fmt.Sprintf(
		"<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'>"+
			"<voice name='%s'><prosody pitch='+0Hz' rate='+0%%' volume='+0%%'>%s</prosody></voice></speak>",
		voice, sanitizeSSMLText(text),
	)
	ssmlMsg := fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%sZ\r\nPath:ssml\r\n\r\n%s", reqID, ts, ssml)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlMsg)); err != nil {
		return nil, fmt.Errorf("tts edge send ssml: %w", err)
	}

	// Wait for completion or context cancellation.
	select {
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("tts edge stream: %w", r.err)
		}
		if len(r.audio) == 0 {
			return nil, fmt.Errorf("tts edge: no audio received")
		}
		return r.audio, nil
	case <-ctx.Done():
		// Force-close connection to unblock the reader goroutine.
		_ = conn.Close()
		return nil, fmt.Errorf("tts edge: %w", ctx.Err())
	}
}

// sanitizeSSMLText replaces XML-unsafe characters in SSML content.
func sanitizeSSMLText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '\'':
			b.WriteString("&apos;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r':
			// Skip control characters.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
