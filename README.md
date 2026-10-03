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
STAGE2_ADVANCED_EXPERIENCE = PASS_LOCAL
```

Stage 1 is frozen and was validated with LocalStack Ultimate, Kind, Terraform CLI, and local Kubernetes controllers. Stage 2 adds a local operator console above the existing engine. Its browser-driven draft, create, exact Plan approval, Apply, Ready, update, and delete/Destroy lifecycle passed locally with Foundry Local inference and current-source images. Stage 3 and real AWS validation have not started.

The 2026-10-02 closure run passed all 13 acceptance gates: a fresh full Stage 1 regression exited zero, initial and final quality checks passed, and owned test resources were cleaned. Source and running artifact identity were checked during that run. The documentation retains Chinese results and browser screenshots.

- [第二阶段本地端到端验收报告](docs/第二阶段本地端到端验收报告.md): 7 acceptance cases, cleanup regression, methods, results, and 16 real browser screenshots.

Validated locally:

- PlatformEnvironment creation and InfraStack/TerraformRun reconciliation
- Terraform Plan, explicit approval, and exact saved-plan Apply against LocalStack
- Kind runtime readiness, ResourceSet SSA/inventory, and bootstrap resources
- Safe runtime update and a new-generation Terraform NoChange
- Manager restart during active Plan and after Ready, without duplicate Apply
- Two-target isolation and the configured concurrent-Apply limit
- Fail-closed handling of missing approval, wrong target, and incomplete discovery
- ResourceSet prune, Terraform Destroy, finalizer cleanup, and owned-resource cleanup

Stage 2 experience capabilities:

- Go `platform-api` read model over Kubernetes resources; Kubernetes remains the source of truth
- React/TypeScript console for environments, conditions, timeline, Terraform evidence, and plan changes
- Exact-plan approval through the existing immutable `ChangeApproval`; there is no Apply API or UI action
- AI-assisted typed drafts with deterministic validation and a separate explicit submit action
- Optional plan visualization artifact derived from `terraform show -json`; it is not used by approval or Apply

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
- Node.js 20.19+ and npm for the Vite console

The repository does not start a second LocalStack container. This keeps the local AWS boundary explicit and prevents accidental use of real AWS credentials or endpoints.

Local development responsibilities are separate: Kind provides the Kubernetes runtime, the externally managed LocalStack Ultimate instance simulates AWS infrastructure, and Foundry Local provides Stage 2 LLM inference.

## Local console

Run the existing control plane against the management Kind cluster first. In another PowerShell terminal, use the same local kubeconfig and LocalStack artifact bucket as the manager, and start/configure Foundry Local before starting the API:

```powershell
$env:PCP_ARTIFACT_ENDPOINT = "http://localhost:4566"
$env:PCP_ARTIFACT_REGION = "us-east-1"
$env:PCP_ARTIFACT_BUCKET = "<the manager's artifact bucket>"
foundry server restart --port 39839 --idle-timeout 0
$env:FOUNDRY_LOCAL_ENDPOINT = "http://127.0.0.1:39839"
$env:FOUNDRY_LOCAL_MODEL = "phi-4-mini"
go run ./cmd/platform-api
```

The API binds to `127.0.0.1:8090` by default and refuses non-local artifact or inference endpoints. Stage 2 AI drafts use Foundry Local by default.

For deterministic offline development only, set `AI_PROVIDER=deterministic`. Foundry Local drafts are advisory; the platform API validates them and requires a separate explicit human action to create a `PlatformEnvironment`.

In another terminal:

```powershell
npm --prefix web ci
npm --prefix web run dev
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` to the local API. This unauthenticated console is for a local operator only; do not expose it beyond the developer workstation. With GNU Make installed, `make api`, `make web`, `make web-build`, and `make stage2-check` are available.

### Environment class management

Open **Environment classes / 环境类别管理** in the sidebar. The form creates a
cluster-scoped `EnvironmentClass`; classes are stored in Kubernetes. Fill in the
name/version, target, default and allowed regions, pinned infrastructure
repository commit, backend configuration reference, runner image/identity,
capacity limits and approval policy. Optional runtime settings are under Advanced
settings.

Use **Validate configuration** to check typed inputs and Kubernetes admission
without saving anything. **Create class** saves only the class, then the page
refreshes its controller conditions. When Ready, **Use this class** opens the
builder with that class selected and its configured region choices. Ready means
class admission/reconciliation; source access, backend availability, runner image
availability and the infrastructure lifecycle are checked later. The referenced
ConfigMap containing `backend.hcl` must exist in each environment namespace before
that environment can execute Terraform.

Class specifications are immutable. **Copy as new version** preserves the
configuration in a new form and requires a unique class name/version. Existing
environments keep their original class. An unused class may be deleted after name
confirmation; the API checks current environment references and Kubernetes
UID/resource-version preconditions, and leaves controller finalizers in charge.
Class management retains backend configuration and infrastructure. Inline Secret
resources and credentials are rejected; legacy credential values are redacted in
class details and those classes cannot be copied from the UI.

The local API provides `GET /api/classes`, `GET /api/classes/{name}`,
`POST /api/classes/validate`, `POST /api/classes`, and
`DELETE /api/classes/{name}`. Class changes require a new class. Creating or
validating a class neither submits an environment nor approves a Terraform Plan.

The E2E harness needs Docker access to create three disposable Kind clusters and network reachability from Kind nodes to the host LocalStack endpoint. It never creates or deletes the externally managed LocalStack container.

## Validation

Run the deterministic repository checks from the root:

```powershell
go test ./...
go build ./cmd/controller ./cmd/terraform-runner ./cmd/platform-api
go vet ./...
go test -race ./...
npm --prefix web ci
npm --prefix web run build
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

For Stage 2 browser regression, run the same command with `-Stage2Session`. After the full Stage 1 lanes, `stage2-session.json` in the run directory supplies the management kubeconfig, artifact bucket, and a fresh EnvironmentClass. Use those values with the local console setup above. Complete draft → explicit create → Plan approval → Ready → update → name-confirmed delete → separate Destroy approval, then stop the console processes and remove that run's `stage2-hold.flag`. The harness completes cleanup and reports its final result. An intermediate session summary is not the final Stage 1 PASS.

The lanes are:

- `all`: lifecycle, recovery, multi-target, fail-closed, and cleanup
- `lifecycle`: create, approval/apply, safe update, and destroy
- `recovery`: lifecycle with manager restarts during and after reconciliation
- `multitarget`: two target clusters, identity isolation, and concurrency limit
- `failclosed`: rejection paths with no runtime side effects

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
cmd/platform-api/             local console API entrypoint
cmd/terraform-runner/        Terraform runner entrypoint
config/                      CRDs, RBAC, manager security manifests
docs/architecture.md         current lifecycle and safety invariants
internal/console/             read model, exact approval, and draft API
internal/controller/         lifecycle and runtime controllers
internal/runtime/            SSA, readiness, inventory, runtime bootstrap
internal/target/             identity, discovery, client isolation, AWS seams
internal/terraform/          source closure, saved-plan execution, evidence
test/fixtures/terraform/     LocalStack Terraform fixture
e2e/                         Stage 1 real E2E harness
web/                         React + TypeScript operator console
```
