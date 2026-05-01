package feishu

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
	"github.com/stretchr/testify/require"
)



// TestFormatSecurityError 测试安全错误格式化函数
func TestFormatSecurityError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "forbidden system directory",
			err:      errors.New("security: work dir \"/\" is a forbidden system directory"),
			contains: "🚫 禁止访问系统目录",
		},
		{
			name:     "under forbidden directory",
			err:      errors.New("security: work dir \"/home/hotplex/hotplex\" is under forbidden directory \"/home\""),
			contains: "🚫 目录被安全策略禁止（系统关键目录）",
		},
		{
			name:     "not in whitelist",
			err:      errors.New("security: work dir \"/unallowed\" is not in whitelist"),
			contains: "🚫 目录未在允许列表中",
		},
		{
			name:     "nil error",
			err:      nil,
			contains: "",
		},
		{
			name:     "non-security error",
			err:      errors.New("some other error"),
			contains: "some other error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSecurityError(tt.err)
			if tt.contains == "" {
				require.Empty(t, result)
			} else {
				require.Contains(t, result, tt.contains)
			}
		})
	}
}

// TestFormatSecurityError_ComplexErrors 测试复杂错误消息的格式化
func TestFormatSecurityError_ComplexErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "wrapped security error",
			err:      errors.New("INTERNAL_ERROR: 切换失败：switch-workdir-inplace: security: work dir \"/home\" is a forbidden system directory"),
			contains: "🚫 禁止访问系统目录",
		},
		{
			name:     "path traversal error",
			err:      errors.New("security: work dir \"../../../etc\" is outside allowed base"),
			contains: "🚫 安全策略拒绝",
		},
		{
			name:     "permission denied",
			err:      errors.New("security: work dir \"/root\" permission denied"),
			contains: "🚫 安全策略拒绝",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSecurityError(tt.err)
			require.Contains(t, result, tt.contains)
		})
	}
}

// TestWriteCtx_ErrorEvent_WithMessage 测试 Error 事件正确提取和发送错误消息
func TestWriteCtx_ErrorEvent_WithMessage(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg001"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	errorMsg := "测试错误消息"
	env := &events.Envelope{
		Version:   events.Version,
		ID:        "test-id",
		SessionID: "session-123",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{
				Code:    events.ErrCodeInternalError,
				Message: errorMsg,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	// 验证：不应该 panic，错误消息被处理
}

// TestWriteCtx_ErrorEvent_WithoutPlatformMsgID 测试没有 platformMsgID 时的 Error 事件处理
func TestWriteCtx_ErrorEvent_WithoutPlatformMsgID(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.startedAt = time.Now()
	// 不设置 platformMsgID
	conn.mu.Unlock()

	errorMsg := "测试错误消息"
	env := &events.Envelope{
		Version:   events.Version,
		ID:        "test-id",
		SessionID: "session-123",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{
				Code:    events.ErrCodeInternalError,
				Message: errorMsg,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	// 验证：不应该 panic，只是简单地跳过发送
}

// TestWriteCtx_ErrorEvent_WithStreamCtrl 测试有 streamCtrl 时的 Error 事件处理
func TestWriteCtx_ErrorEvent_WithStreamCtrl(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")

	// 创建一个测试用的 streaming controller
	ctrl := newTestStreamingCtrl()
	conn.mu.Lock()
	conn.streamCtrl = ctrl
	conn.platformMsgID = "msg001"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	errorMsg := "测试错误消息"
	env := &events.Envelope{
		Version:   events.Version,
		ID:        "test-id",
		SessionID: "session-123",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{
				Code:    events.ErrCodeInternalError,
				Message: errorMsg,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	// 验证：streamCtrl 应该被关闭
	// 注意：由于 mock 的 streaming controller 实际上不会关闭，这里主要验证不 panic
}

// TestWriteCtx_ErrorEvent_EmptyMessage 测试空错误消息的处理
func TestWriteCtx_ErrorEvent_EmptyMessage(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg001"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        "test-id",
		SessionID: "session-123",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{
				Code:    events.ErrCodeInternalError,
				Message: "", // 空消息
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	// 验证：不应该 panic，空消息被跳过
}
