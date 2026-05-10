package cron

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDelivery_Deliver_NilExtractor(t *testing.T) {
	t.Parallel()

	d := NewDelivery(slog.Default(), nil, nil)
	// Should not panic, just returns.
	d.Deliver(context.Background(), &CronJob{ID: "test"}, "session-key")
}

func TestDelivery_Deliver_EmptyResponse(t *testing.T) {
	t.Parallel()

	var delivered bool
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) { return "", nil },
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			delivered = true
			return nil
		},
	)

	d.Deliver(context.Background(), &CronJob{
		ID:       "test",
		Platform: "feishu",
	}, "session-key")

	require.False(t, delivered)
}

func TestDelivery_Deliver_NoPlatform(t *testing.T) {
	t.Parallel()

	var delivered bool
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) {
			return "some response", nil
		},
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			delivered = true
			return nil
		},
	)

	// Empty platform — should not deliver.
	d.Deliver(context.Background(), &CronJob{
		ID:       "test",
		Platform: "",
	}, "session-key")
	require.False(t, delivered)

	// "cron" platform — self-originated, should not deliver.
	d.Deliver(context.Background(), &CronJob{
		ID:       "test",
		Platform: "cron",
	}, "session-key")
	require.False(t, delivered)
}

func TestDelivery_Deliver_NilDeliverer(t *testing.T) {
	t.Parallel()

	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) {
			return "response text", nil
		},
		nil, // no deliverer
	)

	// Should not panic, just logs debug.
	d.Deliver(context.Background(), &CronJob{
		ID:       "test",
		Platform: "feishu",
	}, "session-key")
}

func TestDelivery_Deliver_Success(t *testing.T) {
	t.Parallel()

	var (
		gotPlatform string
		gotResponse string
		gotKey      map[string]string
	)

	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) {
			return "cron job completed successfully", nil
		},
		func(_ context.Context, platform string, key map[string]string, response string) error {
			gotPlatform = platform
			gotKey = key
			gotResponse = response
			return nil
		},
	)

	platformKey := map[string]string{"chat_id": "oc_xxx"}
	d.Deliver(context.Background(), &CronJob{
		ID:          "test",
		Platform:    "feishu",
		PlatformKey: platformKey,
	}, "session-key")

	require.Equal(t, "feishu", gotPlatform)
	require.Equal(t, "cron job completed successfully", gotResponse)
	require.Equal(t, platformKey, gotKey)
}

func TestDelivery_Deliver_ExtractError(t *testing.T) {
	t.Parallel()

	var delivered bool
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) {
			return "", errTestDelivery
		},
		func(_ context.Context, _ string, _ map[string]string, _ string) error {
			delivered = true
			return nil
		},
	)

	d.Deliver(context.Background(), &CronJob{
		ID:       "test",
		Platform: "feishu",
	}, "session-key")

	require.False(t, delivered)
}

var errTestDelivery = errors.New("extract failed")

func TestDelivery_SetDeliverer_Overrides(t *testing.T) {
	t.Parallel()

	var calls int
	d := NewDelivery(slog.Default(),
		func(_ context.Context, _ string) (string, error) {
			return "response", nil
		},
		nil, // initial deliverer is nil
	)

	// Before SetDeliverer: no delivery.
	d.Deliver(context.Background(), &CronJob{
		Platform:    "feishu",
		PlatformKey: map[string]string{"chat_id": "oc_123"},
	}, "session-key")
	require.Equal(t, 0, calls)

	// After SetDeliverer: delivery works.
	d.SetDeliverer(func(_ context.Context, platform string, key map[string]string, resp string) error {
		calls++
		require.Equal(t, "feishu", platform)
		require.Equal(t, "oc_123", key["chat_id"])
		require.Equal(t, "response", resp)
		return nil
	})

	d.Deliver(context.Background(), &CronJob{
		Platform:    "feishu",
		PlatformKey: map[string]string{"chat_id": "oc_123"},
	}, "session-key")
	require.Equal(t, 1, calls)
}
