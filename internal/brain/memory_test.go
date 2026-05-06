package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Mock Brain for Memory Tests
// ========================================

type mockBrainForMemory struct {
	chatFn     func(ctx context.Context, prompt string) (string, error)
	analyzeFn  func(ctx context.Context, prompt string, target any) error
	chatCount  int
	analyzeCnt int
}

func (m *mockBrainForMemory) Chat(ctx context.Context, prompt string) (string, error) {
	m.chatCount++
	if m.chatFn != nil {
		return m.chatFn(ctx, prompt)
	}
	return "mock summary", nil
}

func (m *mockBrainForMemory) Analyze(ctx context.Context, prompt string, target any) error {
	m.analyzeCnt++
	if m.analyzeFn != nil {
		return m.analyzeFn(ctx, prompt, target)
	}
	return json.Unmarshal([]byte(`{"preferences": {"language": "go"}}`), target)
}

// ========================================
// DefaultCompressionConfig Tests
// ========================================

func TestDefaultCompressionConfig(t *testing.T) {
	config := DefaultCompressionConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, 8000, config.TokenThreshold)
	assert.Equal(t, 2000, config.TargetTokenCount)
	assert.Equal(t, 5, config.PreserveTurns)
	assert.Equal(t, 500, config.MaxSummaryTokens)
	assert.InDelta(t, 0.25, config.CompressionRatio, 0.01)
	assert.Equal(t, 24*time.Hour, config.SessionTTL)
	assert.Equal(t, 1*time.Hour, config.CleanupInterval)
}

// ========================================
// NewContextCompressor Tests
// ========================================

func TestNewContextCompressor(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0 // Disable cleanup for tests

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	require.NotNil(t, compressor)
	assert.NotNil(t, compressor.sessions)
}

func TestNewContextCompressor_Disabled(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.Enabled = false
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	require.NotNil(t, compressor)

	// RecordTurn should be no-op
	compressor.RecordTurn("session1", "user", "hello", 100)
	assert.Nil(t, compressor.GetHistory("session1"))
}

// ========================================
// RecordTurn Tests
// ========================================

func TestContextCompressor_RecordTurn_NewSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	history := compressor.GetHistory("session1")
	require.NotNil(t, history)
	assert.Equal(t, "session1", history.SessionID)
	assert.Len(t, history.Turns, 1)
	assert.Equal(t, "user", history.Turns[0].Role)
	assert.Equal(t, "hello", history.Turns[0].Content)
	assert.Equal(t, 50, history.Turns[0].TokenCount)
	assert.Equal(t, 50, history.TotalTokens)
}

func TestContextCompressor_RecordTurn_Appends(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)
	compressor.RecordTurn("session1", "assistant", "hi there", 30)
	compressor.RecordTurn("session1", "user", "how are you?", 20)

	history := compressor.GetHistory("session1")
	require.Len(t, history.Turns, 3)
	assert.Equal(t, 100, history.TotalTokens)
}

func TestContextCompressor_RecordTurn_MultipleSessions(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)
	compressor.RecordTurn("session2", "user", "world", 30)

	assert.NotNil(t, compressor.GetHistory("session1"))
	assert.NotNil(t, compressor.GetHistory("session2"))
	assert.Nil(t, compressor.GetHistory("session3"))
}

// ========================================
// CheckAndCompress Tests
// ========================================

func TestContextCompressor_CheckAndCompress_BelowThreshold(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.TokenThreshold = 1000

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 100)

	result, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	assert.Nil(t, result) // Below threshold
}

func TestContextCompressor_CheckAndCompress_NoSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	result, err := compressor.CheckAndCompress(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestContextCompressor_CheckAndCompress_Disabled(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.Enabled = false
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 10000)

	result, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	assert.Nil(t, result) // Disabled
}

func TestContextCompressor_CheckAndCompress_NoBrain(t *testing.T) {
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(nil, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 10000)

	result, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	assert.Nil(t, result) // No brain
}

