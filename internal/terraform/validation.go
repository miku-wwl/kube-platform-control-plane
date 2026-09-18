package terraform

import (
	"fmt"
	"time"
)

type PlanBinding struct {
	PlanUID                         string
	PlanDigest                      string
	ExecutionContextDigest          string
	ExecutionTargetIdentityDigest   string
	ExecutionPlatformIdentityDigest string
	CreatedAt                       time.Time
	ExpiresAt                       time.Time
	ObservedStateLineage            string
	ObservedStateSerial             int64
}

type ApplyBinding struct {
	PlanUID                         string
	PlanDigest                      string
	ExecutionContextDigest          string
	ExecutionTargetIdentityDigest   string
	ExecutionPlatformIdentityDigest string
	ApprovalUID                     string
	ApprovalPlanUID                 string
	ApprovalPlanDigest              string
	ApprovalExecutionContextDigest  string
	CurrentStateLineage             string
	CurrentStateSerial              int64
	Now                             time.Time
}

func ValidateApply(binding PlanBinding, apply ApplyBinding) error {
	if binding.PlanUID == "" || apply.PlanUID == "" || binding.PlanUID != apply.PlanUID {
		return fmt.Errorf("plan UID mismatch")
	}
	if binding.PlanDigest == "" || binding.PlanDigest != apply.PlanDigest {
		return fmt.Errorf("plan digest mismatch")
	}
	if binding.ExecutionContextDigest == "" || binding.ExecutionContextDigest != apply.ExecutionContextDigest {
		return fmt.Errorf("execution context digest mismatch")
	}
	if binding.ExecutionTargetIdentityDigest == "" || binding.ExecutionTargetIdentityDigest != apply.ExecutionTargetIdentityDigest {
		return fmt.Errorf("execution target identity digest mismatch")
	}
	if binding.ExecutionPlatformIdentityDigest == "" || binding.ExecutionPlatformIdentityDigest != apply.ExecutionPlatformIdentityDigest {
		return fmt.Errorf("execution platform identity digest mismatch")
	}
	if !binding.ExpiresAt.IsZero() && !apply.Now.IsZero() && !apply.Now.Before(binding.ExpiresAt) {
		return fmt.Errorf("plan expired")
	}
	if binding.ApprovalRequired() {
		if apply.ApprovalUID == "" || apply.ApprovalPlanUID != binding.PlanUID {
			return fmt.Errorf("approval binding mismatch")
		}
		if apply.ApprovalPlanDigest != binding.PlanDigest || apply.ApprovalExecutionContextDigest != binding.ExecutionContextDigest {
			return fmt.Errorf("approval digest mismatch")
		}
	}
	if binding.ObservedStateLineage != "" && apply.CurrentStateLineage != "" && binding.ObservedStateLineage != apply.CurrentStateLineage {
		return fmt.Errorf("early stale plan: state lineage changed")
	}
	if binding.ObservedStateSerial != 0 && apply.CurrentStateSerial != 0 && binding.ObservedStateSerial != apply.CurrentStateSerial {
		return fmt.Errorf("early stale plan: state serial changed")
	}
	return nil
}

func (p PlanBinding) ApprovalRequired() bool {
	return p.PlanDigest != ""
}
