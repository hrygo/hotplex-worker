package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestParseControlCommand_SlashCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		action events.ControlAction
		label  string
	}{
		{"/gc", "/gc", events.ControlActionGC, "gc"},
		{"/park", "/park", events.ControlActionGC, "gc"},
		{"/reset", "/reset", events.ControlActionReset, "reset"},
		{"/new", "/new", events.ControlActionReset, "reset"},
		{"/gc with trailing space", "/gc ", events.ControlActionGC, "gc"},
		{"/gc with trailing punct", "/gc!", events.ControlActionGC, "gc"},
		{"/gc with Chinese punct", "/gc。", events.ControlActionGC, "gc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseControlCommand(tt.input)
			require.NotNil(t, result)
			require.Equal(t, tt.action, result.Action)
			require.Equal(t, tt.label, result.Label)
		})
	}
}

func TestParseControlCommand_NaturalLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		action events.ControlAction
	}{
		// GC: sleep/suspend ($ prefix required)
		{"gc", "$gc", events.ControlActionGC},
		{"休眠", "$休眠", events.ControlActionGC},
		{"挂起", "$挂起", events.ControlActionGC},
		{"休眠 with punct", "$休眠。", events.ControlActionGC},
		{"挂起 with space+punct", " $挂起！", events.ControlActionGC},
		// Reset: start over ($ prefix required)
		{"重置", "$重置", events.ControlActionReset},
		{"重置 with punct", "$重置。", events.ControlActionReset},
		{"reset", "$reset", events.ControlActionReset},
		{"reset with punct", "$reset?", events.ControlActionReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseControlCommand(tt.input)
			require.NotNil(t, result)
			require.Equal(t, tt.action, result.Action)
		})
	}
}

func TestParseWorkerCommand_SlashCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantCmd  events.WorkerStdioCommand
		wantArgs string
	}{
		{"/context", "/context", events.StdioContextUsage, ""},
		{"/mcp", "/mcp", events.StdioMCPStatus, ""},
		{"/compact", "/compact", events.StdioCompact, ""},
		{"/clear", "/clear", events.StdioClear, ""},
		{"/rewind", "/rewind", events.StdioRewind, ""},
		{"/commit", "/commit", events.StdioCommit, ""},
		{"/model sonnet-4", "/model sonnet-4", events.StdioSetModel, "sonnet-4"},
		{"/perm bypassPermissions", "/perm bypassPermissions", events.StdioSetPermMode, "bypasspermissions"},
		{"/effort high", "/effort high", events.StdioEffort, "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseWorkerCommand(tt.input)
			require.NotNil(t, result)
			require.Equal(t, tt.wantCmd, result.Command)
			require.Equal(t, tt.wantArgs, result.Args)
		})
	}
}

func TestParseWorkerCommand_NaturalLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  events.WorkerStdioCommand
	}{
		{"上下文", "$上下文", events.StdioContextUsage},
		{"context", "$context", events.StdioContextUsage},
		{"mcp", "$mcp", events.StdioMCPStatus},
		{"切换模型", "$切换模型", events.StdioSetModel},
		{"model", "$model", events.StdioSetModel},
		{"权限模式", "$权限模式", events.StdioSetPermMode},
		{"perm", "$perm", events.StdioSetPermMode},
		{"压缩", "$压缩", events.StdioCompact},
		{"compact", "$compact", events.StdioCompact},
		{"清空", "$清空", events.StdioClear},
		{"clear", "$clear", events.StdioClear},
		{"effort", "$effort", events.StdioEffort},
		{"回退", "$回退", events.StdioRewind},
		{"rewind", "$rewind", events.StdioRewind},
		{"提交", "$提交", events.StdioCommit},
		{"commit", "$commit", events.StdioCommit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseWorkerCommand(tt.input)
			require.NotNil(t, result)
			require.Equal(t, tt.want, result.Command)
		})
	}
}

func TestParseWorkerCommand_PriorityControlCommandFirst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"/reset is control command", "/reset"},
		{"/gc is control command", "/gc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ParseControlCommand takes precedence over ParseWorkerCommand.
			ctrl := ParseControlCommand(tt.input)
			require.NotNil(t, ctrl, "ParseControlCommand should handle %s", tt.input)
			worker := ParseWorkerCommand(tt.input)
			require.Nil(t, worker, "ParseWorkerCommand should not handle %s (it's a control command)", tt.input)
		})
	}
}

func TestParseWorkerCommand_NotACommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"normal text", "hello"},
		{"unknown slash", "/unknown"},
		{"bare 上下文 without $", "上下文"},
		{"bare context without $", "context"},
		{"empty", ""},
		{"whitespace", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseWorkerCommand(tt.input)
			require.Nil(t, result)
		})
	}
}

func TestParseControlCommand_NotACommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"normal Chinese text", "帮我写一个函数"},
		{"question about reset", "密码重置怎么做"},
		{"gc in sentence", "请帮我分析这段gc日志"},
		{"park in sentence", "我们去公园玩吧"},
		{"new in sentence", "这是一个新的开始"},
		{"restart in sentence", "重启服务器试试"},
		{"empty", ""},
		{"whitespace", "   "},
		{"partial match 休眠中", "休眠中"},
		{"removed alias park", "park"},
		{"removed alias reset", "reset"},
		{"removed alias new", "new"},
		{"removed alias 从头开始", "从头开始"},
		{"removed alias 清空重来", "清空重来"},
		{"bare gc without $", "gc"},
		{"bare 休眠 without $", "休眠"},
		{"bare 挂起 without $", "挂起"},
		{"bare 重置 without $", "重置"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseControlCommand(tt.input)
			require.Nil(t, result)
		})
	}
}