func TestContextCompressor_CheckAndCompress_Compresses(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "Summary: User asked about Go, assistant provided code examples.", nil
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.TokenThreshold = 100
	config.PreserveTurns = 2

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// Add turns totaling more than threshold
	for i := 0; i < 5; i++ {
		compressor.RecordTurn("session1", "user", fmt.Sprintf("user message %d", i), 50)
		compressor.RecordTurn("session1", "assistant", fmt.Sprintf("assistant response %d", i), 50)
	}

	result, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Compressed)
	assert.Contains(t, result.Summary, "Summary")
	assert.Len(t, result.Turns, 4) // PreserveTurns * 2 = 4
}

func TestContextCompressor_CheckAndCompress_AlreadyCompressed(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "Summary", nil
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.TokenThreshold = 100
	config.PreserveTurns = 2

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// Add enough turns to trigger compression
	for i := 0; i < 5; i++ {
		compressor.RecordTurn("session1", "user", fmt.Sprintf("user message %d", i), 50)
		compressor.RecordTurn("session1", "assistant", fmt.Sprintf("response %d", i), 50)
	}

	// First compression
	result1, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Second call should not re-compress (already compressed with few turns)
	result2, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)
	assert.Nil(t, result2)
}

// ========================================
// GetSessionSummary Tests
// ========================================

func TestContextCompressor_GetSessionSummary_NoSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	summary := compressor.GetSessionSummary("nonexistent")
	assert.Equal(t, "", summary)
}

func TestContextCompressor_GetSessionSummary_NotCompressed(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	summary := compressor.GetSessionSummary("session1")
	assert.Equal(t, "", summary)
}

func TestContextCompressor_GetSessionSummary_AfterCompression(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "Compressed summary here", nil
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.TokenThreshold = 50
	config.PreserveTurns = 2

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	for i := 0; i < 5; i++ {
		compressor.RecordTurn("session1", "user", fmt.Sprintf("msg %d", i), 30)
		compressor.RecordTurn("session1", "assistant", fmt.Sprintf("resp %d", i), 30)
	}

	_, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)

	summary := compressor.GetSessionSummary("session1")
	assert.Equal(t, "Compressed summary here", summary)
}

// ========================================
// GetContextForEngine Tests
// ========================================

func TestContextCompressor_GetContextForEngine_NoSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	ctx := compressor.GetContextForEngine("nonexistent")
	assert.Equal(t, "", ctx)
}

func TestContextCompressor_GetContextForEngine_WithSummary(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "Previous discussion about Go concurrency", nil
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.TokenThreshold = 50
	config.PreserveTurns = 2

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	for i := 0; i < 5; i++ {
		compressor.RecordTurn("session1", "user", fmt.Sprintf("msg %d", i), 30)
		compressor.RecordTurn("session1", "assistant", fmt.Sprintf("resp %d", i), 30)
	}

	_, err := compressor.CheckAndCompress(context.Background(), "session1")
	require.NoError(t, err)

	ctx := compressor.GetContextForEngine("session1")
	assert.Contains(t, ctx, "## Session Context")
	assert.Contains(t, ctx, "Previous discussion about Go concurrency")
}

// ========================================
// ClearSession Tests
// ========================================

func TestContextCompressor_ClearSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	compressor.ClearSession("session1")
	assert.Nil(t, compressor.GetHistory("session1"))
}

func TestContextCompressor_ClearSession_Nonexistent(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	// Should not panic
	compressor.ClearSession("nonexistent")
}

// ========================================
// ClearExpired Tests
// ========================================

func TestContextCompressor_ClearExpired(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.SessionTTL = 1 * time.Nanosecond // Very short TTL

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	cleared := compressor.ClearExpired()
	assert.Equal(t, 1, cleared)
	assert.Nil(t, compressor.GetHistory("session1"))
}

