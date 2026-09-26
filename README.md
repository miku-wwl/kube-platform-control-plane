# kube-platform-control-plane

Kubernetes control plane for declarative platform environments. It keeps infrastructure mutation explicit and reviewable:

```text
PlatformEnvironment
  -> InfraStack
      -> Terraform Plan
      -> ChangeApproval
      -> exact saved-plan Apply
  -> ResourceSet runtime reconciliation
  -> EnvironmentReady
```

The controller manages infrastructure execution, target discovery, runtime SSA/inventory/prune, readiness, cleanup, and restart reconstruction. Terraform is an executor; it is not the top-level control plane.

## Current status

```text
STAGE1_E2E_AUTOMATION = IMPLEMENTED
STAGE1_LIVE_E2E = PASS_LOCAL (run 20260923220530-54550173; lifecycle/recovery/runtime update/multi-target/fail-closed/cleanup)
PHASE12_FINAL_FREEZE = PASS
STAGE1_LOCAL_VALIDATION = PASS_LOCAL
PRODUCTION_AWS_VALIDATION = NOT_RUN (outside Stage 1)
```

Stage 1 E2E evidence is local-only: LocalStack Ultimate, Kind, Terraform CLI, and local Kubernetes controllers. Real AWS is outside this stage. Stage 2 is not started.

Authoritative documents:

- [Detailed frozen design](platform-control-plane-detailed-design-v2.0.3-final-freeze-locked-r3.md)
- [Brief frozen design](platform-control-plane-brief-design-v2.0.3-final-freeze-locked-r3.md)
- [Gap analysis](platform-control-plane-gap-analysis-v2.0.3-final-freeze-locked-r3.md)
- [Local production readiness report](LOCAL-PRODUCTION-READINESS-REPORT.md)
- [Stage 1 E2E and evidence guide](e2e/README.md)

## Local prerequisites

- Go toolchain matching `go.mod`
- Terraform CLI `1.14.0`
- Docker Desktop
- Kind and `kubectl`
- LocalStack Ultimate started externally at `http://localhost:4566`
- Docker Desktop with permission to create temporary Kind clusters

The repository does not start a second LocalStack container. This keeps the local AWS boundary explicit and prevents accidental use of real AWS credentials or endpoints.

## Validation

Run the deterministic repository checks from the root:

```powershell
go test ./...
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

Run the Kind integration lanes with the target context explicitly selected:

```powershell
$env:PCP_TARGET_CONTEXT = "kind-pcp-target-local"
go test ./internal/runtime -run 'TestKind' -count=1
go test ./internal/controller -run 'TestKindResourceSetControllerSSAInventoryAndPrune' -count=1
```

The complete Stage 1 lifecycle, restart recovery, runtime update, multi-target, and fail-closed evidence from this host is in `artifacts/e2e/20260923220530-54550173/summary.txt` (Git-ignored). Broader readiness is summarized in [LOCAL-PRODUCTION-READINESS-REPORT.md](LOCAL-PRODUCTION-READINESS-REPORT.md); this Stage 1 result makes no production AWS claim.

## Repository layout

```text
api/                         CRD Go types
cmd/controller/              management controller entrypoint
cmd/terraform-runner/        Terraform runner entrypoint
config/                      CRDs, RBAC, manager security manifests
internal/controller/         lifecycle and runtime controllers
internal/runtime/            SSA, readiness, inventory, Valkey bootstrap
internal/target/             identity, discovery, client isolation, AWS seams
internal/terraform/          source closure, saved-plan execution, evidence
test/fixtures/terraform/     LocalStack Terraform fixture
e2e/                         Stage 1 real E2E harness and evidence guide
```
