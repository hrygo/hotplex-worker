package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsAbortCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"stop", true},
		{"Stop", true},
		{" STOP ", true},
		{"abort", true},
		{"停止", true},
		{"取消", true},
		{"やめて", true},
		{"стоп", true},
		{"please stop", true},
		{"stop please", true},
		{"wait", true},
		{"hello", false},
		{"stopped", false},
		{"stopping", false},
		{"stop.", true},
		{"stop!", true},
		{"stop。", true},
		{"stop！", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsAbortCommand(tt.input))
		})
	}
}

func TestRegisterAbortTrigger(t *testing.T) {
	orig := "custom_abort_word_xyz"
	RegisterAbortTrigger(orig)
	require.True(t, IsAbortCommand(orig))

	// Cleanup: remove by overwriting map entry is not possible, but
	// this is a package-level registry; test only verifies registration works.
}

func TestDetectCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		action CommandAction
	}{
		{"abort", "stop", CmdAbort},
		{"help", "/help", CmdHelp},
		{"control /gc", "/gc", CmdControl},
		{"control /reset", "/reset", CmdControl},
		{"passthrough /compact", "/compact", CmdPassthrough},
		{"passthrough /effort", "/effort", CmdPassthrough},
		{"none", "hello world", CmdNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DetectCommand(tt.input)
			require.Equal(t, tt.action, result.Action)
		})
	}
}

func TestDetectCommand_ControlResult(t *testing.T) {
	t.Parallel()

	result := DetectCommand("/gc")
	require.NotNil(t, result.Control)
	require.Equal(t, "gc", result.Control.Label)
}

func TestDetectCommand_WorkerResult(t *testing.T) {
	t.Parallel()

	result := DetectCommand("/compact")
	require.NotNil(t, result.Worker)
}
