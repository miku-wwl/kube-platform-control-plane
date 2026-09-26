# kube-platform-control-plane

Kubernetes control plane for declarative platform environments. It keeps infrastructure mutation explicit and reviewable:

```text
PlatformEnvironment
  -> EnvironmentClass
  -> InfraStack
      -> Terraform Plan
      -> ChangeApproval
      -> exact saved-plan Apply
  -> TargetDiscovery
  -> ResourceSet runtime reconciliation
  -> EnvironmentReady
```

The controller manages infrastructure execution, target discovery, runtime SSA/inventory/prune, readiness, cleanup, and restart reconstruction. Terraform is an executor; it is not the top-level control plane.

## Current status

```text
STAGE1_LOCAL_VALIDATION = PASS_LOCAL
STAGE1_E2E = PASS
STAGE1_FREEZE = PASS
```

Stage 1 was validated with LocalStack Ultimate, Kind, Terraform CLI, and local Kubernetes controllers. Real AWS validation has not been performed. Stage 2 and Stage 3 have not started.

Validated locally:

- PlatformEnvironment creation and InfraStack/TerraformRun reconciliation
- Terraform Plan, explicit approval, and exact saved-plan Apply against LocalStack
- Kind runtime readiness, ResourceSet SSA/inventory, and bootstrap resources
- Safe runtime update and a new-generation Terraform NoChange
- Manager restart during active Plan and after Ready, without duplicate Apply
- Two-target isolation and the configured concurrent-Apply limit
- Fail-closed handling of missing approval, wrong target, and incomplete discovery
- ResourceSet prune, Terraform Destroy, finalizer cleanup, and owned-resource cleanup

Architecture overview:

- [Current architecture and invariants](docs/architecture.md)

## Core resources

| Resource | Purpose |
|---|---|
| PlatformEnvironment | User-facing desired environment and lifecycle owner |
| EnvironmentClass | Reusable policy and typed infrastructure/runtime inputs |
| InfraStack | Controller-materialized infrastructure intent |
| TerraformRun | Immutable Plan, Apply, or Destroy attempt |
| ChangeApproval | Approval bound to exact Plan evidence |
| ResourceSet | Runtime SSA, ownership inventory, readiness, and prune |

## Local prerequisites

- Go toolchain matching `go.mod`
- Terraform CLI `1.14.0`
- Docker Desktop
- Kind and `kubectl`
- LocalStack Ultimate started externally at `http://localhost:4566`

The repository does not start a second LocalStack container. This keeps the local AWS boundary explicit and prevents accidental use of real AWS credentials or endpoints.

The E2E harness needs Docker access to create three disposable Kind clusters and network reachability from Kind nodes to the host LocalStack endpoint. It never creates or deletes the externally managed LocalStack container.

## Validation

Run the deterministic repository checks from the root:

```powershell
go test ./...
go build ./cmd/controller ./cmd/terraform-runner
go vet ./...
go test -race ./...
git diff --check
terraform fmt -check test/fixtures/terraform/localstack-basic
terraform -chdir=test/fixtures/terraform/localstack-basic init -backend=false -input=false
terraform -chdir=test/fixtures/terraform/localstack-basic validate
```

Run the complete real local Stage 1 harness. LocalStack Ultimate must already be running; the harness creates and deletes three unique Kind clusters and never starts LocalStack itself:

```powershell
# Windows equivalent when GNU make is not installed
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-Stage1E2E.ps1 -Suite all

# On CI/Linux/macOS with make
make e2e
```

Component lanes are available as `make e2e-lifecycle`, `make e2e-recovery`, `make e2e-multitarget`, and `make e2e-failclosed`. The harness writes only text evidence under `artifacts/e2e/<run-id>/`, which is ignored by Git, and verifies cleanup of its owned Kind clusters, LocalStack buckets, temporary Git server, and source directory in `finally`.

The lanes are:

- `all`: lifecycle, recovery, multi-target, fail-closed, and cleanup
- `lifecycle`: create, approval/apply, safe update, and destroy
- `recovery`: lifecycle with manager restarts during and after reconciliation
- `multitarget`: two target clusters, identity isolation, and concurrency limit
- `failclosed`: rejection paths with no runtime side effects

Run the Kind integration lanes with the target context explicitly selected:

```powershell
$env:PCP_TARGET_CONTEXT = "kind-pcp-target-local"
go test ./internal/runtime -run 'TestKind' -count=1
go test ./internal/controller -run 'TestKindResourceSetControllerSSAInventoryAndPrune' -count=1
```

## Typical development loop

1. Keep changes within the API, controller, runner, or target/runtime package they affect.
2. Run the Go test and vet checks before using an integration environment.
3. For lifecycle or runtime changes, start LocalStack Ultimate externally and run `make e2e`.
4. Inspect the generated summary and logs locally; do not commit temporary E2E output.
5. Treat LocalStack/Kind results as local validation only, not as real-AWS proof.

## Repository layout

```text
api/                         CRD Go types
cmd/controller/              management controller entrypoint
cmd/terraform-runner/        Terraform runner entrypoint
config/                      CRDs, RBAC, manager security manifests
docs/architecture.md         current lifecycle and safety invariants
internal/controller/         lifecycle and runtime controllers
internal/runtime/            SSA, readiness, inventory, Valkey bootstrap
internal/target/             identity, discovery, client isolation, AWS seams
internal/terraform/          source closure, saved-plan execution, evidence
test/fixtures/terraform/     LocalStack Terraform fixture
e2e/                         Stage 1 real E2E harness
```
