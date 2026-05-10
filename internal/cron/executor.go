package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// BridgeStarter is the narrow interface the executor needs from the gateway Bridge.
type BridgeStarter interface {
	StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string, title string) error
}

// SessionStateChecker polls session state for completion detection.
type SessionStateChecker interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
}

// Executor runs a single cron job by starting a worker session and delivering the prompt.
type Executor struct {
	log    *slog.Logger
	bridge BridgeStarter
	sm     SessionStateChecker
}

// NewExecutor creates a new cron executor.
func NewExecutor(log *slog.Logger, bridge BridgeStarter, sm SessionStateChecker) *Executor {
	return &Executor{
		log:    log.With("component", "cron_executor"),
		bridge: bridge,
		sm:     sm,
	}
}

// Execute runs a cron job: starts a session, sends the prompt, and waits for completion.
// Returns the session key used for delivery routing.
// timeout is the execution deadline (from job.TimeoutSec or scheduler default).
func (e *Executor) Execute(ctx context.Context, job *CronJob, timeout time.Duration) (string, error) {
	sessionKey := session.DerivePlatformSessionKey(
		job.OwnerID, worker.TypeClaudeCode,
		session.PlatformContext{
			Platform: "cron",
			BotID:    job.BotID,
			UserID:   job.OwnerID,
			WorkDir:  job.WorkDir,
			ChatID:   job.ID,
		},
	)

	platformKey := map[string]string{"cron_job_id": job.ID}
	title := fmt.Sprintf("cron:%s", job.Name)

	if err := e.bridge.StartSession(ctx, sessionKey, job.OwnerID, job.BotID,
		worker.TypeClaudeCode, job.Payload.AllowedTools, job.WorkDir,
		"cron", platformKey, title,
	); err != nil {
		return "", fmt.Errorf("start cron session: %w", err)
	}

	w := e.sm.GetWorker(sessionKey)
	if w == nil {
		return "", fmt.Errorf("cron executor: worker not found after start")
	}

	prompt := fmt.Sprintf("[cron:%s %s] %s\n%s", job.ID, job.Name,
		job.Payload.Message, time.Now().Format(time.RFC3339))
	prompt += buildDeliverySuffix(job)

	if err := w.Input(ctx, prompt, nil); err != nil {
		return "", fmt.Errorf("cron executor: input prompt: %w", err)
	}

	return sessionKey, e.waitForCompletion(ctx, sessionKey, timeout)
}

func (e *Executor) waitForCompletion(ctx context.Context, sessionID string, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("cron executor: timeout waiting for session %s: %w", sessionID, timeoutCtx.Err())
		case <-ticker.C:
			si, err := e.sm.Get(timeoutCtx, sessionID)
			if err != nil {
				e.log.Warn("cron executor: failed to check session state", "session_id", sessionID, "err", err)
				continue
			}
			// IDLE means the worker finished this turn and is waiting.
			// TERMINATED means the worker exited.
			if si.State != events.StateRunning && si.State != events.StateCreated {
				return nil
			}
		}
	}
}

// HasCLIDelivery returns true if the job has sufficient platform info
// for CLI-based result delivery.
func HasCLIDelivery(job *CronJob) bool {
	switch job.Platform {
	case "slack":
		return job.PlatformKey["channel_id"] != ""
	case "feishu":
		return job.PlatformKey["chat_id"] != ""
	default:
		return false
	}
}

// buildDeliverySuffix appends CLI delivery instructions to the cron prompt.
func buildDeliverySuffix(job *CronJob) string {
	if job.Platform == "" || job.Platform == "cron" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(job.Payload.Message), "[SILENT]") {
		return ""
	}
	switch job.Platform {
	case "slack":
		return buildSlackDelivery(job)
	case "feishu":
		return buildFeishuDelivery(job)
	default:
		return ""
	}
}

func buildSlackDelivery(job *CronJob) string {
	ch := job.PlatformKey["channel_id"]
	if ch == "" {
		return ""
	}
	cmd := fmt.Sprintf("hotplex slack send-message --text \"结果内容\" --channel %s", ch)
	if ts := job.PlatformKey["thread_ts"]; ts != "" {
		cmd += fmt.Sprintf(" --thread-ts %s", ts)
	}
	return fmt.Sprintf(deliveryBlockFmt, cmd)
}

func buildFeishuDelivery(job *CronJob) string {
	chatID := job.PlatformKey["chat_id"]
	if chatID == "" {
		return ""
	}
	cmd := fmt.Sprintf("lark-cli im +messages-send --chat-id %s --text \"结果内容\"", chatID)
	return fmt.Sprintf(deliveryBlockFmt, cmd)
}

const deliveryBlockFmt = `

## 结果投递（必须执行）

任务执行完成后，必须通过以下命令将结果投递给用户。将 "结果内容" 替换为实际执行结果。

` + "```bash\n%s\n```"
