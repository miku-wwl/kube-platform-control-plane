# Phase 2.5B validation

2026-11-01, Pacific/Auckland. `PHASE_2_5B = PARTIAL`.

The latest single-cluster HTTP API/concurrency lane passed in 599.78 seconds;
the separate scoped transient-fault lane passed in 204.25 seconds. Raw logs,
configuration and OCI archives
are ignored under `tmp/phase25b`; historical evidence is not current proof.

| Check | Current evidence |
|---|---|
| `go test ./... -count=1` | PASS; opted-in tests skip without explicit configuration |
| `go test -race ./...`; `go vet ./...` | PASS; final target/controller and expanded harness race checks also PASS |
| Controller, Runner, Platform API Go build | PASS via `go build ./...`; Linux controller also built |
| `npm --prefix web run build` | PASS, TypeScript and Vite; no separate frontend unit command exists |
| Terraform fmt/init/validate | PASS with Terraform 1.14.0 OCI runner and `-lockfile=readonly`; provider 5.100.0 lock retained |
| CRDs/RBAC/manifests | API Server CRD dry-run PASS; supplied-cluster install PASS; all RBAC/manager resources strict client dry-run PASS. Manager ServiceAccount runtime permissions are not claimed |
| MiniStack contract | PASS: STS GetCallerIdentity, S3 Put/Get/Head/List/Delete, conditional conflicting writes, same-content retry, corruption/missing detection, lock contention |
| Real lifecycle | PASS for implemented A/B/C/E/F scenarios, NoChange, live HTTP APIs and single-cluster concurrency; cross-cluster D remains NOT RUN. This does not close the full six-group completion gate |
| Production AWS | NOT USED; explicit dummy account `000000000000`, secret `test`, session token empty |
| Browser/visual/AI quality | NOT RUN; attempted IAB creation returned `The browser access host is unavailable`. Live HTTP API tests and frontend build are separate evidence |

The user-selected current context is `podman-minikube`; direct `kubectl get nodes`
confirms Ready (Kubernetes 1.37.0). All continuation work uses that context.
Management and target share one actual cluster. Separate namespaces, state and
runner identities are tested without counting them as independent clusters;
cross-cluster D remains NOT RUN under the supplied single-cluster scope.

Two defects were found by real tests and corrected without weakening assertions:
the retained status snapshot inherited TerraformRun spec immutability CEL, and
runtime readiness ignored the existing ApplyOnly policy. The failed expanded run
was stopped through ordinary Environment deletion and a separate exact Destroy
approval. Cleanup verified cloud absence and removed namespace
`kpcp-1793498257020907700` with original UID
`e219f489-072e-4bca-b1c4-bf8515268c9b`; no finalizers were forced off. Its FAIL log
is retained alongside the cleanup receipt. A rejected background-helper launch
was replaced by a foreground temporary controller, subsequently stopped.

Final executable scenarios: five mismatched approvals cause no Apply; corrupt
Plan bytes (with forged matching metadata) and missing source each reject Apply
before Job creation; four dedicated Operator process restarts retain the Plan UID
and exactly one Apply; NoChange converges without a second Apply; nonexistent
Git revision and unreachable Pod endpoint produce failed Jobs without Apply;
owned runtime is pruned before Destroy approval, the finalizer holds, separately
approved Terraform Destroy removes the actual bucket, and an unrelated ConfigMap
survives. Ordinary Class/namespace/bucket cleanup succeeds. These are concrete
scenario results, not exhaustive legacy parity or multi-replica failover proof.

The optional two-target lane is implemented but NOT RUN: it needs `SecondContext`
in a supplied kubeconfig containing two independent clusters. It checks live
incarnations, negative cross-target reads, separate backend state keys, overlapping
approvals with observed global Apply contention, and cleanup on both targets.
Full live source/target/account/discovery/fence negatives, Deployment leader
failover, KMS/versioning/AssumeRole scenarios and browser acceptance remain open.
Core live HTTP API workflows and scoped transient-error recovery now pass.
Lock contention is verified at the S3 API
contract; simultaneous Terraform backend clients were not exercised here.

