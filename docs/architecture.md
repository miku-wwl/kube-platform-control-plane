# Architecture

This document describes the current control-plane contracts. It is a concise
guide to the running design, not a phase-by-phase implementation history.

## Lifecycle

~~~text
PlatformEnvironment
    ↓ selects
EnvironmentClass
    ↓ materializes
InfraStack
    ↓ creates immutable attempt
TerraformRun: Plan
    ↓ review boundary
ChangeApproval
    ↓ approves exact plan
TerraformRun: Apply
    ↓ Apply or NoChange produces trusted evidence
TargetDiscovery
    ↓ creates
ResourceSet
    ↓ server-side apply, inventory, readiness
Runtime Reconciliation
    ↓
EnvironmentReady
~~~

One PlatformEnvironment owns one dedicated runtime target. Multiple
environments can target different clusters.

Creation starts from the user-facing PlatformEnvironment. Its EnvironmentClass
defines the approved infrastructure source, backend, execution identity, and
runtime intent. The controller materializes and reconciles an InfraStack;
Terraform is an execution engine, not the top-level control plane.

Infrastructure changes require a Plan followed by an explicit ChangeApproval.
Apply consumes the exact saved plan produced by that Plan attempt. A successful
Apply or a proven NoChange both record a last-converged source closure and
trusted target discovery. Runtime objects are not applied until the current
infrastructure and target evidence pass admission checks.

Deletion is finalizer-driven. The controller waits for active mutation to
finish, prunes ResourceSet-owned runtime objects, plans and applies Terraform
destroy from the last converged closure, records infrastructure removal, then
removes finalizers.

## Responsibilities

| Resource | Responsibility |
|---|---|
| PlatformEnvironment | User-facing desired state and lifecycle owner |
| EnvironmentClass | Reusable, typed environment policy and execution inputs |
| InfraStack | Materialized infrastructure intent and convergence state |
| TerraformRun | Immutable Plan, Apply, or Destroy execution attempt |
| ChangeApproval | Explicit approval bound to one exact Plan and its evidence |
| TargetDiscovery | Trusted identity and connection profile for the target |
| ResourceSet | Runtime objects, SSA ownership, inventory, readiness, and prune |

TerraformRun artifacts and Kubernetes status provide durable evidence for
reconciliation and recovery. Controllers derive progress from persisted
resources and artifacts rather than relying on in-memory workflow state.

Readiness is generation-aware: a condition from an older desired generation
does not make the current object Ready. Job completion alone is not environment
readiness; infrastructure evidence, trusted target identity, runtime object
readiness, and inventory must converge. ResourceSet pruning is limited to
objects recorded as owned by that ResourceSet.

Plan, Apply, and Destroy attempts are immutable records. A retry or desired
input change does not rewrite an earlier attempt or its approval; a new attempt
must be admitted against the current generation and its own evidence.

## Safety invariants

- **Exact-plan execution:** Apply and Destroy consume the saved plan artifact
  referenced by the approved execution record; they do not create a new plan
  implicitly.
- **Approval boundary:** approval is bound to the PlanRun UID, plan digest,
  execution-context digest, effective-input digest, and plan-report digest.
  Any changed input requires a new Plan and approval.
- **Mutation fence:** runtime mutation is denied while infrastructure is not
  current and ready, the target is stale or inaccessible, the environment is
  deleting, or a mutation fence is active.
- **Idempotency:** generation-aware immutable attempts, stable ownership, and
  persisted terminal evidence prevent duplicate execution and ambiguous retries.
- **Reconciliation:** current-generation conditions and observed state, not
  command submission alone, determine readiness.
- **Target isolation:** infrastructure execution identity, runtime target
  identity, and target connection profile are distinct. A client is created
  only from trusted discovery and must match the intended account and cluster.
- **Fail closed:** missing approval, invalid identity, incomplete discovery,
  stale evidence, or failed admission produces no runtime side effect.
- **Recovery:** after manager restart, persisted resources and artifacts
  reconstruct active work and converged state without corrupting approvals or
  repeating completed mutations.

## Identity and artifact boundaries

The Terraform runner uses the ServiceAccount immutably bound to its
TerraformRun. InfrastructureExecutionIdentity authorizes infrastructure
operations. RuntimeTargetIdentity and TargetConnectionProfile select and
authenticate the Kubernetes client used for runtime reconciliation. These
identities are never substituted for one another.

Source closure, variables, backend configuration, and execution identity are
captured in the effective Plan input. Content digests bind review and execution
to the same source and artifacts. TargetDiscovery is trusted input to
ResourceSet; Terraform Destroy output is not treated as new target identity.

## Validation boundary

Stage 1 is validated locally with the real Terraform CLI, LocalStack Ultimate,
Kind management and target clusters, and real Kubernetes controllers. This
validates local lifecycle, recovery, target isolation, and fail-closed
behavior. Real AWS IAM, STS, EKS, KMS, networking, quotas, and service semantics
have not been validated.
