package main

import (
	"testing"

	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
)

func TestStandbyReadinessDoesNotOpenExecutionAdmission(t *testing.T) {
	elected := make(chan struct{})
	gate := reliability.NewGate(1, 1)
	gate.CloseAdmission()
	check := leaderExecutionReady(elected, gate)
	work := reliability.WorkItem{ID: "apply", Kind: reliability.KindApply, Stack: "stack"}
	if err := check(nil); err != nil {
		t.Fatalf("healthy standby must be ready: %v", err)
	}
	if gate.Ready() || gate.TryAcquire(work) {
		t.Fatal("standby readiness opened execution admission")
	}
	close(elected)
	if err := check(nil); err == nil || gate.TryAcquire(work) {
		t.Fatal("new leader became ready or admitted work before reconstruction")
	}
	gate.Reconstruct(nil)
	if err := check(nil); err != nil || !gate.TryAcquire(work) {
		t.Fatalf("reconstructed leader must be ready and able to admit work: %v", err)
	}
}

func TestLeaderReadinessRetainsReconstructionSafetyFailure(t *testing.T) {
	elected := make(chan struct{})
	close(elected)
	gate := reliability.NewGate(1, 1)
	gate.CloseAdmission()
	gate.Reconstruct([]reliability.WorkItem{
		{ID: "apply-a", Kind: reliability.KindApply, Stack: "same"},
		{ID: "apply-b", Kind: reliability.KindApply, Stack: "same"},
	})
	if err := leaderExecutionReady(elected, gate)(nil); err == nil {
		t.Fatal("leader reported ready despite ambiguous active executions")
	}
}
