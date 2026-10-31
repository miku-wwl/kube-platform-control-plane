# Phase 2.5B refactor handoff

Source baseline: `9d149cc82a3cec21998ae4a780a444c631cad6b4`; delivery branch: `main`.
No deployment, emulator startup or infrastructure mutation was performed.
This document describes remaining work; listed future test files do not yet exist.

## Implementation order and dependencies

| Order | Exact locations | Work and dependencies | Completion evidence |
|---|---|---|---|
| B1 | `internal/terraform/identity.go:EndpointProfile`, `internal/controller/environmentclass.go:buildClassInfraStack`, `internal/controller/infrastack_controller.go:buildPlanRun`, `api/v1alpha1/types.go`, `config/crd/bases/` | Decide explicit endpoint/auth profile admission and immutable identity bindings across artifact, provider and backend consumers. Preserve existing CRD fields and serialized legacy digests; version any necessary additions. Do not auto-detect emulator/hostnames. | Table tests for endpoint/account/credential-profile mismatch and immutability; existing objects still decode and reconcile. |
| B2 | `internal/terraform/job.go:BuildJob`, `internal/controller/terraformrun_controller.go:buildJob`, `cmd/controller/main.go`, `internal/artifacts/s3.go`, `internal/console/server.go:NewArtifactStoreFromEnvironment`, `config/manager/` | Deliver explicit emulator credentials through a reviewed Secret reference or workload identity to each consumer, including runner Jobs. Never copy workstation secrets, put values in CRDs/backend snapshots, or restore hostname-triggered test credentials. Bind selected credential identity to execution evidence. Depends on B1. | Real Job uses only authorized emulator identity; missing credentials fail before mutation. Console loopback restriction remains unless separately designed. |
| B3 | `internal/terraform/executor.go:prepareExecutionProviderOverride`, `internal/terraform/executor_test.go`, `test/fixtures/terraform/aws-compatible-basic/` | Replace legacy ephemeral AWS provider HCL override with fixed versioned module authentication inputs/identity. Retain existing role/account tests and immutable source/lock integrity; do not build a Terraform generator. Depends on B1/B2. | Fixed-module Plan and saved-plan Apply use the same role/input/source closure; no execution-time HCL authoring. |
| B4 | `cmd/controller/main.go:buildTargetClient`, `internal/target/identity.go:MaterializeTarget`, `internal/target/discovery.go`, `internal/target/factory.go`, `internal/controller/infrastack_controller.go:verifyTargetDiscovery` | Configure explicit full target identities and verified incarnation (not context-name fallback) for supplied Kubernetes clusters; keep legacy values without rewriting old digests. Generic registration currently supports account/region `local` and context-name incarnation. Any custom account/region/incarnation currently fails closed rather than being inferred. | Arbitrary cluster distributions resolve only exact registered identities/profiles; replacement-cluster UID, endpoint and credentials cannot cross targets. |
| B5 | `api/v1alpha1/deepcopy_helpers.go`, `api/v1alpha1/zz_generated.deepcopy.go` | Audit and regenerate complete deep copies with pinned controller-gen workflow. Existing handwritten `InfraStackSpec`, `TerraformRunSpec` and `ResourceSetStatus` helpers omit deep copying some nested slices/pointers (e.g. SecretVariables and inventory). Retain needed helpers until generator output replaces them. No API schema weakening. | Mutation of a copy cannot change the source object, including nested runtime JSON, variable selectors, inventory and convergence evidence. |
| B6 | `config/manager/manager.yaml`, `config/manager/networkpolicy.yaml`, `config/rbac/`, `Dockerfile`, `runner.Dockerfile` | Supply immutable OCI image references, target kubeconfig/credentials and narrowly scoped endpoint egress for the actual deployment. Manager image remains a placeholder `platform-control-plane:phase12-local`; it must be replaced by deploy tooling. Verify CNI/DNS selectors, API ports and image architectures externally. Build preferably with Podman; no engine sockets in Pods. | Real supplied-cluster image/deployment identity, controller Ready and endpoint reachability; RBAC/fail-closed semantics retained. |
| B7 | `internal/artifacts/s3.go`, `internal/target/aws_adapters.go`, `internal/target/eks_resolver.go`, `internal/terraform/executor.go`, `test/fixtures/terraform/aws-compatible-basic/` | Run MiniStack capability probes for S3/STS and Terraform backend locking, encryption/conditional-write behavior and state consistency. No LocalStack health endpoints or `awslocal`. Existing EKS adapter remains a real AWS integration, not assumed emulator support. Depends on B1–B3/B6. | Record each API semantic as VERIFIED/UNSUPPORTED/UNKNOWN; no synthetic test promoted to emulator proof. |
| B8 | future `e2e/lifecycle_test.go`, `e2e/approval_test.go`, `e2e/artifacts_test.go`, `e2e/recovery_test.go`, `e2e/isolation_test.go`, `e2e/failclosed_test.go`, `e2e/finalizer_test.go`, `e2e/platform_test.go`, `e2e/ownership_test.go` | Port every mapped assertion from ACCEPTANCE_COVERAGE_MATRIX and LEGACY_HARNESS_ASSERTIONS using Go testing/client-go and supplied environments. Test top-level intent/approvals and observe Operator-owned Jobs; do not implement another controller in the test harness. Depends on B4/B6/B7. | Real lifecycle, exact approval negatives, immutable evidence, three restart phases, duplicate prevention, two-target isolation, concurrency, durable fence, update/NoChange and Destroy/finalizer suites. |
| B9 | future `e2e/ownership_test.go`, `e2e/platform_test.go`, `web/src/`, `docs/STAGE2_*`, `docs/phase2.5/` | Capture scoped diagnostics, run/resource UID ownership, image/source identity and durable cleanup evidence. Cleanup requests ordinary deletion only for newly test-owned intent; retain finalizers/state/artifacts if approval or emulator is unavailable. Perform separate console/browser acceptance after backend parity. | Every previous gate explicitly PASS/FAIL/BLOCKED/NOT RUN; human visual/AI quality and browser clicks have their own evidence. |

