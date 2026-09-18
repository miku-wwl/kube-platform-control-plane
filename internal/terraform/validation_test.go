package terraform

import (
	"strings"
	"testing"
	"time"
)

func TestValidateApplyRejectsExpiredPlanAndApprovalMismatch(t *testing.T) {
	binding := validPlanBinding()
	apply := validApplyBinding()
	apply.Now = binding.ExpiresAt.Add(time.Second)
	if err := ValidateApply(binding, apply); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired plan error = %v", err)
	}
	apply.Now = binding.CreatedAt.Add(time.Minute)
	apply.ApprovalUID = "replacement-approval-uid"
	apply.ApprovalPlanUID = "different-plan"
	if err := ValidateApply(binding, apply); err == nil || !strings.Contains(err.Error(), "approval binding") {
		t.Fatalf("approval mismatch error = %v", err)
	}
}

func TestValidateApplyRejectsIdentityAndAdvisoryStateChanges(t *testing.T) {
	binding := validPlanBinding()
	apply := validApplyBinding()
	apply.ExecutionPlatformIdentityDigest = "sha256:wrong"
	if err := ValidateApply(binding, apply); err == nil || !strings.Contains(err.Error(), "platform identity") {
		t.Fatalf("platform mismatch error = %v", err)
	}
	apply = validApplyBinding()
	apply.CurrentStateSerial = 8
	if err := ValidateApply(binding, apply); err == nil || !strings.Contains(err.Error(), "early stale") {
		t.Fatalf("state mismatch error = %v", err)
	}
}

func validPlanBinding() PlanBinding {
	return PlanBinding{
		PlanUID:                         "plan-uid",
		PlanDigest:                      "sha256:plan",
		ExecutionContextDigest:          "sha256:context",
		ExecutionTargetIdentityDigest:   "sha256:target",
		ExecutionPlatformIdentityDigest: "sha256:platform",
		CreatedAt:                       time.Unix(100, 0),
		ExpiresAt:                       time.Unix(200, 0),
		ObservedStateLineage:            "lineage",
		ObservedStateSerial:             7,
	}
}

func validApplyBinding() ApplyBinding {
	return ApplyBinding{
		PlanUID:                         "plan-uid",
		PlanDigest:                      "sha256:plan",
		ExecutionContextDigest:          "sha256:context",
		ExecutionTargetIdentityDigest:   "sha256:target",
		ExecutionPlatformIdentityDigest: "sha256:platform",
		ApprovalUID:                     "approval-uid",
		ApprovalPlanUID:                 "plan-uid",
		ApprovalPlanDigest:              "sha256:plan",
		ApprovalExecutionContextDigest:  "sha256:context",
		CurrentStateLineage:             "lineage",
		CurrentStateSerial:              7,
		Now:                             time.Unix(150, 0),
	}
}
