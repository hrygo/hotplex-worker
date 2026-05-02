package messaging

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestFormatSecurityError_Nil(t *testing.T) {
	t.Parallel()
	require.Empty(t, FormatSecurityError(nil, SecurityMessagesCN))
}

func TestFormatSecurityError_CN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"forbidden dir", errors.New("security: work dir is a forbidden system directory"), "🚫 禁止访问系统目录"},
		{"under dir", errors.New("security: work dir under forbidden directory"), "🚫 目录被安全策略禁止（系统关键目录）"},
		{"not in whitelist", errors.New("security: work dir not in whitelist"), "🚫 目录未在允许列表中（需在 config.yaml 中配置 security.work_dir_allowed_base_patterns）"},
		{"must be absolute", errors.New("security: work dir must be absolute"), "🚫 路径必须是绝对路径（以 / 开头）"},
		{"must not be empty", errors.New("security: work dir must not be empty"), "🚫 工作目录不能为空"},
		{"policy rejected fallback", errors.New("security: work dir unknown issue"), "🚫 安全策略拒绝"},
		{"session not active", errors.New("session not active"), "⚠️ 会话未激活（请先发送消息启动会话）"},
		{"get session", errors.New("get session failed"), "⚠️ 会话不存在"},
		{"expand work dir", errors.New("expand work dir failed"), "📁 路径展开失败（请检查路径格式）"},
		{"worker terminate", errors.New("worker terminate failed"), "⚠️ 停止原工作进程失败"},
		{"start session", errors.New("start session error"), "⚠️ 启动新会话失败"},
		{"switch-workdir", errors.New("switch-workdir: no such file"), "no such file"},
		{"switch-workdir-inplace", errors.New("switch-workdir-inplace: permission denied"), "permission denied"},
		{"unknown error", errors.New("something unexpected"), "something unexpected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FormatSecurityError(tt.err, SecurityMessagesCN))
		})
	}
}

func TestFormatSecurityError_EN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"forbidden dir", errors.New("security: work dir is a forbidden system directory"), ":no_entry_sign: Forbidden system directory"},
		{"policy rejected fallback", errors.New("security: work dir unknown issue"), ":no_entry_sign: Security policy rejected"},
		{"session not active", errors.New("session not active"), ":warning: Session not active (send a message first to start)"},
		{"switch-workdir", errors.New("switch-workdir: no such file"), "no such file"},
		{"unknown error", errors.New("something unexpected"), "something unexpected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FormatSecurityError(tt.err, SecurityMessagesEN))
		})
	}
}

func TestMCPServerIcon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		want   string
	}{
		{"connected", "✅"},
		{"ok", "✅"},
		{"disconnected", "❌"},
		{"error", "❌"},
		{"", "❌"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, MCPServerIcon(tt.status))
		})
	}
}

func TestExtractMCPStatusData(t *testing.T) {
	t.Parallel()

	t.Run("typed data", func(t *testing.T) {
		t.Parallel()
		env := &events.Envelope{Event: events.Event{Data: events.MCPStatusData{
			Servers: []events.MCPServerInfo{{Name: "test", Status: "connected"}},
		}}}
		d, ok := ExtractMCPStatusData(env)
		require.True(t, ok)
		require.Len(t, d.Servers, 1)
		require.Equal(t, "test", d.Servers[0].Name)
	})

	t.Run("map data", func(t *testing.T) {
		t.Parallel()
		env := &events.Envelope{Event: events.Event{Data: map[string]any{
			"servers": []any{map[string]any{"name": "srv", "status": "ok"}},
		}}}
		d, ok := ExtractMCPStatusData(env)
		require.True(t, ok)
		require.Len(t, d.Servers, 1)
		require.Equal(t, "srv", d.Servers[0].Name)
	})

	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()
		env := &events.Envelope{Event: events.Event{Data: "string"}}
		_, ok := ExtractMCPStatusData(env)
		require.False(t, ok)
	})
}

func TestControlFeedbackMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action   events.ControlAction
		msgs     map[events.ControlAction]string
		fallback string
		want     string
	}{
		{events.ControlActionGC, ControlFeedbackCN, "fallback", "✅ 会话已休眠，发消息即可恢复。"},
		{events.ControlActionReset, ControlFeedbackEN, "fallback", "🔄 Context reset."},
		{events.ControlAction("unknown"), ControlFeedbackCN, "✅ 已完成。", "✅ 已完成。"},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ControlFeedbackMessage(tt.action, tt.msgs, tt.fallback))
		})
	}
}
