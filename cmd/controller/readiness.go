package main

import (
	"net/http"

	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

// A standby does not reconstruct executions until it acquires leadership. Its
// readiness must not open execution admission. After election, reconstruction
// remains mandatory before the leader can report ready or admit new work.
func leaderExecutionReady(elected <-chan struct{}, gate *reliability.Gate) healthz.Checker {
	return func(request *http.Request) error {
		select {
		case <-elected:
			return gate.ReadyCheck(request)
		default:
			return nil
		}
	}
}
