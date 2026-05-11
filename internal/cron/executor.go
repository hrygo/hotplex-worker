package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
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
	Transition(ctx context.Context, id string, to events.SessionState) error
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
	sessionKey := session.DeriveCronSessionKey(job.ID, time.Now().UnixNano())

	// Merge platform context so the bridge can inject environment variables (like channel_id).
	platformKey := make(map[string]string)
	if job.PlatformKey != nil {
		for k, v := range job.PlatformKey {
			platformKey[k] = v
		}
	}
	platformKey["cron_job_id"] = job.ID
	title := fmt.Sprintf("cron:%s", job.Name)

	wt := worker.WorkerType(job.Payload.WorkerType)
	if wt == "" {
		wt = worker.TypeClaudeCode // Default
	}

	if err := e.bridge.StartSession(ctx, sessionKey, job.OwnerID, job.BotID,
		wt, job.Payload.AllowedTools, job.WorkDir,
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

	err := e.waitForCompletion(ctx, sessionKey, timeout)

	// Explicitly terminate the session to ensure the worker process exits immediately.
	// We use context.Background() with a short timeout to ensure termination happens
	// even if the original context is canceled.
	termCtx, cancel := context.WithTimeout(context.Background(), base.GracefulShutdownTimeout)
	defer cancel()
	if termErr := e.sm.Transition(termCtx, sessionKey, events.StateTerminated); termErr != nil {
		e.log.Warn("cron executor: failed to terminate session", "session_id", sessionKey, "err", termErr)
	}

	return sessionKey, err
}

func (e *Executor) waitForCompletion(ctx context.Context, sessionID string, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Initial check to avoid waiting for the first ticker tick if the task is near-instant.
	si, err := e.sm.Get(timeoutCtx, sessionID)
	if err == nil && si.State != events.StateRunning && si.State != events.StateCreated {
		return nil
	}

	// 500ms provides a good balance between responsiveness and system overhead.
	ticker := time.NewTicker(500 * time.Millisecond)
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
	if job.Silent {
		return ""
	}
	if job.Platform == "" || job.Platform == "cron" {
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
	cmd := fmt.Sprintf("hotplex slack send-message --channel %s --text \"结果内容\"", ch)
	if ts := job.PlatformKey["thread_ts"]; ts != "" {
		cmd += fmt.Sprintf(" --thread-ts %s", ts)
	}
	return fmt.Sprintf(deliveryBlockFmt, job.Name, cmd)
}

func buildFeishuDelivery(job *CronJob) string {
	chatID := job.PlatformKey["chat_id"]
	if chatID == "" {
		return ""
	}
	cmd := fmt.Sprintf("lark-cli im +messages-send --chat-id %s --markdown \"结果内容\"", chatID)
	return fmt.Sprintf(deliveryBlockFmt, job.Name, cmd)
}

const deliveryBlockFmt = `

## 结果投递（必须执行）

任务「%s」执行完成后，你必须将结果通过以下命令投递给用户。将 "结果内容" 替换为执行结果的简洁摘要（支持 Markdown 格式）。

` + "```bash\n%s\n```" + `

投递完成后，直接结束对话退出。如果投递命令执行失败，在日志中记录错误后仍然退出。`
