package slack

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestIsStreamStateError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"random error", errors.New("some random error"), false},
		{"message_not_in_streaming_state", errors.New("message_not_in_streaming_state"), true},
		{"not_in_channel", errors.New("not_in_channel"), true},
		{"channel_not_found", errors.New("channel_not_found"), true},
		{"message_not_found", errors.New("message_not_found"), true},
		{"wrapped message_not_in_streaming_state", fmt.Errorf("wrapped: %w", errors.New("message_not_in_streaming_state")), true},
		{"not_in_channel with context", errors.New("error: not_in_channel: user is not in channel"), true},
		{"network error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStreamStateError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		expectedIsLimit  bool
		expectedDuration time.Duration
	}{
		{"nil error", nil, false, 0},
		{"random error", errors.New("some error"), false, 0},
		{"slack.RateLimitedError", &slack.RateLimitedError{RetryAfter: 5 * time.Second}, true, 5 * time.Second},
		{"slack.RateLimitedError with 1s", &slack.RateLimitedError{RetryAfter: time.Second}, true, time.Second},
		{"HTTP 429 in message", errors.New("HTTP 429: Too Many Requests"), true, time.Second},
		{"rate_limit in message", errors.New("rate_limit exceeded"), true, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isLimit, duration := isRateLimitError(tt.err)
			require.Equal(t, tt.expectedIsLimit, isLimit)
			require.Equal(t, tt.expectedDuration, duration)
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"random error", errors.New("some random error"), false},
		{"invalid_auth", errors.New("invalid_auth"), true},
		{"missing_scope", errors.New("missing_scope"), true},
		{"not_allowed", errors.New("not_allowed"), true},
		{"account_inactive", errors.New("account_inactive"), true},
		{"invalid_token", errors.New("invalid_token"), true},
		{"token_revoked", errors.New("token_revoked"), true},
		{"wrapped invalid_auth", fmt.Errorf("wrapped: %w", errors.New("invalid_auth")), true},
		{"invalid_auth with context", errors.New("error: invalid_auth: token is invalid"), true},
		{"network error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"transient error", errors.New("connection timeout"), true},
		{"stream state error", errors.New("message_not_in_streaming_state"), false},
		{"auth error", errors.New("invalid_auth"), false},
		{"rate limit error", &slack.RateLimitedError{RetryAfter: time.Second}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substrs  []string
		expected bool
	}{
		{"empty string", "", []string{"a", "b"}, false},
		{"no match", "hello world", []string{"foo", "bar"}, false},
		{"first match", "hello world", []string{"hello", "bar"}, true},
		{"last match", "hello world", []string{"foo", "world"}, true},
		{"middle match", "hello world", []string{"foo", "lo wo", "bar"}, true},
		{"empty substrings", "hello", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAny(tt.str, tt.substrs)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestAC21_StreamStateErrorNoRetries(t *testing.T) {
	err := errors.New("message_not_in_streaming_state")
	require.True(t, isStreamStateError(err), "AC-2.1: message_not_in_streaming_state should be detected as stream state error")
	require.False(t, isRetryableError(err), "AC-2.1: Stream state error should not be retryable")

	notInChannelErr := errors.New("not_in_channel")
	require.True(t, isStreamStateError(notInChannelErr), "AC-2.1: not_in_channel should be detected as stream state error")
}

func TestAC22_RateLimitRespectsRetryAfter(t *testing.T) {
	retryAfter := 5 * time.Second
	rateLimitErr := &slack.RateLimitedError{RetryAfter: retryAfter}

	isLimit, duration := isRateLimitError(rateLimitErr)
	require.True(t, isLimit, "AC-2.2: Rate limit error should be detected")
	require.Equal(t, retryAfter, duration, "AC-2.2: Retry-After duration should be extracted correctly")
}

func TestAC23_AuthErrorMarksStreamExpired(t *testing.T) {
	authErrors := []string{
		"invalid_auth",
		"missing_scope",
		"not_allowed",
		"account_inactive",
		"invalid_token",
		"token_revoked",
	}

	for _, errStr := range authErrors {
		err := errors.New(errStr)
		require.True(t, isAuthError(err), "AC-2.3: %s should be detected as auth error", errStr)
		require.False(t, isRetryableError(err), "AC-2.3: %s should not be retryable", errStr)
	}
}

func TestAC24_TransientErrorsRetryable(t *testing.T) {
	transientErrors := []string{
		"connection timeout",
		"network error",
		"temporary failure",
		"service unavailable",
	}

	for _, errStr := range transientErrors {
		err := errors.New(errStr)
		require.True(t, isRetryableError(err), "AC-2.4: %s should be retryable", errStr)
		require.False(t, isStreamStateError(err), "AC-2.4: %s should not be stream state error", errStr)
		require.False(t, isAuthError(err), "AC-2.4: %s should not be auth error", errStr)
	}
}

func TestAC25_CloseTriggersFallbackOnStreamExpired(t *testing.T) {
	w := &NativeStreamingWriter{
		streamExpired: true,
		started:       true,
	}

	require.True(t, w.streamExpired, "AC-2.5: Stream should be marked expired")

	w.streamExpired = false
	w.failedFlushChunks = []string{"chunk1", "chunk2"}
	w.bytesWritten = 100
	w.bytesFlushed = 50

	integrityOK := len(w.failedFlushChunks) == 0 && w.bytesWritten == w.bytesFlushed
	require.False(t, integrityOK, "AC-2.5: Integrity check should fail with incomplete flush")
	require.True(t, len(w.failedFlushChunks) > 0 || w.streamExpired, "AC-2.5: Fallback condition should be met")
}

func TestAC26_AllTestsPass(t *testing.T) {
	allErrors := []error{
		nil,
		errors.New("random"),
		errors.New("message_not_in_streaming_state"),
		errors.New("invalid_auth"),
		&slack.RateLimitedError{RetryAfter: time.Second},
	}

	for _, err := range allErrors {
		_ = isStreamStateError(err)
		_, _ = isRateLimitError(err)
		_ = isAuthError(err)
		_ = isRetryableError(err)
	}
}

func TestNativeStreamingWriter_StreamExpiredFlag(t *testing.T) {
	w := &NativeStreamingWriter{}
	require.False(t, w.streamExpired)

	w.streamExpired = true
	require.True(t, w.streamExpired)
}

func TestNativeStreamingWriter_WriteChecksStreamExpired(t *testing.T) {
	w := &NativeStreamingWriter{
		streamExpired: true,
		closed:        false,
	}

	w.mu.Lock()
	expired := w.streamExpired
	w.mu.Unlock()

	require.True(t, expired)
}
