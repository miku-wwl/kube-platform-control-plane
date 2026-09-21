# Stage 1 E2E evidence guide

This directory is the single reviewer entry point for Stage 1 validation. It intentionally contains no temporary runner, JSON report, generated kubeconfig, or LocalStack bootstrap script. LocalStack is started and managed externally, in accordance with the project boundary.

## Evidence lanes

| Lane | Primary evidence | Result boundary |
|---|---|---|
| Full lifecycle | Live Kind management controller plus real Terraform CLI against LocalStack | `E2E_FULL_LIFECYCLE = PASS_LOCAL` |
| Restart recovery | Manager restart during active execution and reconstruction tests | `E2E_RESTART_RECOVERY = PASS_LOCAL` |
| Multi-target | Two Kind target contexts, target factory isolation, discovery and inventory tests | `E2E_MULTI_TARGET = PASS_LOCAL` |
| Fail-closed | Approval, stale evidence, wrong target/account, saved-plan and destroy-evidence tests | `E2E_FAIL_CLOSED = PASS_LOCAL` |
| AWS offline contract | Fake STS/EKS/S3/KMS adapters and identity separation tests | `E2E_AWS_OFFLINE_CONTRACT = PASS_LOCAL` |

## Run the reproducible local lanes

Start LocalStack Ultimate externally at `http://localhost:4566`, then ensure the management and two target Kind contexts exist. From the repository root:

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

## Full lifecycle stages

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
  -> safe update / NoChange
  -> manager restart recovery
  -> delete / Destroy Plan / approval / exact saved-plan Destroy
  -> ResourceSet prune / finalizer removal / garbage collection
```

The observed live stages and their evidence are recorded in [LOCAL-PRODUCTION-READINESS-REPORT.md](../LOCAL-PRODUCTION-READINESS-REPORT.md). Do not treat a unit test, rendered manifest, or LocalStack result as proof of real AWS behavior.

## Cleanup checks

After a live run, verify that no temporary Kind resources, namespaces, Terraform working directories, `.terraform` directories, plan files, kubeconfigs, credentials, or test buckets remain. The intentionally retained artifact bucket/state objects must match the current design and report.

## Deferred scope

Stage 2 advanced experience and Phase 13 Real AWS validation remain deferred. Phase 13 is the only place for live IAM/STS/EKS/KMS/VPC/networking, AWS quota/throttling, production operator/storage, DR/RPO/RTO, SLO, and cost evidence.
