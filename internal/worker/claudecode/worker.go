package claudecode

import (
	"hotplex-worker/internal/worker"
	"hotplex-worker/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypeClaudeCode, func() (worker.Worker, error) {
		// // TODO: Implement actual Claude Code worker.
		return noop.NewWorker(), nil
	})
}
