# Local Production Readiness Report

## Scope and safety boundary

Validated Stage 0 and Phases 7–12 only. Phase 13 was not implemented or executed.

The complete work cycle used only LocalStack Ultimate/Pro, Terraform CLI, Kind, unit tests, controller tests, and local operator artifacts. No real AWS account, AWS credentials, EKS, EC2, VPC, IAM, STS, KMS, or paid AWS infrastructure was used.

Local infrastructure used:

- LocalStack: `localstack/localstack-pro:dev`, endpoint `http://localhost:4566`, account `000000000000`.
- Kind management: `kind-pcp-management-local`.
- Kind targets: `kind-pcp-target-local` and `kind-pcp-target-b-local`.
- Terraform CLI: `1.14.0`.
- Pinned Halter operator source was tested locally at commit `fd5054747ebcaa052770ab7cbbd6f277639e35ec`.

## Final status

`PARTIAL`

The core Stage 0 and Phase 7–12 control-plane behavior that was runnable locally passed, including a real Terraform Plan/approval/Apply/Destroy lifecycle against LocalStack, update, manager restart recovery, two-target isolation, wrong-target rejection, quota rejection, API immutability, runtime pruning, and static checks.

The result is not `LOCAL_VALIDATION_PASS` because several acceptance lanes remain genuinely unavailable or blocked in the current local environment: envtest assets, the Kind network plugin's NetworkPolicy enforcement, the pinned Halter image/operator startup contract, the C race-test toolchain, and a full live 100/500/1000 fault/soak harness.

## Acceptance results

