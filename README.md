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
LOCAL_E2E = PASS_LOCAL
STAGE1_FREEZE = PASS
STAGE2_ADVANCED_EXPERIENCE = PASS_LOCAL
```

Stage 1 is frozen and was validated with LocalStack Ultimate, Kind, Terraform CLI, and local Kubernetes controllers. Stage 2 adds a local operator console above the existing engine. Its draft, create, exact Plan approval, Apply, Ready, update, and delete/Destroy lifecycle passed locally in the 2026-10-02/03 runs. Class/template management passed browser validation on 2026-10-03/04. These are dated acceptance results; Stage 3 and real AWS validation have not started.

The 2026-10-02 closure run passed all 13 acceptance gates: a fresh full Stage 1 regression exited zero, initial and final quality checks passed, and owned test resources were cleaned. Source and running artifact identity were checked during that run. The documentation retains Chinese results and browser screenshots.

- [网页手动测试步骤与截图](docs/环境类别与模板网页手动测试及验收报告.md): class/template tests and environment lifecycle steps, with each screenshot's validation period identified.

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
- Environment class management, Kind starter presets, persisted project templates and independent configuration copies
- Persistent English/Chinese console language selection
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
- Docker Desktop
- Kind and `kubectl`
- Git and Python for the disposable pinned Git fixture server
- LocalStack at `http://localhost:4566` (an existing service is reused; the harness can bootstrap an owned S3/STS container)
- Node.js 20.19+ and npm for the Vite console

The E2E runner uses Terraform `1.14.0` inside the pinned terraform-runner image;
host Terraform, Helm and standalone Kustomize are not prerequisites. Host
Terraform is only needed for the optional fixture formatting/validation commands
below. Go is needed for local development and the platform/API E2E lane.

The harness checks the requested loopback port and reuses its existing LocalStack
container, including an externally managed Ultimate instance. If none is running
there, it starts `pcp-local-e2e-localstack-<port>` with S3/STS and a run ownership
label, then removes that owned container in cleanup. It uses dummy credentials
and refuses non-local endpoints. External containers are preserved.

When using your own Ultimate deployment, add `-RequireExistingLocalStack` to
prevent automatic container startup if that deployment is absent. For example:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite all -LocalStackEndpoint http://127.0.0.1:4566 -RequireExistingLocalStack
```

Local development responsibilities are separate: Kind provides the Kubernetes runtime, the externally managed LocalStack Ultimate instance simulates AWS infrastructure, and Ollama with Phi-4-mini provides Stage 2 LLM inference.

## Local console

Run the existing control plane against the management Kind cluster first. Start Ollama locally and install `phi4-mini` with `ollama pull phi4-mini` if it is not already installed. In another PowerShell terminal, use the same local kubeconfig and LocalStack artifact bucket as the manager:

```powershell
$env:PCP_ARTIFACT_ENDPOINT = "http://localhost:4566"
$env:PCP_ARTIFACT_REGION = "us-east-1"
$env:PCP_ARTIFACT_BUCKET = "<the manager's artifact bucket>"
$env:AI_PROVIDER = "ollama"
$env:OLLAMA_ENDPOINT = "http://127.0.0.1:11434"
$env:OLLAMA_MODEL = "phi4-mini:latest"
Invoke-RestMethod "$env:OLLAMA_ENDPOINT/api/tags"
go run ./cmd/platform-api
```

The API binds to `127.0.0.1:8090` by default and refuses non-local artifact or inference endpoints. `AI_PROVIDER=ollama`, `OLLAMA_ENDPOINT=http://127.0.0.1:11434`, and `OLLAMA_MODEL=phi4-mini:latest` are the defaults. The endpoint is the server root, without `/api` or `/v1`. The model must match a locally installed tag from `/api/tags`; the API does not download models. Legacy Foundry provider/environment settings are no longer supported, so update any local service wrappers before restarting the API.

For deterministic offline development only, set `AI_PROVIDER=deterministic`. Ollama drafts are advisory; the platform API validates them and requires a separate explicit human action to create a `PlatformEnvironment`. Native `POST /api/chat` uses a JSON schema, non-streaming output, temperature 0 and a 4096-token context. Inference failures return an error and never silently fall back to deterministic generation.

To check real local inference (English/Chinese, cache selection, explicit form fields):