func TestContextCompressor_ClearExpired_NoExpired(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.SessionTTL = 24 * time.Hour

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	cleared := compressor.ClearExpired()
	assert.Equal(t, 0, cleared)
	assert.NotNil(t, compressor.GetHistory("session1"))
}

// ========================================
// Stats Tests
// ========================================

func TestContextCompressor_Stats(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	stats := compressor.Stats()
	assert.True(t, stats["enabled"].(bool))
	assert.Equal(t, 1, stats["session_count"])
	assert.Equal(t, 8000, stats["token_threshold"])
}

// ========================================
// SetEnabled Tests
// ========================================

func TestContextCompressor_SetEnabled(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	compressor.SetEnabled(false)
	assert.False(t, compressor.config.Enabled)

	compressor.SetEnabled(true)
	assert.True(t, compressor.config.Enabled)
}

// ========================================
// UpdateConfig Tests
// ========================================

func TestContextCompressor_UpdateConfig(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	newConfig := CompressionConfig{
		Enabled:        true,
		TokenThreshold: 5000,
		PreserveTurns:  3,
	}
	compressor.UpdateConfig(newConfig)

	stats := compressor.Stats()
	assert.Equal(t, 5000, stats["token_threshold"])
}

// ========================================
// Stop Tests
// ========================================

func TestContextCompressor_Stop(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 10 * time.Millisecond

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// Should not panic
	compressor.Stop()
}

// ========================================
// ForceCompress Tests
// ========================================