| Scope | Acceptance criterion | Result | Evidence / boundary |
|---|---|---|---|
| Stage 0 | Core regression, generated CRDs, lifecycle fences, approval binding, current-generation readiness | `PASS_LOCAL` | `go test ./...`, `go vet ./...`, `git diff --check`, generated CRDs, controller tests |
| Phase 7 | Runtime mutation is gated until current infrastructure is ready and the mutation fence is clear | `PASS_LOCAL` | Live ResourceSet stayed mutation-blocked while InfraStack was not ready |
| Phase 7 | SSA ownership, inventory, UID/target identity binding, and prune | `PASS_LOCAL` | Kind target integration and live two-target ResourceSets; both target objects were pruned on delete |
| Phase 7 | Wave ordering and readiness gating | `PASS_LOCAL` | Runtime bootstrap tests and live controller path passed |
| Phase 7 | Valkey bootstrap/readiness contract | `PASS_LOCAL` | Kind runtime tests and lifecycle runtime checks passed for the supported bootstrap path |
| Phase 7 | Real pinned Halter Valkey Operator reconciliation | `BLOCKED_LOCAL_ENVIRONMENT` | Pinned operator created CR/StatefulSet/PVC/Service, but the official Halter image required `VALKEY_PASSWORD` that the pinned operator did not inject; the substitute `valkey/valkey:8` did not load the operator-mounted cluster config. CR remained non-Available. |
| Phase 8 | Active Terraform Job reconstruction before manager admission | `PASS_LOCAL` | Manager restart logged reconstruction of one active execution with no duplicate Job |
| Phase 8 | Restart recovery, backpressure, duplicate Apply safety, and scale decision logic | `PASS_LOCAL` | Live restart converged; reliability tests covered 100/500/1000 in-memory cases |
| Phase 8 | Pod hardening and requests/limits | `PASS_LOCAL` | Manager Job/Pod security settings and resource limits were inspected live |
| Phase 8 | envtest acceptance | `BLOCKED_LOCAL_ENVIRONMENT` | `KUBEBUILDER_ASSETS` is unset and no `setup-envtest` executable/assets are installed |
| Phase 9 | Immutable Plan/Approval/Apply binding, source/input digests, runner identity, key-level secret injection | `PASS_LOCAL` | Terraform/controller/job tests and live saved-plan Apply passed |
| Phase 9 | LocalStack STS/S3 availability | `PASS_LOCAL` | Endpoint-scoped STS identity returned account `000000000000`; S3 artifact bucket was available |
| Phase 9 | Fresh real Terraform Plan/approval/Apply/Destroy against LocalStack | `PASS_LOCAL` | Repository fixture initialized with AWS provider `5.100.0`; real Terraform Plan produced `ChangesPresent`, saved-plan Apply succeeded, and Destroy removed the test bucket |
| Phase 10 | InfrastructureExecutionIdentity, RuntimeTargetIdentity, TargetConnectionProfile, discovery allowlist, factory isolation | `PASS_LOCAL` | Unit tests and live management-to-target client path passed |
| Phase 10 | Two-target isolation and wrong-target fail-closed behavior | `PASS_LOCAL` | A and B objects landed only in their matching Kind clusters; cross-reads were NotFound; unregistered target was `RuntimeTargetRejected` |
| Phase 10 | Real AWS STS/IAM/EKS authentication and account isolation | `DEFERRED_TO_PHASE13` | Real AWS is explicitly prohibited in this work cycle |
| Phase 11 | Typed Class Mode, legacy-mode validation, capacity/region policy | `PASS_LOCAL` | Live EnvironmentClass/PlatformEnvironment reconciliation reached expected states |
| Phase 11 | Runner ServiceAccount/Role/RoleBinding materialization | `PASS_LOCAL` | Live namespace-scoped runner identity and local `can-i` checks passed |
| Phase 11 | Developer/Approver separation | `PASS_LOCAL` | Tenant RBAC manifest and local authorization checks passed |
| Phase 11 | EnvironmentClass mutation protection | `PASS_LOCAL` | Live API rejected `spec.version` mutation with `EnvironmentClass spec is immutable`; original `v1/ClassValidated` state remained |
| Phase 11 | Tenant max-environment quota and class-level concurrency limit | `PASS_LOCAL` | Namespace annotation `max-platform-environments=1` caused both concurrently-created test environments to reach `QuotaRejected`; test objects were then removed |
| Phase 11 | Rejected-environment deletion without an InfraStack reference | `PASS_LOCAL` | Found and fixed nil `status.infraStackRef` dereference; regression test and live cleanup passed after manager restart |
| Phase 12 | Drift classification, fresh-plan rollback guard, cleanup evidence, retain/delete policy | `PASS_LOCAL` | `internal/day2` tests passed and lifecycle cleanup evidence was observed |
| Phase 12 | Failure-injection and fail-closed contract | `PASS_LOCAL` | Reliability/day2 fault contract tests, wrong-target rejection, approval gating, and restart recovery passed |
| Phase 12 | Live LocalStack/Kind fault injection, mixed live soak, and 100/500/1000 live environments | `BLOCKED_LOCAL_ENVIRONMENT` | In-memory 100/500/1000 tests passed, but no stable live high-scale/fault-injection harness was available for a credible local run |
| Phase 12 | Live default-deny NetworkPolicy enforcement | `BLOCKED_LOCAL_ENVIRONMENT` | Default-deny plus scoped health/DNS/API/LocalStack/Git egress policies were applied and manager stayed Ready, but Kind's default `kindnet` plugin does not provide evidence of actual deny/allow enforcement |
| Phase 12 | Full delete finalizer and Terraform destroy | `PASS_LOCAL` | ResourceSet pruned, destroy Plan reached `ChangesPresent`, approved saved-plan Destroy succeeded, all test CRs disappeared, and LocalStack test bucket returned 404 |

## Full lifecycle stages actually observed

1. `EnvironmentClass` reached `Ready=True / ClassValidated`.
2. `PlatformEnvironment` created `InfraStack`, Plan `TerraformRun`, `ResourceSet`, and runner identity.
3. Real Terraform Git source checkout and provider initialization completed in a Kind Job.
4. Terraform Plan reached `ChangesPresent / TerraformSucceeded` with a saved plan and digest.
5. Approval was bound to the Plan UID, digest, and execution context.
6. Saved-plan Apply reached `Succeeded / TerraformSucceeded`; LocalStack bucket readiness was verified.
7. `InfraStack` reached `InfrastructureReady`; `ResourceSet` reached its expected runtime state; top-level `PlatformEnvironment` reached `EnvironmentReady`.
8. Safe capacity update advanced the environment and stack generation; the new Plan reached `NoChange` without replacement.
9. Manager restart during an active execution reconstructed state and converged with one execution and no duplicate Job.
10. Delete pruned ResourceSet-managed objects, created and approved a destroy Plan, completed saved-plan Destroy, removed the LocalStack test bucket, and removed the environment finalizer.

