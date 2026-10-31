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

Phase 2.5A removes legacy infrastructure orchestration and prepares the next
refactor. **Phase 2.5 migration is not complete.** The old PowerShell E2E harness
and its lifecycle Make targets are removed; Go integration parity is pending.
The Go successor must consume supplied clusters; its implementation is pending.

- [Cleanup report](docs/phase2.5/CLEANUP_REPORT.md): actual changes and checks.
- [Architecture contract](docs/phase2.5/ARCHITECTURE_CONTRACT.md): authoritative responsibilities and safety requirements.
- [Acceptance matrix](docs/phase2.5/ACCEPTANCE_COVERAGE_MATRIX.md): retained unit tests, extracted legacy assertions and pending E2E ports.
- [Phase 2.5B handoff](docs/phase2.5/REFACTOR_HANDOFF.md): exact remaining files and dependencies.
- [Component architecture and API contracts](docs/architecture.md).

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
emulator; its required S3/STS and backend semantics are **not yet verified**.
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
creation. `PCP_TARGET_CONTEXTS` (or singular `PCP_TARGET_CONTEXT`) selects explicit
contexts from `PCP_KUBECONFIG`; there is no current-context fallback for target
registration. The initial generic registration uses local account/region and
context-name incarnation; full explicit identity registration is Phase 2.5B work.

Artifact configuration uses `PCP_ARTIFACT_ENDPOINT`, `PCP_ARTIFACT_REGION`,
`PCP_ARTIFACT_BUCKET` and optional `PCP_ARTIFACT_KMS_KEY_ID`. Explicit HTTP(S)
endpoints use path-style S3; credentials come from the standard AWS SDK chain.
Endpoint names no longer inject dummy credentials. Provider endpoints, backend
HCL and artifact endpoints are separate settings. Local runner credential
provisioning and MiniStack deployment integration remain pending; see the contract.
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

The Go E2E successor is Phase 2.5B work and must use supplied clusters.
Live advisory-model tests independently require `PCP_RUN_OLLAMA_INTEGRATION=1`.

## Historical evidence

[Historical local validation](docs/phase2.5/LEGACY_VALIDATION_RECORD.md) retains
the dated 2026-10-06 run IDs/results formerly in this README. The unchanged
[Stage 2 validation report](docs/STAGE2_VALIDATION_REPORT.md),
[freeze evidence](docs/STAGE2_FREEZE_EVIDENCE.md),
[test design](docs/STAGE2_TEST_DESIGN.md), manual record and screenshots refer to
historical versions. Their old Kind/LocalStack/PowerShell commands are not active
instructions for this branch. The historical Stage 2 freeze had incomplete current
browser acceptance. Historical PASS results are not Phase 2.5 or real AWS proof.
