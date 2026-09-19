package day2

import (
	"testing"
	"time"
)

func TestDriftIsReadOnlyClassification(t *testing.T) {
	if ClassifyDrift(0, false, nil) != NoDrift || ClassifyDrift(2, true, nil) != DriftDetected || ClassifyDrift(1, false, nil) != DriftCheckFailed {
		t.Fatal("unexpected drift classification")
	}
}

func TestRollbackRequiresFreshPlan(t *testing.T) {
	selection := RollbackSelection{EnvironmentClass: "standard", SourceRevision: "abc", ParameterDigest: "sha256:param"}
	if err := ValidateRollbackSelection(selection, "plan-new", "plan-old"); err != nil {
		t.Fatalf("ValidateRollbackSelection() error = %v", err)
	}
	if err := ValidateRollbackSelection(selection, "plan-old", "plan-old"); err == nil {
		t.Fatal("saved plan reuse was accepted")
	}
}

func TestCleanupEvidenceAndStatefulRetainContract(t *testing.T) {
	encoded, digest, err := BuildCleanupEvidence("run-1", "Succeeded", time.Now(), []CleanupItem{{Kind: "ConfigMap", Name: "x", UID: "uid", Action: "Deleted", Verified: true}}, nil)
	if err != nil || len(encoded) == 0 || digest == "" {
		t.Fatalf("cleanup evidence = %q %q %v", encoded, digest, err)
	}
	if err := ValidateStatefulCleanup(StatefulRetain, StatefulObservation{PVCUID: "pvc", PVUID: "pv", StorageClass: "standard", ReclaimPolicy: "Retain", RestoreEvidenceRef: "runs/restore.json"}); err != nil {
		t.Fatalf("retained stateful evidence error = %v", err)
	}
}

func TestLocalSoakCovers100To1000Environments(t *testing.T) {
	for _, count := range []int{100, 500, 1000} {
		result := RunLocalSoak(count, 8, 4)
		if result.Metrics.Completed != count*2 || result.Metrics.MaxActivePlans > 8 || result.Metrics.MaxActiveApplies > 4 || result.Metrics.MaxStackApplies > 1 {
			t.Fatalf("count=%d result=%+v", count, result)
		}
	}
}
