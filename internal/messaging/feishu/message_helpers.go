package feishu

import "time"

const (
	messageExpiry = 30 * time.Minute
)

// IsMessageExpired checks if a message's create time is beyond the expiry threshold.
func IsMessageExpired(createTimeMs int64) bool {
	if createTimeMs <= 0 {
		return false
	}
	return time.Since(time.UnixMilli(createTimeMs)) > messageExpiry
}
