# kube-platform-control-plane

KPCP is a Kubernetes-native cloud infrastructure control plane. Go controllers
own reconciliation, Terraform Job lifecycle, exact Plan approval, immutable
execution evidence, recovery, target isolation and finalization.

```text
PlatformEnvironment -> EnvironmentClass -> InfraStack -> TerraformRun: Plan
  -> exact ChangeApproval -> saved-plan Apply -> trusted TargetDiscovery
  -> ResourceSet reconciliation -> EnvironmentReady
```

## Phase 2.5 status

Phase 2.5B implements explicit emulator execution profiles, fixed modules,
saved-plan admission and durable recovery. Real MiniStack/Kubernetes lifecycle
tests run through Go. **PHASE_2_5B = PARTIAL:** two-cluster isolation and full
legacy API/browser acceptance remain open; see the current validation report.

- [Cleanup report](docs/phase2.5/CLEANUP_REPORT.md): actual changes and checks.
- [Architecture contract](docs/phase2.5/ARCHITECTURE_CONTRACT.md): authoritative responsibilities and safety requirements.
- [Acceptance matrix](docs/phase2.5/ACCEPTANCE_COVERAGE_MATRIX.md): retained unit tests, extracted legacy assertions and pending E2E ports.
- [Phase 2.5B handoff](docs/phase2.5/REFACTOR_HANDOFF.md): exact remaining files and dependencies.
- [Component architecture and API contracts](docs/architecture.md).
- [Implementation report](docs/phase2.5/IMPLEMENTATION_REPORT.md).
- [Current validation and limitations](docs/phase2.5/VALIDATION_REPORT.md).

## Deployment prerequisites

Supply a management Kubernetes environment, explicit target kubeconfig contexts,
CRDs/RBAC from `config/`, deployable OCI images, a pinned Terraform module source,
backend/artifact storage, workload credentials and network access. KPCP does not
create clusters, start a container engine/emulator, rewrite DNS or discover the
host OS. The stable manager manifest is a deployment template: replace its image
reference and supply environment-specific credentials, endpoints and egress rules.
Terraform execution and AWS EKS integration remain disabled unless explicitly enabled.

The target stack is Go/controller-runtime, standard Kubernetes APIs, fixed
versioned HCL modules and Terraform Jobs. MiniStack is the intended local AWS
emulator. S3/STS, conditional writes and the saved-plan lifecycle have live
evidence in the validation report; service-wide compatibility is not implied.
Podman is the preferred developer OCI build tool. The two Dockerfiles are standard
OCI image build inputs and impose no Docker CLI/runtime requirement.

| Component | Owns |
|---|---|
| Operator | Plans execution attempts, validates approvals/evidence, schedules Jobs, recovers and finalizes |
| Terraform module | Fixed resource declarations, typed provider inputs and allowlisted outputs |
| Cloud emulator | AWS-compatible APIs only; no control-plane lifecycle |
| Deployment tooling | Cluster, image, source, storage, credential and routing prerequisites |
| Developer commands | Build, install, check and invoke Go tests |

New supplied-cluster targets use `provider: kubernetes`. Existing `kind` identities
remain supported as serialized compatibility values. Neither value causes cluster
creation. Set `PCP_TARGETS_FILE` to a JSON array of `{identity, connection}` and
`PCP_KUBECONFIG` to the supplied kubeconfig. Identity uses `provider: kubernetes`,
`accountId`, `region`, `clusterName` and `incarnationId` (the actual kube-system
namespace UID). Connection uses `authMode: kubeconfig-context`, explicit
`kubeContext`, `endpoint: kubeconfig:<context>` and `networkRouteProfile: kubeconfig`.
Registration checks that UID against the API. Legacy `PCP_TARGET_CONTEXTS`
continues to register legacy serialized identities only.

Artifact configuration uses `PCP_ARTIFACT_ENDPOINT`, `PCP_ARTIFACT_REGION`,
`PCP_ARTIFACT_BUCKET` and optional `PCP_ARTIFACT_KMS_KEY_ID`. Explicit HTTP(S)
endpoints use path-style S3 and explicit dummy environment credentials
(`AWS_ACCESS_KEY_ID=000000000000`, `AWS_SECRET_ACCESS_KEY=test`, no session token).
Empty/AWS endpoints and ambient credential fallback are rejected. The Class
`executionProfile` supplies emulator endpoints, region, 12-digit account,
artifact bucket and namespace-local `credentialsSecretRef`; the Secret contains
those same dummy AWS keys. The Operator binds its UID/resourceVersion to the Plan.
Runner Jobs receive Secret references. Provider/backend endpoints must match
the profile; the controller may use a separately routed host alias.
Base egress allows DNS/443/6443; custom emulator/source ports need explicit site rules.

## Local console

Use Go matching `go.mod`, Node.js/npm for `web/`, and a supplied Kubernetes
configuration. The unauthenticated API binds to `127.0.0.1:8090`; Vite binds to
`127.0.0.1:5173` and proxies `/api`. Keep these local operator tools on loopback.