## Acceptance and cleanup rules

- Consume explicitly supplied management kubeconfig/context and separate target
  profiles. Never provision/delete clusters, start an engine/emulator, mutate DNS,
  alias images or kill workstation processes from tests or business code.
- A run must record resource ownership before creation; prefer isolated supplied
  namespaces/backend keys and exact UID preconditions. Check actual identity,
  not just a name prefix. Failed business cleanup is pending, not authorization
  to force-delete state/finalizers. Preserve all older artifacts and ownership files.
- Restart testing requires a supplied dedicated controller deployment and explicit
  test ownership/authorization. Observe actual leader replacement and reconstruct
  existing Jobs; a fake client does not close restart acceptance.
- The removed script configured a concurrent-Apply limit of one, but its
  multi-target lane approved targets sequentially and contained no explicit
  contention/active-Apply-limit assertion. Add deliberate overlap to prove
  contention, not just two successes. Historical summary wording is insufficient.
- Existing runtime prune validates target/labels/UID before deletion, but
  `internal/controller/resourceset_controller.go:pruneItem` calls Delete without a
  UID precondition. Test and address this read/delete race in B8 rather than
  presenting the current label/UID check as an atomic deletion guarantee.
- The fixture's capacity update is NoChange by design. It proves generation
  reconciliation and no duplicate Apply, not cloud autoscaling. Its resource is
  an S3 bucket; supplied target clusters already exist and are not provisioned by it.
- Historical Stage 2 reports retain incomplete browser acceptance and previous
  cleanup failures. Their old commands are historical; the deleted harness is
  available only from the baseline Git tree.

## Minimum closing checks

Run Go unit/static checks and console build, fixed-fixture format/validate, then
opted-in supplied-cluster/MiniStack suites with separate safe fixture ownership.
Record module/lock/image/Job/run identity, state/workspace keys and post-test
absence of only owned resources. Real AWS remains outside the authorized scope.
Do not declare Phase 2.5 migration complete until all six safety-critical gates
have actual integration evidence.
