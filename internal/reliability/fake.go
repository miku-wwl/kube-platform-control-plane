package reliability

type Fault string

const (
	FaultNone              Fault = "None"
	FaultRestart           Fault = "Restart"
	FaultTimeout           Fault = "Timeout"
	FaultStalePlan         Fault = "StalePlan"
	FaultExpiredPlan       Fault = "ExpiredPlan"
	FaultArtifactFailure   Fault = "ArtifactFailure"
	FaultIndeterminate     Fault = "Indeterminate"
	FaultTargetUnavailable Fault = "TargetUnavailable"
	FaultDeleteDuringApply Fault = "DeleteDuringApply"
)

type Outcome string

const (
	OutcomeSucceeded         Outcome = "Succeeded"
	OutcomeFailed            Outcome = "Failed"
	OutcomeTimedOut          Outcome = "TimedOut"
	OutcomeEarlyStale        Outcome = "EarlyStalePlan"
	OutcomePlanExpired       Outcome = "PlanExpired"
	OutcomeArtifactsMissing  Outcome = "ArtifactsMissing"
	OutcomeIndeterminate     Outcome = "Indeterminate"
	OutcomeTargetUnavailable Outcome = "TargetUnavailable"
	OutcomeDeletionHeld      Outcome = "DeletionHeld"
	OutcomeReconstructed     Outcome = "Reconstructed"
)

type Result struct {
	Outcome           Outcome
	MutationKnown     bool
	RetryAllowed      bool
	RequiresFreshPlan bool
}

func Inject(fault Fault) Result {
	switch fault {
	case FaultNone:
		return Result{Outcome: OutcomeSucceeded, MutationKnown: true}
	case FaultRestart:
		return Result{Outcome: OutcomeReconstructed, MutationKnown: false, RetryAllowed: true}
	case FaultTimeout:
		return Result{Outcome: OutcomeTimedOut, MutationKnown: false}
	case FaultStalePlan:
		return Result{Outcome: OutcomeEarlyStale, RequiresFreshPlan: true}
	case FaultExpiredPlan:
		return Result{Outcome: OutcomePlanExpired, RequiresFreshPlan: true}
	case FaultArtifactFailure:
		return Result{Outcome: OutcomeArtifactsMissing, MutationKnown: true}
	case FaultIndeterminate:
		return Result{Outcome: OutcomeIndeterminate, MutationKnown: false}
	case FaultTargetUnavailable:
		return Result{Outcome: OutcomeTargetUnavailable, RetryAllowed: true}
	case FaultDeleteDuringApply:
		return Result{Outcome: OutcomeDeletionHeld, MutationKnown: false}
	default:
		return Result{Outcome: OutcomeFailed}
	}
}
