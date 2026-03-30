package pi

import (
	"hotplex-worker/internal/worker"
	"hotplex-worker/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypePimon, func() (worker.Worker, error) {
		// // TODO: Implement actual Pi-Mono worker.
		return noop.NewWorker(), nil
	})
}