```powershell
$env:PCP_RUN_OLLAMA_INTEGRATION = "1"
go test ./internal/console -run TestOllamaLiveDraftScenarios -count=1 -v
```

This check uses the actual model with a test Kubernetes client; browser draft validation against Kind is a separate check. The Foundry report dated 2026-10-04 preserves historical evidence and is not the current startup procedure.

Migration checks and manual draft review steps: [Ollama 接入验证报告](docs/Ollama接入验证报告.md).

In another terminal:

```powershell
npm --prefix web ci
npm --prefix web run dev
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` to the local API. This unauthenticated console is for a local operator only; do not expose it beyond the developer workstation. With GNU Make installed, `make api`, `make web`, `make web-build`, and `make stage2-check` are available.

### Environment class management

Chinese manual browser test steps, parameter examples, and the latest scoped local
acceptance results: [环境类别与模板网页手动测试及验收报告](docs/环境类别与模板网页手动测试及验收报告.md).

Open **Environment classes / 环境类别管理** in the sidebar, then **Create class**.
Choose a small/team Kind starter preset, a saved project template, or an existing
class before adjusting the form. Saved templates retain project connections,
runtime resources and policy settings; their source/backend/runner sections start
collapsed so you can focus on name, version, target and capacity. Starter presets
provide policy defaults and require real project references on the first use.
**Start from a blank form** remains available.

After validation, **Save as template** stores a reusable configuration snapshot in
a labeled `pcp-class-template-{name}` ConfigMap in the management cluster's
`default` namespace. The console's Kubernetes identity needs get/list/create/delete
ConfigMap permissions there. Saving a template does not create an EnvironmentClass
or environment. Templates survive API restarts and are available from other
browsers connected to the same console. Template names are unique; saving with an
existing name is rejected. Drafts receive independent copies, including optional
runtime settings, and class names are suggested without reusing an existing name.
Deleting a template requires its name and UID and retains existing classes and
environments. Inline credentials are rejected in saved templates as in classes.

The form creates a cluster-scoped `EnvironmentClass`; classes are stored in
Kubernetes. Fill in the
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
Template endpoints are `GET /api/class-templates`, `POST /api/class-templates`,
and `DELETE /api/class-templates/{name}`. Saved templates are snapshots; adjustments
to a draft affect the new class rather than the template.

The E2E harness needs Docker access to create one disposable management Kind
cluster and two target clusters, plus network reachability from Kind nodes to
the host LocalStack and fixture server. It never deletes external LocalStack or
the developer's Kind cluster. Run the suite serially: the existing Runner Git
init image alias is temporarily prepared and restored by the harness.

