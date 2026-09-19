package day2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/miku-wwl/kube-platform-control-plane/internal/reliability"
)

type DriftOutcome string

const (
	NoDrift          DriftOutcome = "NoDrift"
	DriftDetected    DriftOutcome = "DriftDetected"
	DriftCheckFailed DriftOutcome = "DriftCheckFailed"
)

func ClassifyDrift(planExitCode int, hasChanges bool, err error) DriftOutcome {
	if err != nil || (planExitCode != 0 && planExitCode != 2) {
		return DriftCheckFailed
	}
	if hasChanges || planExitCode == 2 {
		return DriftDetected
	}
	return NoDrift
}

type RollbackSelection struct {
	EnvironmentClass string `json:"environmentClass"`
	SourceRevision   string `json:"sourceRevision"`
	ParameterDigest  string `json:"parameterDigest"`
}

func ValidateRollbackSelection(selection RollbackSelection, currentPlanUID, reusedPlanUID string) error {
	if selection.EnvironmentClass == "" || selection.SourceRevision == "" || selection.ParameterDigest == "" {
		return fmt.Errorf("rollback selection must include class, source revision, and parameter digest")
	}
	if currentPlanUID != "" && currentPlanUID == reusedPlanUID {
		return fmt.Errorf("rollback must create a fresh PlanRun and cannot reuse saved plan %q", reusedPlanUID)
	}
	return nil
}

type CleanupItem struct {
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Action     string `json:"action"`
	Verified   bool   `json:"verified"`
	TargetHash string `json:"targetIdentityDigest"`
}

type CleanupEvidence struct {
	RunUID     string        `json:"runUID"`
	FinishedAt time.Time     `json:"finishedAt"`
	Outcome    string        `json:"outcome"`
	Items      []CleanupItem `json:"items"`
	Skipped    []CleanupItem `json:"skipped,omitempty"`
}

func BuildCleanupEvidence(runUID, outcome string, finishedAt time.Time, items, skipped []CleanupItem) ([]byte, string, error) {
	if runUID == "" || outcome == "" || finishedAt.IsZero() {
		return nil, "", fmt.Errorf("cleanup evidence requires run UID, outcome, and finished time")
	}
	evidence := CleanupEvidence{RunUID: runUID, Outcome: outcome, FinishedAt: finishedAt.UTC(), Items: append([]CleanupItem(nil), items...), Skipped: append([]CleanupItem(nil), skipped...)}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return encoded, "sha256:" + hex.EncodeToString(sum[:]), nil
}

type StatefulPolicy string

const (
	StatefulDelete StatefulPolicy = "Delete"
	StatefulRetain StatefulPolicy = "Retain"
)

type StatefulObservation struct {
	ExternalStateEffect bool   `json:"externalStateEffect"`
	Verified            bool   `json:"verified"`
	PVCUID              string `json:"pvcUID,omitempty"`
	PVUID               string `json:"pvUID,omitempty"`
	StorageClass        string `json:"storageClass,omitempty"`
	ReclaimPolicy       string `json:"reclaimPolicy,omitempty"`
	VolumeHandle        string `json:"volumeHandle,omitempty"`
	RestoreEvidenceRef  string `json:"restoreEvidenceRef,omitempty"`
}

func ValidateStatefulCleanup(policy StatefulPolicy, observation StatefulObservation) error {
	if policy == StatefulDelete && observation.ExternalStateEffect && !observation.Verified {
		return fmt.Errorf("unverified stateful cleanup must block infrastructure destroy")
	}
	if policy == StatefulRetain {
		if observation.PVCUID == "" || observation.PVUID == "" || observation.StorageClass == "" || observation.ReclaimPolicy == "" {
			return fmt.Errorf("retained stateful evidence requires PVC/PV identity and storage policy")
		}
		if observation.RestoreEvidenceRef == "" {
			return fmt.Errorf("retained stateful evidence requires restore evidence")
		}
	}
	return nil
}

type Fault string

const (
	FaultAWS429             Fault = "aws-429"
	FaultS3Unavailable      Fault = "s3-unavailable"
	FaultCredentialExpiry   Fault = "credential-expiry"
	FaultTargetUnavailable  Fault = "target-api-unavailable"
	FaultDNSFailure         Fault = "dns-failure"
	FaultLeaderDeath        Fault = "leader-death"
	FaultRunnerPodLoss      Fault = "runner-pod-loss"
	FaultMissingEvidence    Fault = "missing-terminal-evidence"
	FaultPlanExpiry         Fault = "plan-expiry"
	FaultApprovalSubstitute Fault = "approval-substitution"
	FaultArtifactMismatch   Fault = "artifact-mismatch"
	FaultTargetMismatch     Fault = "target-identity-mismatch"
	FaultNetworkPartition   Fault = "network-partition"
)

type FaultResult struct {
	Fault            Fault `json:"fault"`
	FailClosed       bool  `json:"failClosed"`
	RequiresRecovery bool  `json:"requiresRecovery"`
}

func RunFaultMatrix(faults []Fault, handler func(Fault) FaultResult) []FaultResult {
	results := make([]FaultResult, 0, len(faults))
	for _, fault := range faults {
		if handler == nil {
			results = append(results, FaultResult{Fault: fault, FailClosed: true, RequiresRecovery: true})
			continue
		}
		result := handler(fault)
		result.Fault = fault
		results = append(results, result)
	}
	return results
}

type SoakResult struct {
	EnvironmentCount int
	Metrics          reliability.Metrics
}

func RunLocalSoak(environmentCount, maxPlans, maxApplies int) SoakResult {
	items := make([]reliability.WorkItem, 0, environmentCount*2)
	for index := 0; index < environmentCount; index++ {
		stack := fmt.Sprintf("env-%d", index)
		items = append(items, reliability.WorkItem{ID: fmt.Sprintf("plan-%d", index), Kind: reliability.KindPlan, Stack: stack})
		items = append(items, reliability.WorkItem{ID: fmt.Sprintf("apply-%d", index), Kind: reliability.KindApply, Stack: stack})
	}
	return SoakResult{EnvironmentCount: environmentCount, Metrics: reliability.Execute(items, maxPlans, maxApplies)}
}