MiniStack reports version 1.5.35; existing image source revision is
`ae9285c9b50ef2232a9d2c2c92e9a5efa93cead2`. Host route is
`http://127.0.0.1:4566`; test-only Pod transport is explicitly
`http://169.254.1.2:14566`. Source-clone Pods reached the supplied HTTP Git source;
Runner Pods wrote/read artifacts and backend state through that route. These
addresses are site configuration, not business-code defaults. No emulator reset
or existing-container teardown occurred. [MiniStack upstream](https://github.com/ministackorg/ministack)
documents the emulator; compatibility claims here come from current execution.

Fixed module fixture commit: `bb6235ebdc67da635108f58d8157ef13241ccb46` in an
ignored, isolated Git fixture containing the four fixed module files. No remote
push was used. Initial imported runner OCI manifest:
`sha256:167deb80fbad76a4d20e80cc1d647c57e4b3591c996817ff47ef263facf196f6`.
First Plan UID `da2e331f-5fcf-4ccc-9de1-cc084cbf326f`, Plan digest
`sha256:b67ec751a710b6dfeeb5a03e4cf1dbcbc09cfd5a3ccc4fbda65f2f2da7e0d2c8`.
Its namespace `kpcp-1793497213470800800` was removed through ordinary lifecycle
cleanup. Final run:

- Namespace `kpcp-1793498935465163800`, UID `9a4cc8d0-2afc-41ee-be61-2f452dce6a92`.
- Runner imported OCI manifest `sha256:0878fa0d4bdc4d01aaab3a4732ec54df3e5f2dbf4632fc64efec6d48d245d8f6`.
- Controller built OCI manifest `sha256:ba641664b6f2de75e9d86c27458ea6b26f85eb85c0f0f8e79e6bb10420e8e968`; E2E ran the dedicated host controller process, not a Deployment of this image.
- Plan UID `b44bd14b-59ad-4c94-a8c8-2505060b2fe6`.
- Plan SHA-256 `076807cd4f18e382952be08ef040ffb1e29e0378965ba293a19acc5ffd60a312`.
- Source bundle SHA-256 `10a402e0c905bb760137a662bdb20ee89206135d0f5334c5b0d9e49bb378b06a`.
- Effective-input SHA-256 `c969a38eba0aad2096b63e58236ba6346903ae4b5571a14fa72dbd749deaf6d3`.
- Tracked provider lockfile SHA-256 `c9fdf3e4c695e49bf1176df234c6b46400457aa29fe45532d51ab6c97c39acc0`.
- Raw receipts: `tmp/phase25b/e2e-verified.log`, `unit-final.log`, `race-verified.log`, `ministack-final.log`, and the run-prefixed controller log.

## Continuation using only the configured podman-minikube context

The API lane found and fixed an actual approval-availability defect: a differently
named invalid approval for the same Plan blocked a later valid exact request.
The API now preserves canonical-name conflicts, ignores unrelated invalid records
and keeps exact retries idempotent. A regression test checks both names and
unchanged immutable records. The live rerun retains the invalid approvals and
succeeds through the exact API submission. Template DELETE assertions use the
existing HTTP 200 contract; no product status was changed. Failed setup fixtures
were normally finalized after verifying that no Apply had been admitted.

`e2e-api-concurrency-verified.log` records the full 599.78-second PASS: real HTTP
class dry-run/create/duplicate/in-use protection; persisted template listing,
duplicate/UID deletion; advisory draft without execution; explicit submission;
graph UID/digest; stale/exact approvals; read models; stale/current capacity
updates and NoChange; UID-bound deletion and separately approved Destroy. Two
additional environments become Ready with distinct runner identities and durable
workspace keys. Overlapping approvals produce observed active/waiting contention
with at most one active Apply Job. Runtime cross-namespace absence and ordinary
cleanup also pass. This is single-cluster evidence, not cross-cluster proof.

- Namespace `kpcp-1793514494174065900`, UID `828b1ab9-5b2b-4b4c-914e-662f358a3271`.
- Secondary namespace UID `5b0856e2-3ebb-4dd5-90ae-3ae16207eb3a`; both use cluster UID `2c62d6a8-08b6-460d-bb1b-ac59557dc281`.
- Plan UID `b7c5325b-44fc-403d-8b29-c4b720f52814`, digest `sha256:15598d788f9a3ee75ce5824567cee32d8a78f3d7d432ee71f687d69572c0fe84`.
- Apply Job UID `8c7b8d42-0b45-48fb-a632-cc16dabed4b8`; actual Pod imageID is `localhost/kpcp-runner@sha256:0878fa0d4bdc4d01aaab3a4732ec54df3e5f2dbf4632fc64efec6d48d245d8f6`.
- Job/Pod/run snapshots are in `tmp/phase25b/api-concurrency-{jobs,pods,runs}.json`.

`e2e-transient-fault.log` records a separate 204.25-second PASS. A test-owned
transport returns scoped HTTP 503 for the owned artifact bucket while MiniStack
stays healthy. Destroy Plan Job `0625273a-48d2-4330-8400-b35e0146ed21` genuinely
fails. The environment finalizer remains, the cloud bucket remains, and no Destroy
Apply is admitted. Restoring transport yields a fresh immutable Destroy Plan;
exact API approval then performs real destruction and normal cleanup.
Namespace `kpcp-1793515270992021500` has UID
`c1994b62-dd3f-4dee-8f21-0ad669d28318`. Transport receipts are in
`tmp/phase25b/transport-fault.log`; the fault switch is removed.

The current Pod/source test routes use site address `192.168.1.19` on ports 14566
and 18085; this replaces the earlier temporary route after the host network
changed. Source and bucket access are explicitly probed before execution. These
addresses remain external fixture configuration. Additional invocation:

```text
go test ./e2e -run 'TestLifecycle/(PlatformAPI|F_DestroyFinalizer)' -v -count=1 -timeout=15m
```

That command uses `KPCP_E2E_CONFIG=tmp/phase25b/e2e-fault.json`, whose absolute
`FaultFile` is consumed by the test-owned transport. The selected subtests do not
claim to rerun the omitted groups. Current unit/race/vet/build checks and frontend
build pass; their receipts include `unit-single-context-final.log` and
`race-api-concurrency.log`. No additional test file or branch was created.

Executed command families (PowerShell environment assignment uses `$env:NAME`):

```text
controller-gen object crd paths=./api/v1alpha1 output:crd:artifacts:config=./config/crd/bases
kubectl --context podman-minikube apply --server-side --dry-run=server -f config/crd/bases
kubectl --context podman-minikube apply --server-side -f config/crd/bases
go test ./... -count=1
go test -race ./...
go vet ./...
go build ./...
go build -o tmp/phase25b/controller-final.exe ./cmd/controller
npm --prefix web run build
go test ./internal/artifacts -run TestMiniStackIntegration -v -count=1
go test ./e2e -run TestLifecycle -v -count=1 -timeout=20m
git diff --check
```

OCI commands used the installed WSL remote client:
`wsl.exe -d podman-machine-default --exec /usr/bin/podman --remote --url unix:///run/user/1000/podman/podman.sock`.
Arguments: `build --format oci -f runner.Dockerfile -t localhost/kpcp-runner:phase25b .`,
`build --format oci -f Dockerfile -t localhost/kpcp-controller:phase25b .`,
and `run --rm --entrypoint terraform -v <fixture>:/module:rw -w /module <runner> <command>`
for `fmt`, `init -backend=false -input=false -lockfile=readonly`, `validate`.
OCI archives were loaded into the supplied node with its external containerd
tooling, preserving their actual imported manifest digest. Engine commands are
developer preparation; the Go suite neither provisions clusters nor invokes them.

| Required flag | Value and basis |
|---|---|
| E2E_COVERAGE_REDUCED | TRUE: full legacy and two-target coverage is not restored |
| API_CONTRACT_CHANGED | TRUE: additive profile/inputs/status fields and stricter execution admission; fresh Plan required for old approvals |
| APPROVAL_SEMANTICS_WEAKENED | FALSE for implemented paths: fuller identity binding, required verified evidence and one Apply intent per Plan |
| TERRAFORM_EXECUTION_SEMANTICS_WEAKENED | FALSE for implemented paths: exact binary-plan Apply, fixed HCL, readonly lockfile, persistent locked backend |
| K8S_DISTRIBUTION_COUPLING | FALSE in new business paths: standard APIs and supplied explicit contexts; legacy identity strings retained |
| CONTAINER_ENGINE_COUPLING | FALSE in business paths: OCI/Kubernetes runtime, external Podman build/load only |
| REAL_AWS_USED | FALSE: explicit emulator endpoints/dummy credentials; AWS endpoints and ambient artifact credentials rejected |