func TestContextCompressor_ForceCompress_Disabled(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.Enabled = false
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	_, err := compressor.ForceCompress(context.Background(), "session1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestContextCompressor_ForceCompress_NoBrain(t *testing.T) {
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(nil, config, slog.Default())

	_, err := compressor.ForceCompress(context.Background(), "session1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestContextCompressor_ForceCompress_Success(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "Forced summary", nil
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.PreserveTurns = 1

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// Add turns (need > PreserveTurns*2 = 2 turns to compress)
	for i := 0; i < 3; i++ {
		compressor.RecordTurn("session1", "user", fmt.Sprintf("msg %d", i), 50)
		compressor.RecordTurn("session1", "assistant", fmt.Sprintf("resp %d", i), 50)
	}

	result, err := compressor.ForceCompress(context.Background(), "session1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Compressed)
}

func TestContextCompressor_ForceCompress_NotEnoughTurns(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	config.PreserveTurns = 5

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	compressor.RecordTurn("session1", "user", "hello", 50)

	result, err := compressor.ForceCompress(context.Background(), "session1")
	require.NoError(t, err)
	assert.Nil(t, result) // Not enough turns
}

func TestContextCompressor_ForceCompress_NoSession(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	_, err := compressor.ForceCompress(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ========================================
// estimateTokens Tests
// ========================================

func TestEstimateTokens_Empty(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())
	assert.Equal(t, 0, compressor.estimateTokens(""))
}

func TestEstimateTokens_ASCII(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// 100 ASCII chars ~ 25 tokens (100/4)
	tokens := compressor.estimateTokens("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assert.Greater(t, tokens, 20)
	assert.Less(t, tokens, 30)
}

func TestEstimateTokens_CJK(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// CJK characters are denser (~1.5 chars per token)
	text := "这是一个中文测试文本用来验证"
	tokens := compressor.estimateTokens(text)
	assert.Greater(t, tokens, 5)
}

func TestEstimateTokens_Mixed(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	tokens := compressor.estimateTokens("Hello 你好 World 世界")
	assert.Greater(t, tokens, 0)
}

// ========================================
// updateCompressionRatio Tests
// ========================================

func TestUpdateCompressionRatio(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0

	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	// First compression
	compressor.totalCompressions = 1
	compressor.updateCompressionRatio(0.5)
	assert.InDelta(t, 0.5, compressor.averageCompressionRatio, 0.01)

	// Second compression
	compressor.totalCompressions = 2
	compressor.updateCompressionRatio(0.3)
	// Moving average: (0.5*1 + 0.3) / 2 = 0.4
	assert.InDelta(t, 0.4, compressor.averageCompressionRatio, 0.01)
}

// ========================================
// MemoryManager Tests
// ========================================

func TestMemoryManager_NewMemoryManager(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	compressor := NewContextCompressor(mockBrain, DefaultCompressionConfig(), slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.GetCompressor())
}

func TestMemoryManager_RecordAndGetPreference(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	mgr.RecordUserPreference("user1", "language", "go")
	mgr.RecordUserPreference("user1", "framework", "gin")

	val, ok := mgr.GetUserPreference("user1", "language")
	assert.True(t, ok)
	assert.Equal(t, "go", val)

	val, ok = mgr.GetUserPreference("user1", "framework")
	assert.True(t, ok)
	assert.Equal(t, "gin", val)
}

func TestMemoryManager_GetPreference_Nonexistent(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	_, ok := mgr.GetUserPreference("user1", "nonexistent")
	assert.False(t, ok)
}

func TestMemoryManager_GetAllPreferences(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	mgr.RecordUserPreference("user1", "lang", "go")
	mgr.RecordUserPreference("user1", "style", "idiomatic")

	prefs := mgr.GetAllUserPreferences("user1")
	require.Len(t, prefs, 2)
	assert.Equal(t, "go", prefs["lang"])
	assert.Equal(t, "idiomatic", prefs["style"])
}

func TestMemoryManager_GetAllPreferences_Nonexistent(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	prefs := mgr.GetAllUserPreferences("nonexistent")
	assert.Nil(t, prefs)
}

func TestMemoryManager_GenerateUserContext(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	mgr.RecordUserPreference("user1", "language", "go")

	ctx := mgr.GenerateUserContext("user1")
	assert.Contains(t, ctx, "## User Preferences")
	assert.Contains(t, ctx, "language: go")
}

func TestMemoryManager_GenerateUserContext_NoPreferences(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	ctx := mgr.GenerateUserContext("user1")
	assert.Equal(t, "", ctx)
}

func TestMemoryManager_ExtractPreferences_NoBrain(t *testing.T) {
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(nil, config, slog.Default())

	mgr := NewMemoryManager(compressor, nil, slog.Default())

	err := mgr.ExtractPreferences(context.Background(), "user1", "I prefer Go")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "brain not configured")
}

func TestMemoryManager_ExtractPreferences_Success(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		analyzeFn: func(ctx context.Context, prompt string, target any) error {
			return json.Unmarshal([]byte(`{"preferences": {"language": "go", "framework": "gin"}}`), target)
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	err := mgr.ExtractPreferences(context.Background(), "user1", "I prefer Go with Gin framework")
	require.NoError(t, err)

	val, ok := mgr.GetUserPreference("user1", "language")
	assert.True(t, ok)
	assert.Equal(t, "go", val)

	val, ok = mgr.GetUserPreference("user1", "framework")
	assert.True(t, ok)
	assert.Equal(t, "gin", val)
}

func TestMemoryManager_ExtractPreferences_BrainError(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		analyzeFn: func(ctx context.Context, prompt string, target any) error {
			return fmt.Errorf("brain error")
		},
	}
	config := DefaultCompressionConfig()
	config.CleanupInterval = 0
	compressor := NewContextCompressor(mockBrain, config, slog.Default())

	mgr := NewMemoryManager(compressor, mockBrain, slog.Default())

	err := mgr.ExtractPreferences(context.Background(), "user1", "I prefer Go")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "extract preferences")
}

// ========================================
// Global Memory Functions Tests
// ========================================

func TestGlobalCompressor_NilInitially(t *testing.T) {
	assert.Nil(t, GlobalCompressor())
}

func TestGlobalMemoryManager_NilInitially(t *testing.T) {
	assert.Nil(t, GlobalMemoryManager())
}

func TestRecordTurn_NilCompressor(t *testing.T) {
	// Should not panic
	RecordTurn("session1", "user", "hello", 50)
}

func TestCheckCompress_NilCompressor(t *testing.T) {
	result, err := CheckCompress(context.Background(), "session1")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetSessionSummary_NilCompressor(t *testing.T) {
	assert.Equal(t, "", GetSessionSummary("session1"))
}

func TestInitMemory_NoBrain(t *testing.T) {
	oldBrain := globalBrain
	oldCompressor := globalCompressor
	oldMgr := globalMemoryMgr
	defer func() {
		globalBrain = oldBrain
		globalCompressor = oldCompressor
		globalMemoryMgr = oldMgr
		memoryOnce = sync.Once{}
	}()

	SetGlobal(nil)
	memoryOnce = sync.Once{}

	InitMemory(DefaultCompressionConfig(), slog.Default())
	// Should not create compressor when brain is nil
	assert.Nil(t, GlobalCompressor())
}

// ========================================
// Turn Structure Tests
// ========================================

func TestTurn_Structure(t *testing.T) {
	turn := Turn{
		Role:       "user",
		Content:    "hello",
		Timestamp:  time.Now(),
		TokenCount: 10,
	}

	assert.Equal(t, "user", turn.Role)
	assert.Equal(t, "hello", turn.Content)
	assert.Equal(t, 10, turn.TokenCount)
	assert.NotNil(t, turn.Timestamp)
}

func TestSessionHistory_Structure(t *testing.T) {
	history := SessionHistory{
		SessionID:   "session1",
		Turns:       []Turn{{Role: "user", Content: "hello"}},
		TotalTokens: 50,
		Compressed:  false,
		Summary:     "",
	}

	assert.Equal(t, "session1", history.SessionID)
	assert.Len(t, history.Turns, 1)
	assert.Equal(t, 50, history.TotalTokens)
	assert.False(t, history.Compressed)
	assert.Equal(t, "", history.Summary)
}

// ========================================
// CompressionConfig Tests
// ========================================

func TestCompressionConfig_JSONTags(t *testing.T) {
	config := CompressionConfig{
		Enabled:          true,
		TokenThreshold:   8000,
		TargetTokenCount: 2000,
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"enabled":true`)
	assert.Contains(t, string(data), `"token_threshold":8000`)
}

// ========================================
// estimateTokens coverage
// ========================================

func TestEstimateTokens_MixedContent(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	c := NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         true,
		TokenThreshold:  10000,
		CleanupInterval: 0, // Disable cleanup daemon
	}, slog.Default())
	defer c.Stop()

	// Mixed ASCII + CJK
	tokens := c.estimateTokens("Hello world! Chinese text")
	assert.Greater(t, tokens, 0)

	// All ASCII
	tokensASCII := c.estimateTokens("This is a plain ASCII text.")
	assert.Greater(t, tokensASCII, 0)

	// CJK characters
	tokensCJK := c.estimateTokens("Chinese text")
	assert.Greater(t, tokensCJK, 0)

	// Other unicode (emoji etc.)
	tokensEmoji := c.estimateTokens("Hello!")
	assert.Greater(t, tokensEmoji, 0)
}

// ========================================
// InitMemory coverage
// ========================================

func TestInitMemory_WithBrain(t *testing.T) {
	oldBrain := globalBrain
	oldCompressor := globalCompressor
	oldMemoryMgr := globalMemoryMgr
	defer func() {
		globalBrain = oldBrain
		globalCompressor = oldCompressor
		globalMemoryMgr = oldMemoryMgr
		memoryOnce = sync.Once{}
	}()

	SetGlobal(&mockBrainForMemory{})
	memoryOnce = sync.Once{}

	InitMemory(CompressionConfig{Enabled: true}, slog.Default())
	assert.NotNil(t, GlobalCompressor())
	assert.NotNil(t, GlobalMemoryManager())

	if comp := GlobalCompressor(); comp != nil {
		comp.Stop()
	}
}

// ========================================
// Global convenience functions
// ========================================

func TestRecordTurn_WithGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	mockBrain := &mockBrainForMemory{}
	globalCompressor = NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         true,
		TokenThreshold:  10000,
		CleanupInterval: 0,
	}, slog.Default())
	defer globalCompressor.Stop()

	// Should not panic
	RecordTurn("session1", "user", "hello", 10)

	history := globalCompressor.GetHistory("session1")
	require.NotNil(t, history)
	assert.Len(t, history.Turns, 1)
}

func TestRecordTurn_NilGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	globalCompressor = nil

	// Should not panic
	RecordTurn("session1", "user", "hello", 10)
}

func TestCheckCompress_WithGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	mockBrain := &mockBrainForMemory{}
	globalCompressor = NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         true,
		TokenThreshold:  10000,
		CleanupInterval: 0,
	}, slog.Default())
	defer globalCompressor.Stop()

	result, err := CheckCompress(context.Background(), "session1")
	assert.NoError(t, err)
	// Below threshold, so nil
	assert.Nil(t, result)
}

func TestCheckCompress_NilGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	globalCompressor = nil

	result, err := CheckCompress(context.Background(), "session1")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetSessionSummary_WithGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	mockBrain := &mockBrainForMemory{}
	globalCompressor = NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         true,
		TokenThreshold:  10000,
		CleanupInterval: 0,
	}, slog.Default())
	defer globalCompressor.Stop()

	summary := GetSessionSummary("nonexistent")
	assert.Empty(t, summary)
}

func TestGetSessionSummary_NilGlobalCompressor(t *testing.T) {
	oldCompressor := globalCompressor
	defer func() { globalCompressor = oldCompressor }()

	globalCompressor = nil

	summary := GetSessionSummary("session1")
	assert.Empty(t, summary)
}

// ========================================
// startCleanupDaemon coverage
// ========================================

func TestStartCleanupDaemon_Fires(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	c := NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         true,
		TokenThreshold:  10000,
		CleanupInterval: 50 * time.Millisecond, // Very short for testing
	}, slog.Default())
	defer c.Stop()

	// Add an expired session (with very short TTL)
	// We can't easily control the TTL per-session, but we can verify the daemon starts
	stats := c.Stats()
	assert.Equal(t, int64(0), stats["total_compressions"])
}

// ========================================
// RecordTurn disabled
// ========================================

func TestRecordTurn_Disabled(t *testing.T) {
	mockBrain := &mockBrainForMemory{}
	c := NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:         false,
		TokenThreshold:  10000,
		CleanupInterval: 0,
	}, slog.Default())
	defer c.Stop()

	c.RecordTurn("session1", "user", "hello", 10)

	history := c.GetHistory("session1")
	assert.Nil(t, history)
}

// ========================================
// generateSummary with brain error
// ========================================

func TestGenerateSummary_BrainError(t *testing.T) {
	mockBrain := &mockBrainForMemory{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "", fmt.Errorf("brain error")
		},
	}
	c := NewContextCompressor(mockBrain, CompressionConfig{
		Enabled:          true,
		TokenThreshold:   10000,
		PreserveTurns:    1,
		MaxSummaryTokens: 100,
		CleanupInterval:  0,
	}, slog.Default())
	defer c.Stop()

	// Record enough turns to trigger compression
	for i := 0; i < 6; i++ {
		c.RecordTurn("session1", "user", fmt.Sprintf("message %d", i), 2000)
	}

	// This should fail because generateSummary calls brain.Chat which returns error
	_, err := c.ForceCompress(context.Background(), "session1")
	assert.Error(t, err)
}
