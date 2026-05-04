package slack

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsLocalMediaPath(t *testing.T) {
	t.Parallel()

	imageDir := filepath.Join(MediaPathPrefix, "images") + string(filepath.Separator)
	videoDir := filepath.Join(MediaPathPrefix, "videos") + string(filepath.Separator)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"valid image path", imageDir + "photo.png", true},
		{"valid video path", videoDir + "clip.mp4", true},
		{"dotdot traversal", imageDir + "../../../../etc/passwd", false},
		{"dotdot traversal video", videoDir + "../../../tmp/evil", false},
		{"absolute system path", "/etc/passwd", false},
		{"empty path", "", false},
		{"relative path no prefix", "images/photo.png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isLocalMediaPath(tt.path)
			require.Equal(t, tt.want, got)
		})
	}
}
