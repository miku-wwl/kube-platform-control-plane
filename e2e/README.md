# Stage 1 E2E evidence guide

This directory is the single reviewer entry point for Stage 1 validation. `Run-Stage1E2E.ps1` is the real non-interactive harness; it uses `kubectl` against live controller Pods, real Terraform Runner Jobs, two target Kind clusters, and an externally started LocalStack Ultimate. It does not start LocalStack, call real AWS, or add generated kubeconfigs/reports to the repository.

## Evidence lanes

| Lane | Primary evidence | Result boundary |
|---|---|---|
| Full lifecycle | Real Terraform CLI against LocalStack; runtime SSA update, capacity NoChange, and destroy | `E2E_FULL_LIFECYCLE = PASS_LOCAL` |
| Restart recovery | Leader Pod restarted during an active Plan Job and again after EnvironmentReady; job, Apply, discovery and inventory checked | `E2E_RESTART_RECOVERY = PASS_LOCAL` |
| Multi-target | Two Kind target contexts, separate runner identities, isolation, and both finalizers/destroys | `E2E_MULTI_TARGET = PASS_LOCAL` |
| Fail-closed | No Apply before approval; unregistered target and missing discovery produce no runtime side effect | `E2E_FAIL_CLOSED = PASS_LOCAL` |

## Run the reproducible real E2E

Start LocalStack Ultimate externally at `http://localhost:4566`, then run from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File e2e/Run-Stage1E2E.ps1 -Suite all
```

The equivalent Make targets are:

```text
make e2e              # all lanes
make e2e-lifecycle    # create -> plan -> approval -> saved-plan apply -> update -> destroy
make e2e-recovery     # lifecycle with a manager Pod restart during reconciliation
make e2e-multitarget  # two environments, two target clusters, isolation checks
make e2e-failclosed   # unapproved/unknown-target/incomplete-discovery checks
make e2e-clean        # removes only pcp-e2e-* Kind clusters
```

GNU `make` is optional on Windows; the PowerShell command is the same entrypoint. Set `PCP_LOCALSTACK_ENDPOINT` to another local endpoint if required. The harness rejects non-local endpoints.

The harness creates one unique management cluster and two unique target clusters, builds and loads the manager/runner images, serves a temporary Git fixture over a temporary local HTTP server, creates the required CRDs/RBAC and target kubeconfig Secret, and waits on side effects rather than only status fields. `LocalStack` stays externally managed.

Generated text evidence is written to `artifacts/e2e/<run-id>/summary.txt` plus logs and snapshots on failure. The directory is Git-ignored. A `finally` cleanup removes the run's temporary source tree, server, Terraform buckets, and Kind clusters; success is reported only after those resources are verified absent. Use `-KeepArtifacts` only when investigating a failed local run.

## Lower-level deterministic lanes

```powershell
go test ./...
go vet ./...
go test -race ./...
git diff --check
```

Run the live target-runtime lanes with an explicit context:

```powershell
$env:PCP_TARGET_CONTEXT = "kind-pcp-target-local"
go test ./internal/runtime -run 'TestKind' -count=1
go test ./internal/controller -run 'TestKindResourceSetControllerSSAInventoryAndPrune' -count=1
```

Run the LocalStack Terraform fixture:

```powershell
terraform fmt -check test/fixtures/terraform/localstack-basic
terraform -chdir=test/fixtures/terraform/localstack-basic init -backend=false -input=false
terraform -chdir=test/fixtures/terraform/localstack-basic validate
$env:PCP_LOCALSTACK_ENDPOINT = "http://localhost:4566"
$env:PCP_ARTIFACT_BUCKET = "platformlens-artifacts"
go test ./internal/artifacts -run TestLocalStackImmutableArtifactRoundTrip -count=1
```

## Full lifecycle stages observed by the harness

The live Stage 1 scenario must observe the following sequence:

```text
PlatformEnvironment
  -> InfraStack
  -> Terraform Plan / ChangesPresent
  -> digest-bound ChangeApproval
  -> exact saved-plan Apply
  -> LocalStack infrastructure ready
  -> trusted TargetDiscovery
  -> ResourceSet SSA/inventory/readiness
  -> EnvironmentReady
  -> leader restart during active Plan and again after Ready
  -> versioned class with the same source / runtime SSA update in place
  -> capacity update / new-generation NoChange without extra Apply
  -> delete / Destroy Plan / approval / exact saved-plan Destroy
  -> ResourceSet prune / finalizer removal / garbage collection
```

The observed live stages and their evidence on this host are in `artifacts/e2e/20260923220530-54550173/summary.txt` (Git-ignored). Broader readiness is recorded in [LOCAL-PRODUCTION-READINESS-REPORT.md](../LOCAL-PRODUCTION-READINESS-REPORT.md). Do not treat a unit test, rendered manifest, or LocalStack result as proof of real AWS behavior.

## Cleanup checks

After a live run, verify that no temporary Kind resources, namespaces, Terraform working directories, `.terraform` directories, plan files, kubeconfigs, credentials, or test buckets remain. The intentionally retained artifact bucket/state objects must match the current design and report.

## Deferred scope

Stage 2 advanced experience and Phase 13 Real AWS validation remain deferred. Phase 13 is the only place for live IAM/STS/EKS/KMS/VPC/networking, AWS quota/throttling, production operator/storage, DR/RPO/RTO, SLO, and cost evidence.