Automatic LocalStack startup defaults to the pinned Community image
`localstack/localstack:4.14.0`, with S3 and STS only. Existing healthy LocalStack
containers are reused regardless of edition. `-LocalStackImage` overrides the
startup image. Current `latest` images require authentication; historical
Community tags remain available ([official distribution notice](https://blog.localstack.cloud/the-road-ahead-for-localstack/)).
An exited container fails immediately and its logs are captured; the harness
does not obtain or copy license credentials.

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

### KPCP Local E2E Regression Suite

Run the permanent real local harness. The controller remains a two-replica
Kubernetes Deployment; Terraform runs in actual terraform-runner Jobs. The
platform API uses the existing local process architecture, built from current
source against the disposable management kubeconfig. No browser automation or
real AWS is used.

```powershell
# Windows equivalent when GNU make is not installed
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite all

# With GNU Make installed
make e2e
```

| Suite / Make target | Coverage |
| --- | --- |
| `all` / `make e2e` | Recovery lifecycle (including lifecycle), multi-target, fail-closed, platform and final cleanup |
| `lifecycle` / `make e2e-lifecycle` | Class Ready → environment → automatic InfraStack → Plan/evidence → wrong approval rejection → exact saved-plan Apply → Terraform output → runtime Ready → update/NoChange → independent Destroy/finalizer |
| `recovery` / `make e2e-recovery` | Lifecycle plus leader restart during an active Plan, after completed Plan and after Ready; stable discovery/inventory/approval, no duplicate Plan Job/Apply |
| `multitarget` / `make e2e-multitarget` | Separate target contexts/identities/execution accounts, correct target placement, no cross-target runtime writes, destroy of both environments |
| `failclosed` / `make e2e-failclosed` | Unregistered target, incomplete discovery and runtime mutation protection |
| `platform` / `make e2e-platform` | Real API health, class dry-run/create/list/detail, template ConfigMap create/read/delete, draft/explicit create, read model, sanitized PlanGraph, stale/exact approval API, saved-plan execution, runtime, stale/update/delete API contracts |
| `clean` / `make e2e-clean` | Cleanup of recorded inactive E2E runs; live runs and unrelated resources are skipped |

Individual suites use the same `-Suite <name>` interface. Polling defaults to 2s;
`-PollSeconds` and `-WaitTimeoutSeconds` configure execution waits. Each major
wait and lane reports elapsed time. Text/JSON summaries, source/image identity,
verified artifact evidence and focused failure diagnostics are stored under
`artifacts/e2e/<run-id>/` (ignored by Git).

Container builds default to `-BuildParallelism 2` for Go compiler concurrency.
Use `-BuildParallelism 1` on a workstation with limited available memory. The
harness derives disposable build files from the canonical Dockerfiles, sets the
build command's `GOMEMLIMIT=512MiB` and `go build -p`, and records
`build-profile.json`. These settings affect compiler processes only, and the
Go memory limit is a soft limit rather than a container memory cap. Disposable
Dockerfile-specific ignore files preserve `.dockerignore` and additionally omit
local `bin/` outputs from the build context. A unique run ID in the compiler step
forces `go build` to execute against current source on every run, with source
fingerprints and built/running image identities recorded. Go module/compiler
cache mounts are shared across serial runs; `--no-cache` is deliberately omitted
so those mounts remain usable. These compiler caches remain in Docker's build
cache and are not run resources. Product Dockerfiles and execution behavior are
unchanged. See [Docker's cache-mount documentation](https://docs.docker.com/build/cache/optimize/#use-cache-mounts)
and [Go's content-based build caching](https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching).

For Docker Desktop environments with intermittent Terraform registry DNS failures,
optionally pass `-TerraformDnsServer <resolver-IP>` (this workstation was checked
with `1.1.1.1`). The harness adds domain-specific CoreDNS forwarding for
`registry.terraform.io` and `releases.hashicorp.com` in its owned management
cluster. The original Corefile continues handling other domains. A real Pod
must resolve both dependencies, `host.docker.internal` and
`kubernetes.default.svc.cluster.local` before regression starts. The resolver,
original/patched Corefile and Pod evidence are recorded in
`terraform-dns-profile.json`. The default leaves DNS configuration unchanged.
This setting is removed with the disposable cluster. See
[CoreDNS forward configuration](https://coredns.io/plugins/forward/).

Temporary execution state is under `tmp/pcp-local-e2e-<run-id>`, rather than the
system Temp directory. `finally` cleans recorded processes, run-specific buckets,
Kind clusters, generated kubeconfigs/Secrets, fixture server and state. Artifact
summaries remain. `-KeepArtifacts` additionally retains temporary state for
investigation; that directory includes kubeconfigs and must stay untracked.
Use `-Suite clean` after that run has exited to remove its retained state.
`-Suite clean` uses exact run IDs and `ownership.json`, verifies absolute state
paths and skips live harness PIDs. It does not sweep clusters/buckets by a broad
prefix. Runs predating ownership records must be handled using their original
run evidence; they are not silently adopted by cleanup.

Stable deployment comes from existing `config/crd`, `config/rbac` and
`config/manager` manifests. There is currently no `config/overlays/local`; the
harness does not invent a Kustomize deployment. Run images, target contexts,
kubeconfig Secret/mount, execution enablement and LocalStack bucket/endpoint are
the dynamic patches. Capacity changes produce NoChange with the current fixture;
they do not prove infrastructure node scaling. Terraform's allowlisted
`target_discovery` output is verified in S3 and propagated into InfraStack trusted
discovery; there is no invented generic output field.

### Local validation — 2026-10-06

The following executions used real Kind controllers/Jobs and an external
LocalStack Ultimate instance. Every passing execution includes verified owned
cleanup. The final `all` run uses the current harness with the optional DNS
setting described above. Results are local backend/control-plane evidence.

| Suite | Result | Total, including bootstrap/cleanup | Run ID |
| --- | --- | --- | --- |
| lifecycle | PASS | 1297.4s / 21.6min | `20261006005812-7facad35` |
| platform | PASS | 674.5s / 11.2min | `20261006054055-1b5c0eb7` |
| recovery | PASS | 706.9s / 11.8min | `20261006055211-f0eb5b6e` |
| multitarget | PASS | 742.6s / 12.4min | `20261006060359-f0c1a85d` |
| failclosed | PASS | 276.7s / 4.6min | `20261006061623-dfaad889` |
| all | PASS | 1923.1s / 32.1min | `20261006084826-e454d7c8` |
| clean | PASS | 0.4s | Final check at 22:20 NZDT |

The standalone lifecycle ran before the final compiler-cache/DNS diagnostic
adjustments; the final `all` run repeated its assertions within recovery.
Final `all` lane times were bootstrap 244.0s, recovery/lifecycle 544.2s,
multitarget 623.1s, failclosed 7.2s and platform 485.0s. Dependency downloads
affect duration; these are measured workstation times, not CI guarantees.

Commands used for the passing executions:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite lifecycle -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1
foreach ($lane in @('platform', 'recovery', 'multitarget', 'failclosed')) {
    powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite $lane -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack
}
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite all -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack -TerraformDnsServer 1.1.1.1
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite clean

$env:GOMEMLIMIT = '512MiB'
go test -p 1 ./...
go build -p 1 ./cmd/controller ./cmd/terraform-runner ./cmd/platform-api
go vet -p 1 ./...
npm --prefix web run build
npm --prefix web run typecheck
npm --prefix web run lint
```

Final Go test/build/vet, web build/typecheck/lint, PowerShell syntax/command
resolution and `git diff --check` passed. Evidence is retained under
`artifacts/e2e/<run-id>/`; the validation inventory and service restoration
checks are under `tmp/local-e2e-modernization/validation-20261006/`.

BrowserSession startup, live API/Ready class, legacy metadata compatibility,
active-run protection in `clean` and timeout cleanup passed in
`20261006062101-9df79c0a`. Normal hold-file release is **NOT VERIFIED**: automatic
policy review rejected both deletion attempts with `blocked by policy`. That
combined `all -BrowserSession` command exited 1 after timeout; its backend lanes
all passed and its final cleanup passed. This is a **NON-PRODUCT BLOCKER**;
the completed standalone/final backend results above remain PASS. No browser
clicks or live Ollama inference are claimed by this harness.

Earlier attempts retain their original failure evidence: external LocalStack
disappearance, upstream provider download/DNS errors, and a corrected DNS-probe
helper-name error. The final passing run did not relax the affected assertions.
The existing Ultimate container identity was preserved, test buckets/state were
absent, and the developer controller/API/Vite services were restored with
Ollama configured after validation.

### Browser acceptance session

```powershell
# For the unchanged Vite proxy, first free API port 8090 yourself.
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite all -BrowserSession -ApiPort 8090
```

Without `-ApiPort`, API checks and session preparation use an available isolated
loopback port and leave an existing development API running. The existing Vite
proxy expects 8090; a browser session intended for that console should explicitly
use `-ApiPort 8090` after freeing that port. A busy requested port fails clearly;
the harness does not stop user processes.

After regression, `BROWSER_SESSION_READY` identifies `browser-session.json`, the
fresh Ready class, backend/artifact bucket, target kubeconfig, owned API URL/PID
and `browser-hold.flag`. The backend regression API uses the deterministic draft
provider so it does not depend on an LLM. Ollama browser acceptance remains a
separate test with its actual endpoint/model and the session kubeconfig/bucket.
The harness itself contains no browser automation.

Complete the manual lifecycle and independent Destroy approval, stop any manual
console processes you started, then remove **only the metadata's `holdFile`** to
release cleanup. The harness owns and stops its API/fixture processes. An
intermediate session summary is not a final PASS. The default session timeout is
3600s, configurable with `-BrowserSessionTimeoutSeconds`; timeout captures
diagnostics and still performs owned cleanup. `-Stage2Session` is a compatibility
alias, and legacy `stage2-session.json` remains available; the canonical names are
`-BrowserSession`, `browser-session.json` and `browser-hold.flag`.

## Typical development loop

1. Keep changes within the API, controller, runner, or target/runtime package they affect.
2. Run the Go test and vet checks before using an integration environment.
3. For lifecycle or runtime changes, run `make e2e`; reuse the external LocalStack service when available.
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
e2e/                         permanent KPCP local E2E regression harness
web/                         React + TypeScript operator console
```
