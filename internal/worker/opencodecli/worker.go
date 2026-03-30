package opencodecli

import (
	"hotplex-worker/internal/worker"
	"hotplex-worker/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypeOpenCodeCLI, func() (worker.Worker, error) {
		// // TODO: Implement actual OpenCode CLI worker.
		return noop.NewWorker(), nil
	})
}
