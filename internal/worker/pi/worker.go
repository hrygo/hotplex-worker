package pi

import (
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypePimon, func() (worker.Worker, error) {
		// // TODO: Implement actual Pi-Mono worker.
		return noop.NewWorker(), nil
	})
}
