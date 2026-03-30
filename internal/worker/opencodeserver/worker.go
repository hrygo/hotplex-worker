package opencodeserver

import (
	"hotplex-worker/internal/worker"
	"hotplex-worker/internal/worker/noop"
)

func init() {
	worker.Register(worker.TypeOpenCodeSrv, func() (worker.Worker, error) {
		// // TODO: Implement actual OpenCode Server worker.
		return noop.NewWorker(), nil
	})
}