```text
go run ./cmd/platform-api
npm --prefix web ci
npm --prefix web run dev
```

Configure the API with the same artifact bucket as the manager and an explicitly
supplied loopback HTTP artifact transport. The existing artifact-preview loopback
restriction is retained. AI defaults are `AI_PROVIDER=ollama`,
`OLLAMA_ENDPOINT=http://127.0.0.1:11434`, `OLLAMA_MODEL=phi4-mini:latest`.
The API uses an already installed model; deterministic offline drafts are available
with `AI_PROVIDER=deterministic`. Model output is advisory typed intent: separate
human submission creates the environment. No inference failure silently falls
back, and there is no direct Apply endpoint.

The optional DeepSeek provider uses the official Chat Completions API. Configure
the backend process with `AI_PROVIDER=deepseek` and `DEEPSEEK_API_KEY`; the
default endpoint is `https://api.deepseek.com` and the default model is
`deepseek-flash` (also supported: `deepseek-v4-pro`). `DEEPSEEK_BASE_URL` is
restricted to the official HTTPS origin. Keep the key in the backend process
environment or an approved local secret manager; never put it in the frontend,
Kubernetes resources, source control, reports, or CI. Requests may incur DeepSeek
usage charges. Provider failures return a safe localized error and do not fall
back to deterministic generation or submit infrastructure.

The console retains environment details/timeline/plan topology, exact approvals,
class validation/create/delete and persisted template snapshots. Classes are
immutable; copy into a new named version. Templates are independent ConfigMaps
in management `default`; deletion requires exact identity and does not delete
existing classes/environments. EN/Chinese language selection is retained.

## Verification

Run from the repository root; no shell implementation is part of the runtime:

```text
go test ./... -count=1
go build ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
terraform fmt -check test/fixtures/terraform/aws-compatible-basic
terraform -chdir=test/fixtures/terraform/aws-compatible-basic init -backend=false -input=false -lockfile=readonly
terraform -chdir=test/fixtures/terraform/aws-compatible-basic validate
git diff --check
```

Terraform init/validate here are static tooling checks, not Plan/Apply acceptance.
The fixed fixture has no default endpoint, bucket name or cluster context.

Live tests are explicit opt-ins:

```text
KPCP_TEST_MINISTACK_ENDPOINT=<explicit endpoint> go test ./internal/artifacts -run TestMiniStackIntegration -v -count=1
KPCP_E2E_CONFIG=<absolute JSON path> go test ./e2e -run TestLifecycle -v -count=1 -timeout=20m
```

Set these environment variables using your shell's syntax. E2E JSON fields are
`Kubeconfig`, `Context`, `ControllerBinary`, `RunnerImage`, `HostEndpoint`,
`PodEndpoint`, `SourceURL`, `SourceRevision`, optional `SecondContext` and
`FaultFile`. Supply a dedicated management API,
installed CRDs, a built controller executable, Pod-accessible fixed-module Git
source pinned to its full commit, and an available runner image pinned by digest.
This lane starts/restarts only its dedicated controller process, owns unique
namespaces/classes/buckets, and preserves finalizers/evidence on failure.
`KPCP_E2E_EVIDENCE_DIR` optionally retains diagnostics outside version control.
The default one-cluster lane also checks real HTTP class/template/draft/approval/
update/delete APIs and concurrent environments with independent state, runner
identities and runtime namespaces. With `SecondContext`, the kubeconfig must contain both independent target contexts.
The suite checks distinct incarnations, cross-target runtime absence, independent
state, overlapping approvals/global Apply contention and ordinary cleanup on both
targets. Without it the cross-cluster gate skips; single-cluster concurrency
remains executable. Optional `FaultFile` must be a new absolute test-owned path;
an externally supplied test transport uses its bucket-name content to inject
temporary 503 responses. The suite checks retained finalizers and recovery,
then removes the switch. MiniStack itself stays externally managed.
Live advisory-model tests independently require `PCP_RUN_OLLAMA_INTEGRATION=1`.
The optional DeepSeek semantic suite requires `KPCP_DEEPSEEK_LIVE=1` and a
backend-only `DEEPSEEK_API_KEY`; it makes seven bounded serial requests to the
official DeepSeek API and is never run by default CI. It can incur API usage
charges. See [DeepSeek live validation](docs/phase2.5/qa/DEEPSEEK_LIVE_VALIDATION.md).

## Historical evidence

[Historical local validation](docs/phase2.5/LEGACY_VALIDATION_RECORD.md) retains
the dated 2026-10-06 run IDs/results formerly in this README. The unchanged
[Stage 2 validation report](docs/STAGE2_VALIDATION_REPORT.md),
[freeze evidence](docs/STAGE2_FREEZE_EVIDENCE.md),
[test design](docs/STAGE2_TEST_DESIGN.md), manual record and screenshots refer to
historical versions. Their old Kind/LocalStack/PowerShell commands are not active
instructions for this branch. The historical Stage 2 freeze had incomplete current
browser acceptance. Historical PASS results are not Phase 2.5 or real AWS proof.
