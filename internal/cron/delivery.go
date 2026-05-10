package cron

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// ResponseExtractor extracts the last assistant response from a completed session.
type ResponseExtractor func(ctx context.Context, sessionID string) (string, error)

// PlatformDeliverer sends a cron result to a specific platform target.
type PlatformDeliverer func(ctx context.Context, platform string, platformKey map[string]string, response string) error

// Delivery routes cron job execution results to the originating platform.
type Delivery struct {
	mu        sync.Mutex
	log       *slog.Logger
	extract   ResponseExtractor
	deliverFn PlatformDeliverer
}

// NewDelivery creates a new Delivery instance.
func NewDelivery(log *slog.Logger, extract ResponseExtractor, deliverFn PlatformDeliverer) *Delivery {
	return &Delivery{
		log:       log.With("component", "cron_delivery"),
		extract:   extract,
		deliverFn: deliverFn,
	}
}

// Deliver extracts the last response from the session and routes it to the platform.
func (d *Delivery) Deliver(ctx context.Context, job *CronJob, sessionKey string) {
	if d.extract == nil {
		return
	}

	response, err := d.extract(ctx, sessionKey)
	if err != nil {
		d.log.Warn("cron delivery: extract response failed", "job_id", job.ID, "err", err)
		return
	}
	if response == "" {
		return
	}

	// [SILENT] suppression.
	if strings.HasPrefix(strings.TrimSpace(response), "[SILENT]") {
		d.log.Debug("cron delivery: suppressed [SILENT] response", "job_id", job.ID)
		return
	}

	// No platform or self-originated = no delivery.
	if job.Platform == "" || job.Platform == "cron" {
		return
	}

	if d.deliverFn == nil {
		d.log.Debug("cron delivery: no platform deliverer configured", "platform", job.Platform)
		return
	}

	d.mu.Lock()
	fn := d.deliverFn
	d.mu.Unlock()

	if err := fn(ctx, job.Platform, job.PlatformKey, response); err != nil {
		d.log.Warn("cron delivery: deliver failed",
			"job_id", job.ID, "platform", job.Platform, "err", err)
	}
}

// SetDeliverer sets the platform deliverer function after construction.
// Used when the deliverer depends on adapters initialized later than the cron scheduler.
func (d *Delivery) SetDeliverer(fn PlatformDeliverer) {
	d.mu.Lock()
	d.deliverFn = fn
	d.mu.Unlock()
}
