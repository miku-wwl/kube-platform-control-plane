package reliability

import (
	"strings"
	"testing"
)

func TestFailureInjectionFailClosed(t *testing.T) {
	cases := []struct {
		fault Fault
		want  Outcome
	}{
		{FaultRestart, OutcomeReconstructed},
		{FaultTimeout, OutcomeTimedOut},
		{FaultStalePlan, OutcomeEarlyStale},
		{FaultExpiredPlan, OutcomePlanExpired},
		{FaultArtifactFailure, OutcomeArtifactsMissing},
		{FaultIndeterminate, OutcomeIndeterminate},
		{FaultTargetUnavailable, OutcomeTargetUnavailable},
		{FaultDeleteDuringApply, OutcomeDeletionHeld},
	}
	for _, testCase := range cases {
		result := Inject(testCase.fault)
		if result.Outcome != testCase.want {
			t.Errorf("Inject(%s) = %s, want %s", testCase.fault, result.Outcome, testCase.want)
		}
	}
	if !Inject(FaultStalePlan).RequiresFreshPlan || !Inject(FaultExpiredPlan).RequiresFreshPlan {
		t.Fatal("stale and expired plans did not require a fresh plan")
	}
	if Inject(FaultIndeterminate).MutationKnown {
		t.Fatal("indeterminate mutation was marked known")
	}
	if !strings.Contains(string(Inject(FaultArtifactFailure).Outcome), "Artifacts") {
		t.Fatal("artifact failure did not remain explicit")
	}
}

func TestScaleGateBounds100To1000Environments(t *testing.T) {
	for _, environmentCount := range []int{100, 500, 1000} {
		items := make([]WorkItem, 0, environmentCount*2)
		for index := 0; index < environmentCount; index++ {
			stack := "stack-" + string(rune('a'+index%26))
			items = append(items, WorkItem{ID: "plan-" + itoa(index), Kind: KindPlan, Stack: stack})
			items = append(items, WorkItem{ID: "apply-" + itoa(index), Kind: KindApply, Stack: stack})
		}
		metrics := Execute(items, 8, 4)
		if metrics.Completed != len(items) || metrics.Submitted != len(items) {
			t.Fatalf("environmentCount=%d metrics=%+v", environmentCount, metrics)
		}
		if metrics.MaxActivePlans > 8 || metrics.MaxActiveApplies > 4 || metrics.MaxStackApplies > 1 || metrics.DuplicateIDs != 0 {
			t.Fatalf("environmentCount=%d gate violation metrics=%+v", environmentCount, metrics)
		}
	}
}

func TestGateSnapshotRestoresActiveSlotsAfterRestart(t *testing.T) {
	first := NewGate(2, 1)
	active := WorkItem{ID: "apply-1", Kind: KindApply, Stack: "stack-a"}
	first.Acquire(active)
	snapshot := first.Snapshot()
	if len(snapshot.Active) != 1 || snapshot.Active[0].ID != active.ID {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	second := NewGate(2, 1)
	if err := second.Restore(snapshot.Active); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if err := second.Restore([]WorkItem{{ID: "apply-2", Kind: KindApply, Stack: "stack-a"}}); err == nil {
		t.Fatal("Restore() accepted a second active apply for one stack")
	}
	second.Release(active)
	second.Acquire(WorkItem{ID: "apply-2", Kind: KindApply, Stack: "stack-a"})
	second.Release(WorkItem{ID: "apply-2", Kind: KindApply, Stack: "stack-a"})
}

func TestGateRejectsDuplicateRunIDs(t *testing.T) {
	gate := NewGate(1, 1)
	item := WorkItem{ID: "apply-1", Kind: KindApply, Stack: "stack-a"}
	if err := gate.Acquire(item); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	gate.Release(item)
	if err := gate.Acquire(item); err == nil {
		t.Fatal("duplicate Acquire() must be rejected")
	}
	metrics := gate.Metrics()
	if metrics.DuplicateIDs != 1 || metrics.Completed != 1 {
		t.Fatalf("duplicate metrics = %+v", metrics)
	}
}

func TestGateEnsureActiveReconstructsAndIsIdempotent(t *testing.T) {
	gate := NewGate(1, 1)
	item := WorkItem{ID: "plan-1", Kind: KindPlan, Stack: "stack-a"}
	if !gate.EnsureActive(item) {
		t.Fatal("EnsureActive() did not reconstruct an active item")
	}
	if !gate.EnsureActive(item) || !gate.TryAcquire(item) {
		t.Fatal("active item reservation was not idempotent")
	}
	if gate.EnsureActive(WorkItem{ID: "plan-2", Kind: KindPlan, Stack: "stack-b"}) {
		t.Fatal("EnsureActive() exceeded plan capacity")
	}
	gate.Release(WorkItem{ID: item.ID, Kind: KindApply, Stack: "wrong-stack"})
	if got := gate.Metrics().Completed; got != 1 {
		t.Fatalf("completed metrics after reconstructed release = %d, want 1", got)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
