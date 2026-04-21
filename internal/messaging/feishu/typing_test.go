package feishu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTimelineEmoji(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		// Greedy: returns emoji for the largest threshold <= elapsed.
		{"zero", 0, "YEAH"},
		{"less than 10s", 5 * time.Second, "YEAH"},
		{"at 10s boundary", 10 * time.Second, "SMILE"},
		{"30s", 30 * time.Second, "THINKING"},
		{"1 min", 1 * time.Minute, "SMUG"},
		{"2 min", 2 * time.Minute, "SMUG"},
		{"5 min", 5 * time.Minute, "STRIVE"},
		{"6 min", 6 * time.Minute, "STRIVE"},
		{"10 min", 10 * time.Minute, "BLACKFACE"},
		{"11 min", 11 * time.Minute, "BLACKFACE"},
		{"15 min", 15 * time.Minute, "NOSEPICK"},
		{"16 min", 16 * time.Minute, "NOSEPICK"},
		{"20 min", 20 * time.Minute, "EMBARRASSED"},
		{"25 min", 25 * time.Minute, "WAIL"},
		{"30 min", 30 * time.Minute, "DIZZY"},
		{"negative returns first emoji", -1 * time.Minute, "YEAH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := timelineEmoji(tt.elapsed)
			require.Equal(t, tt.want, got)
		})
	}
}
