package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	TerraformExecutions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pcp_terraform_executions_total",
		Help: "Terraform executions classified by operation and terminal outcome.",
	}, []string{"operation", "outcome"})
	RecoveryEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pcp_recovery_events_total",
		Help: "Fail-closed recovery events requiring observation or operator action.",
	}, []string{"reason"})
	RuntimeReconciliations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pcp_runtime_reconciliations_total",
		Help: "ResourceSet runtime reconciliation outcomes.",
	}, []string{"reason"})
	ReconstructionOverCapacity = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pcp_reconstruction_over_capacity",
		Help: "Whether reconstructed active work exceeds configured capacity.",
	})
	ReconstructionSafetyViolation = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pcp_reconstruction_safety_violation",
		Help: "Whether reconstructed active work contains a safety violation.",
	})
)

func init() {
	crmetrics.Registry.MustRegister(TerraformExecutions, RecoveryEvents, RuntimeReconciliations, ReconstructionOverCapacity, ReconstructionSafetyViolation)
}

func SetReconstructionState(overCapacity, safetyViolation bool) {
	if overCapacity {
		ReconstructionOverCapacity.Set(1)
	} else {
		ReconstructionOverCapacity.Set(0)
	}
	if safetyViolation {
		ReconstructionSafetyViolation.Set(1)
	} else {
		ReconstructionSafetyViolation.Set(0)
	}
}