## Required final checks

Passed:

- `go test ./...`
- `go vet ./...`
- `git diff --check`
- Terraform fixture `init -backend=false` and `validate`
- LocalStack endpoint-scoped STS and S3 checks
- Kind target SSA/inventory/prune and runtime tests

Not runnable:

- `go test -race ./internal/controller`: Go reported `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; the host has no usable C compiler/race-test toolchain.

## Remaining Phase 13 Real AWS requirements

- IAM/STS AssumeRole, wrong-account denial, credential expiry, and least-privilege verification.
- EKS DescribeCluster/authentication, target-account isolation, Kubernetes RBAC, and network reachability.
- Real VPC/EC2/EKS lifecycle, AWS quotas, throttling, and eventual-consistency behavior.
- S3 backend locking/versioning, KMS encryption, retention, public-access blocking, and recovery.
- Full AWS Plan/NoChange/Apply/failed-Apply/rollback/Destroy lifecycle.
- Production Halter profile, storage/PVC/PV/EBS/KMS retain/delete behavior.
- AWS 429/5xx, DNS/network partition, credential expiry, runner loss, leader loss, DR, RPO/RTO, SLI/SLO, and alert validation.

## R3 contract-completion pass — current workspace

The current uncommitted workspace includes the smallest scoped R3 contract closures without starting Phase 13:

- durable terminal-result and sanitized plan-report artifact capture; Pod logs are no longer authoritative terminal evidence;
- exact Job/Run identity checks, terminal mutation classification, verified `FinishedAt`-anchored Plan expiry, and immutable backend/source restoration for Apply/Destroy;
- durable deletion fencing, stale/superseded same-generation Plan attempts, NoChange convergence evidence, retained source/backend closure, cleanup evidence, and safer bounded names;
- target discovery on successful NoChange/Apply through `terraform output -json`, allowlist sanitization, immutable `target-discovery.json`, and fail-closed identity/profile verification;
- ResourceSet stable ownership/readiness/deletion policy handling, strict Valkey `Available=True` plus current-generation readiness, narrow per-namespace controller Secret RBAC, Prometheus execution/recovery/runtime metrics, and a local HA manager manifest;
- AWS SDK Go v2 artifact storage, Terraform machine-readable version verification, source-root/symlink/special-file/implicit-tfvars checks, and the fixture's target-discovery output contract.

Current evidence for this pass:

- `PASS_LOCAL`: `go test ./...`, `go vet ./...`, `git diff --check`, generated deepcopy/CRDs, Terraform fixture `fmt`, `init -backend=false`, and `validate`.
- `PASS_LOCAL`: LocalStack Ultimate endpoint-scoped STS/S3 checks and immutable artifact round-trip using dummy LocalStack credentials only.
- `PASS_LOCAL`: Kind SSA/inventory/prune/readiness integrations on `kind-pcp-target-local`.
- `PASS_LOCAL`: current R3 controller image built locally, loaded into `kind-pcp-management-local`, started with two replicas, metrics on `:8080`, reconstruction gate ready, and manager pod restart recovery converged.
- `PASS_LOCAL`: management ServiceAccount can read Secrets in `platform-system` and cannot read Secrets in `default`; the controller now materializes the corresponding narrow Role/RoleBinding in environment namespaces.
- `PASS_LOCAL`: a clean current-code Gate0 lifecycle rerun completed after the target-discovery contract was tightened. The run created `PlatformEnvironment` → `InfraStack` → Terraform Plan, reached `ChangesPresent`, accepted the approval bound to the effective plan input and execution-context digests, completed saved-plan Apply against LocalStack, reached `InfrastructureReady`, reconciled the Kind ResourceSet/runtime, and reached top-level `EnvironmentReady`.
- `PASS_LOCAL`: the same rerun covered a safe capacity update (`nodeCount` 1→2) with a successful `NoChange` Plan, manager deletion/recovery during an active execution, and a full delete path. ResourceSet-managed runtime objects were pruned, the approved saved-plan Destroy succeeded, the test infrastructure bucket was removed, and the environment/InfraStack/ResourceSet/Plan/Apply objects were garbage-collected.
- `PASS_LOCAL`: the final rerun left only the intentionally retained `platformlens-artifacts` bucket and the design-protected empty `pcp-gate0` Namespace. The temporary source container was removed; no test Service, StatefulSet, Pod, or test bucket remains.

Known R3 contract gaps remain: pinned Halter compatibility, envtest assets, the host C/race toolchain, genuine NetworkPolicy enforcement with default Kind networking, live 100/500/1000 fault/soak execution, and real AWS identity/EKS/KMS/network semantics. These remain `BLOCKED_LOCAL_ENVIRONMENT` where local infrastructure is insufficient and `DEFERRED_TO_PHASE13` for Real AWS requirements; the final status therefore remains `PARTIAL`.

## Repository state

- No real AWS was used.
- No commit was created and nothing was pushed to GitHub.
- No temporary validation directory, ad-hoc script, JSON report, or unrequested Markdown artifact was added inside the repository; the required report and Terraform fixture are the only requested validation artifacts.
- The working tree is intentionally **not Git-clean** because the scoped Stage 0/Phase 7–12 implementation, tests, configuration, fixture, and this report remain uncommitted.

## Phase 12 AWS Portability Closure — v2.0.4 additive gate

This section is the current Phase 12 portability verdict and supersedes only the older Phase 12 portability wording above. The historical R3 report remains unchanged for unrelated Halter/envtest/NetworkPolicy/high-scale limitations.

### Final status

```text
PHASE12_AWS_PORTABILITY_GATE = PASS
LOCAL_VALIDATION_PASS
```

This means all Phase 12 behavior that can reasonably be validated with unit tests, fake AWS SDK adapters, Kind, LocalStack Ultimate, and real Terraform CLI against LocalStack passed. It is not production validation on Real AWS.

### Acceptance classification

| Acceptance criterion | Classification | Evidence / boundary |
|---|---|---|
| AWS-native artifact store with empty endpoint and standard SDK resolution | `PASS_LOCAL` | S3 constructor/job tests; native mode emits region/bucket and no synthetic credentials |
| LocalStack artifact endpoint and immutable artifact contract | `PASS_LOCAL` | LocalStack Ultimate S3/STS checks and immutable artifact round-trip |
| Provider-aware Kind/AWS target materialization | `PASS_LOCAL` | Target materialization and binding tests; AWS never falls back to Kind |
| Trusted target discovery and ResourceSet propagation | `PASS_LOCAL` | Digest-bound discovery tests plus live env5 InfraStack/ResourceSet trusted status |
| Multi-target isolation and wrong-target fail-closed behavior | `PASS_LOCAL` | Kind target integration and target resolver tests |
| STS AssumeRole, EKS verifier, EKS token/client boundaries | `PASS_LOCAL` | Injectable fake SDK/presigner tests; no live AWS calls |
| Distinct infrastructure/runtime identities and runner role boundary | `PASS_LOCAL` | Controller helper-chain and identity/RBAC tests |
| Destroy retained source/backend/target-discovery closure | `PASS_LOCAL` | Live env5 saved-plan Destroy carried `--target-discovery-ref`/digest and finalizer cleanup completed |
| Destroy evidence fail-closed behavior | `PASS_LOCAL` | Regression test requires terminal, artifact, and retained discovery evidence before `InfrastructureRemoved` |
| Real AWS IAM/STS/EKS/KMS/network semantics | `DEFERRED_TO_PHASE13` | Real AWS prohibited in this work cycle |
| Live AWS quotas/throttling/cost/DR/RPO/RTO/SLO evidence | `DEFERRED_TO_PHASE13` | Requires production-like AWS services |

### Live lifecycle evidence

The clean local env5 run observed:

`PlatformEnvironment create → EnvironmentClass validation → InfraStack → Terraform Plan ChangesPresent → digest-bound ChangeApproval → saved-plan Apply SucceededPostMutation → LocalStack infrastructure ready → trusted target discovery → ResourceSet readiness → PlatformEnvironment EnvironmentReady → safe capacity update → Plan NoChange → manager restart recovery → delete finalizer → Destroy Plan ChangesPresent → digest-bound Destroy Approval → retained-evidence Destroy Apply → InfrastructureRemoved → ResourceSet/InfraStack/PlatformEnvironment cleanup`.

The live fixture intentionally had an empty RuntimeProfile, so its ResourceSet correctly reported `RuntimeEmpty`. Valkey bootstrap/readiness and SSA/inventory/prune behavior remain covered by the existing Kind runtime/controller integration tests and prior Gate0 evidence; they are not misreported as live env5 Valkey objects.

### Bugs found and smallest fixes

- AWS-native desired profiles incorrectly required a custom endpoint. The endpoint is now optional for AWS-native mode and strict for custom LocalStack mode.
- Apply/Destroy retained source paths were dropped. The source path now survives Apply and Destroy reconstruction.
- Destroy Apply attempted to regenerate target discovery from post-destroy Terraform output. It now verifies and retains the previously trusted discovery artifact; the controller also blocks finalizer removal until Destroy evidence is complete.
- Kind LocalStack output region (`us-east-1`) was incorrectly compared as the Kind runtime region. Kind keeps its local runtime identity while AWS remains strict.
- Apply workspace creation was made explicit for clean saved-plan Pods; no re-plan is performed.

### Regression status

| Check | Status |
|---|---|
| `go test ./...` | `PASS` |
| `go vet ./...` | `PASS` |
| `go test -race ./...` | `PASS` (local run completed) |
| `git diff --check` | `PASS` |
| Terraform fixture fmt/init/validate | `PASS` |
| LocalStack Ultimate S3/STS/Terraform | `PASS` |
| Kind target/runtime/SSA/inventory/prune | `PASS` |
| AWS-native mocked/offline adapters | `PASS` |
| Real AWS validation | `NOT RUN` by explicit prohibition |

### Phase 13 boundary

Expected Phase 13 code diff is configuration/wiring and live-verification setup only: AWS-native endpoint profiles, IAM/STS role policy, EKS cluster/account configuration, KMS/S3 backend settings, network controls, and production test fixtures. No new core controller architecture or CRD is expected from the Phase 12 closure. Phase 13 was not started.

Remaining Phase 13 evidence must cover real IAM/STS/EKS/KMS/VPC/EC2/S3 behavior, quotas/throttling/eventual consistency, production Halter storage, AWS failure injection, DR/RPO/RTO, SLI/SLO/alerts, and cost controls.

### Delivery boundary

- No real AWS was used; only LocalStack Ultimate, Kind, fake/injectable AWS adapters, and local Terraform were used.
- The repository was not committed or pushed, as requested.
- The working tree is intentionally not clean because the necessary Phase 12 implementation, tests, generated CRDs, and required documentation remain uncommitted.
- No temporary validation directory, ad-hoc script, JSON report, or unrequested Markdown artifact was added.
- Phase 13 was not started.

## Phase 12 Final AWS Identity & KMS Closure — v2.0.4 additive portability gate

This is a narrow closure of the remaining AWS portability boundaries. It does not change the PlatformEnvironment lifecycle, approval model, saved-plan execution, TargetDiscovery contract, ResourceSet lifecycle, finalizer model, CRD major semantics, or restart-recovery design.

### Final status

```text
PHASE12_FINAL_FREEZE = PASS
LOCAL_VALIDATION_PASS = PASS
```

`PASS` is limited to behavior that can be proven locally with unit tests, fake AWS SDK clients, LocalStack Ultimate, Kind, and the existing Gate0 evidence. Real AWS remains prohibited and deferred to Phase 13.

### Closed gaps

| Gap | Classification | Evidence / smallest fix |
|---|---|---|
| RuntimeRoleARN enters the EKS credential path | `PASS_LOCAL` | `TargetConnectionProfile.RoleARN` is passed to the injectable `AWSSTSTokenProvider`; configured roles call the injectable AssumeRole boundary, install temporary credentials into an AWS SDK config, and use that config for EKS token presigning. Empty role uses the base workload identity. |
| Runtime target/account/region/role isolation | `PASS_LOCAL` | Runtime token tests cover AssumeRole denial, wrong account, wrong region, expired credentials, invalid role identity, and two target calls without cross-target credential caching. |
| InfrastructureExecutionIdentity reaches Terraform execution | `PASS_LOCAL` | Superseded by the final identity-separation closure below: `ExecutionRoleARN` is propagated as a non-secret Terraform provider `assume_role` input; the runner ServiceAccount remains the management/base identity and is never annotated with the target execution role. |
| Artifact encryption portability | `PASS_LOCAL` | S3 store defaults to `AES256`; optional `PCP_ARTIFACT_KMS_KEY_ID` / runner `--artifact-kms-key-id` emits `aws:kms` and the configured `SSEKMSKeyId`. LocalStack default behavior is unchanged. Fake S3 request tests cover both modes. |
| IAM role path handling | `PASS_LOCAL` | Strict ARN parsing now extracts the final role name from simple, one-level, and nested IAM paths and rejects malformed input. |
| Real AWS IAM/STS/EKS/KMS/network semantics | `DEFERRED_TO_PHASE13` | No real AWS calls were made or permitted. |

### Changed files in this closure

- `internal/target/eks_resolver.go`: RuntimeRoleARN-aware, injectable AssumeRole and AWS credential wiring for EKS token generation.
- `internal/target/identity.go`: Temporary assumed-session credential fields for the provider boundary.
- `internal/target/aws_adapters.go`: Strict ARN parsing, role-path handling, and complete temporary-credential validation.
- `internal/target/aws_adapters_test.go`: Runtime-role success/failure, target isolation, refresh/no-cache, and ARN-path tests.
- `internal/artifacts/s3.go`: Optional SSE-KMS request configuration while retaining AES256 default behavior.
- `internal/artifacts/s3_test.go`: Fake S3 assertions for AES256 and SSE-KMS headers.
- `internal/controller/infrastack_controller.go`, `internal/controller/terraformrun_controller.go`, `cmd/controller/main.go`: KMS configuration propagation for artifact reads and execution reconciliation.
- `internal/terraform/job.go`, `cmd/terraform-runner/main.go`: KMS configuration propagation to Terraform runner Jobs without synthetic AWS credentials in AWS-native mode.
- `internal/controller/phase12_aws_portability_test.go`: Full infrastructure identity → ServiceAccount → TerraformRun → Job chain assertion and KMS argument assertion.

### Regression status

| Check | Status | Evidence |
|---|---|---|
| `go test ./...` | `PASS` | Full repository test suite |
| `go vet ./...` | `PASS` | Full repository vet run |
| `go test -race ./...` | `PASS` | Full repository race run |
| `git diff --check` | `PASS` | Only normal LF/CRLF conversion warnings |
| Terraform fixture fmt/init/validate | `PASS` | LocalStack fixture; generated `.terraform` directory removed afterward |
| LocalStack Ultimate S3/STS/artifact regression | `PASS` | Healthy local `localstack/localstack-pro:dev`, synthetic local identity only, immutable artifact round-trip |
| Kind manager/runtime health | `PASS` | Two management manager Pods Ready; both target Kind nodes Ready; no residual test CRs |
| Gate0 lifecycle | `PASS_LOCAL` | Existing clean current-code evidence remains valid; this closure changes no Kind lifecycle path and all lifecycle/controller regressions pass |
| Real AWS validation | `NOT RUN` | Explicitly prohibited; Phase 13 only |

### Phase 13 boundary

Expected core code changes: `NONE`.

Phase 13 remaining work is limited to real AWS deployment/configuration and live evidence: IAM/STS policy and AssumeRole validation, S3/KMS and backend semantics, EKS authentication and Kubernetes access, VPC/EC2/networking, quotas/throttling/eventual consistency, production Halter storage, failure injection, DR/RPO/RTO, SLI/SLO/alerts, and cost evidence. No controller redesign, CRD redesign, identity architecture redesign, TargetDiscovery redesign, or Terraform lifecycle redesign is expected from this closure.

### Delivery boundary

- No real AWS, real credentials, real IAM/STS/EKS/S3/KMS, or paid cloud resource was used.
- No commit or push was performed.
- No temporary validation directory, ad-hoc script, JSON report, or extra Markdown file was added.
- Phase 13 and Stage 2 were not started.

## Final Management vs Execution Identity Separation — v2.0.4 additive closure

This closure addresses the remaining identity-collapse risk without changing the lifecycle, approval, saved-plan, evidence, TargetDiscovery, ResourceSet, finalizer, or restart-recovery architecture.

### Final freeze status

```text
PHASE12_FINAL_FREEZE = PASS
STAGE1_FREEZE = PASS
LOCAL_VALIDATION_PASS = PASS
```

The freeze is local/offline evidence only. Real AWS identity semantics remain Phase 13 work.

### Closed gap

- Management Identity is the Runner Pod base workload identity. It is represented by the management ServiceAccount selected through `BackendSpec.AuthRef`/`RunnerProfileSpec.ServiceAccountName`; an optional `RunnerProfileSpec.ManagementRoleARN` may annotate that ServiceAccount. It is not the target execution role.
- Infrastructure Execution Identity is `InfrastructureExecutionIdentity.RoleARN` / `ExecutionRoleARN`. It is propagated as an immutable non-secret ARN through Plan, Apply, and Destroy and is consumed only by Terraform's temporary provider override:

  ```text
  Runner base identity
      ├─ Artifact S3/KMS
      ├─ Terraform backend S3/lock
      └─ Terraform AWS provider assume_role(ExecutionRoleARN)
             └─ target infrastructure mutation
  ```

- Runtime Target Identity remains the independent `RuntimeRoleARN → STS AssumeRole → EKS token → Kubernetes client` chain from the prior closure.
- AWS execution-role account binding is fail-closed at target materialization: a role ARN from a different account, or a malformed role ARN, is rejected before infrastructure execution can be constructed.
- `BackendSpec.AuthRef` is now consumed: when present it must match the runner profile ServiceAccount and becomes the management/base ServiceAccount reference. It is not an unused fourth AWS identity.
- Temporary AWS credentials are not written to CR spec/status, ConfigMap, artifacts, logs, or reports. The Terraform override contains only the role ARN and is deleted after Plan/Apply; source bundling excludes the override filename.

### Actual credential chains

| Operation | Identity used | Local evidence |
|---|---|---|
| Artifact S3/KMS | Runner base/management AWS SDK credential chain; LocalStack uses synthetic `test/test` only for local endpoint | Job/store tests and LocalStack round-trip |
| Terraform backend init/lock | Runner base/management credential chain; the provider override contains no backend block | Executor override contract and Terraform command tests |
| Terraform Plan | Base identity for backend/artifacts; target AWS provider uses `assume_role(ExecutionRoleARN)` when configured, otherwise base identity by explicit empty-role behavior | Job argument, PlanRun role propagation, ephemeral override tests |
| Terraform Apply | Same immutable execution role ARN and identity digest as Plan; saved plan remains the mutation input | Apply binding and Plan→Apply tests |
| Terraform Destroy | Same execution role ARN and identity digest carried through retained Destroy Plan/Apply | Destroy propagation tests and existing Gate0 evidence |
| EKS Runtime | Runtime base identity or `AssumeRole(RuntimeRoleARN)` for EKS token generation, independent of Terraform execution role | Fake STS/EKS token tests from prior closure |

### Changed files

- `api/v1alpha1/types.go`: management role, runner identity digest, and non-secret Terraform execution role propagation fields.
- `config/crd/bases/platform.example.io_environmentclasses.yaml`, `platform.example.io_infrastacks.yaml`, `platform.example.io_terraformruns.yaml`: corresponding CRD schema fields.
- `internal/controller/environmentclass.go`: consumes `BackendSpec.AuthRef`, materializes management identity fields, and binds identity digests.
- `internal/controller/platformenvironment_controller.go`: removes direct ExecutionRoleARN SA annotation; manages only optional ManagementRoleARN and clears stale execution-role annotations.
- `internal/controller/infrastack_controller.go`: preserves and validates the same execution role and management identity across Plan/Apply/Destroy.
- `internal/controller/terraformrun_controller.go`, `internal/terraform/job.go`, `cmd/terraform-runner/main.go`: propagate the non-secret execution role ARN to the runner.
- `internal/terraform/executor.go`: creates and removes the credential-free Terraform AWS provider `assume_role` override.
- `internal/terraform/executor_test.go`, `internal/controller/phase12_aws_portability_test.go`, `internal/controller/platformenvironment_identity_test.go`: identity separation, propagation, ephemeral override, and no-collapse tests.
- `internal/target/identity.go`, `internal/target/target_test.go`: fail-closed AWS execution-role ARN/account validation and wrong-account regression test.

### Required regression

| Check | Status |
|---|---|
| `gofmt` | `PASS` |
| `go test ./...` | `PASS` |
| `go vet ./...` | `PASS` |
| `go test -race ./...` | `PASS` |
| `git diff --check` | `PASS` |
| Terraform fmt/init/validate | `PASS` |
| LocalStack Ultimate artifact/STS regression | `PASS` |
| Kind manager and target health | `PASS` |
| Gate0 lifecycle baseline | `PASS_LOCAL` — preserved; no lifecycle architecture path changed |
| Wrong-account execution role fail-closed check | `PASS_LOCAL` |
| Real AWS calls | `NOT RUN` — explicitly prohibited |

### Expected Phase 13 core code changes

```text
NONE
```

Phase 13 is limited to live AWS configuration and evidence: IAM/STS role policies, IRSA or EKS Pod Identity, S3/KMS, EKS, VPC/EC2, networking, throttling/eventual consistency, failure injection, DR/RPO/RTO, SLI/SLO, and cost validation. No identity architecture, Runner ServiceAccount semantics, controller, CRD, Terraform lifecycle, or artifact/backend redesign is expected.

### Delivery boundary

- No real AWS credentials or services were used; only LocalStack Ultimate, Kind, fake/mocked contracts, and local Terraform were used.
- No commit or push was performed.
- Stage 2 and Phase 13 were not started.

## Stage 1 Repository Cleanup & E2E Consolidation

```text
REPOSITORY_CLEANUP = PASS
STAGE1_E2E = PASS
STAGE1_FREEZE = PASS
```

### Audit decisions

| Scope | Decision | Reason |
|---|---|---|
| `scripts/`, `hack/`, `*.sh`, `*.ps1`, `*.py` | `KEEP` / no deletion | No such files are present in the current repository; no temporary script is being reintroduced. |
| `test/fixtures/terraform/localstack-basic` | `KEEP` | The single fixture is used by Terraform fmt/init/validate and LocalStack regression evidence. |
| `*_test.go` | `KEEP` | The current tests cover lifecycle fences, identity/integrity, AWS fake failures, reconstruction, runtime readiness, SSA/inventory/prune, and backpressure; no safe duplicate deletion was identified. |
| v2.0.3 brief/detailed design and gap analysis | `KEEP` | These are the frozen architecture and historical decision record, not temporary reports. |
| `LOCAL-PRODUCTION-READINESS-REPORT.md` | `KEEP` | This is the authoritative local evidence and limitation record. |
| temporary validation directories, JSON reports, generated kubeconfigs, stale plan files | `DELETE` / none present | No such repository files remain; generated runtime artifacts stay outside the repository and are cleaned after execution. |

### E2E consolidation

The reviewer entry point is now [README.md](README.md) → [e2e/README.md](e2e/README.md). The E2E guide groups the existing evidence into five high-value lanes: full lifecycle, restart recovery, multi-target isolation, fail-closed behavior, and AWS-native offline contracts. It does not add a second LocalStack launcher or a synthetic E2E that would misrepresent unit evidence as live lifecycle proof.

### Retained unit-test categories

- identity, target/account/region, ARN and fail-closed validation;
- digest, saved-plan, source-closure, evidence and reconstruction integrity;
- fake STS/EKS/S3/KMS failure paths;
- controller lifecycle gates, approval binding, cleanup evidence and backpressure;
- runtime readiness, Valkey bootstrap shape, SSA/inventory/prune and Kind integration.

### Final repository structure

```text
README.md
e2e/README.md
LOCAL-PRODUCTION-READINESS-REPORT.md
platform-control-plane-*-v2.0.3-final-freeze-locked-r3.md
api/ cmd/ config/ internal/ test/fixtures/terraform/
```

### Cleanup validation

The current local evidence remains `PASS_LOCAL`: full lifecycle stages, restart recovery, multi-target isolation, fail-closed checks, and offline AWS identity contracts are recorded above; `go test`, `go vet`, race, Terraform, LocalStack, Kind, and `git diff --check` remain required gates. The cleanup removed eight orphaned Phase 12 `ChangeApproval` test objects from the local management Kind cluster; no PlatformEnvironment, InfraStack, TerraformRun, ResourceSet, test namespace, or test bucket remained. LocalStack retained only the design-approved `platformlens-artifacts` bucket. No real AWS was used, and no commit or push is part of this cleanup cycle.
