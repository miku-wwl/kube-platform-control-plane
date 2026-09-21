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
STAGE1_FREEZE = PASS
PHASE12_FINAL_FREEZE = PASS
LOCAL_VALIDATION_PASS = PASS
```

Evidence is local-only: LocalStack Ultimate, Kind, Terraform CLI, fake/injectable AWS contracts, and local Kubernetes controllers. Real AWS is intentionally deferred to Phase 13. Stage 2 is not started.

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
- one Kind management cluster and two Kind target clusters

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

Run the Kind integration lanes with the target context explicitly selected:

```powershell
$env:PCP_TARGET_CONTEXT = "kind-pcp-target-local"
go test ./internal/runtime -run 'TestKind' -count=1
go test ./internal/controller -run 'TestKindResourceSetControllerSSAInventoryAndPrune' -count=1
```

The complete lifecycle, restart recovery, multi-target, fail-closed, and AWS-native offline evidence is summarized in [LOCAL-PRODUCTION-READINESS-REPORT.md](LOCAL-PRODUCTION-READINESS-REPORT.md). The evidence boundary is explicit: local emulators and fake adapters do not prove production AWS semantics.

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
e2e/                         Stage 1 E2E evidence guide
```
