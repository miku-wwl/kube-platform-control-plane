# Phase 2.5A cleanup report

Date: 2026-11-01, Pacific/Auckland. Repository:
`miku-wwl/kube-platform-control-plane`.

`PHASE_2_5A_CLEANUP = COMPLETE`

This status closes source cleanup and the refactor handoff. It does **not** close
Phase 2.5 migration, MiniStack compatibility, live Kubernetes lifecycle or browser
acceptance. Missing integration replacements are explicitly pending below.

## Baseline and scope

- Started on clean `main`, tracking `origin/main`, at
  `9d149cc82a3cec21998ae4a780a444c631cad6b4`. No pre-existing staged, unstaged or
  untracked user changes were present. Delivery uses `main`; the baseline above
  identifies the pre-cleanup source, and Git history records publication.
- Inventoried all 97 tracked paths, including generated API/CRDs, manifests,
  source, tests, dependencies, fixtures, web files and historical evidence.
  Decisions are summarized below. Full per-file classifications and pre-edit
  hashes are retained locally in `$TEMP/kpcp-phase25a-20261101/review-evidence`,
  outside repository source.
- Baseline `go test ./... -count=1 -coverprofile=...`, `go build ./...`,
  `npm --prefix web ci` and console build all exited 0 before removal. Live
  Ollama inference was opt-in and not run. Baseline total statement coverage:
  17.7%; controller 3.4%; artifacts/target/runtime/reliability 0.0%.
- No cluster, container, emulator, service or pre-existing environment was
  started, stopped, patched or destroyed. No AWS resource calls or Terraform
  Plan/Apply/Destroy were performed. Verification stayed local and did not use
  a live cloud or cluster environment.

## Changes and classifications

| Decision | Component | Actual outcome |
|---|---|---|
| KEEP | Go controllers, runner, runtime SSA/inventory/readiness/prune, concurrency/reconstruction, AWS EKS/STS adapters, plan parsing, console/API | Retained control-plane behavior and safeguards; no script workflow substituted |
| KEEP | `api/v1alpha1/`, all seven CRDs, RBAC, existing seven Go test files / 42 test functions | Byte-identical to baseline; no schema, CEL, RBAC or original test removal |
| KEEP | `Dockerfile`, `runner.Dockerfile`, provider lockfile | OCI build definitions retained; Terraform/provider pins unchanged; lockfile relocated byte-identically |
| KEEP | `docs/STAGE2_*`, seven screenshots | Byte-identical historical evidence; dated results never promoted to current proof |
| REPLACE | `e2e/Run-LocalE2E.ps1` | Removed infrastructure controller/harness; all explicit failure assertions extracted before deletion; Go integration port pending |
| REFACTOR | Make, target registration/materialization/discovery, S3 store, runner Job builder, fixture, UI presets/labels, active documentation | Explicit generic inputs replace engine/emulator/distribution assumptions |
| DELETE | `isLocalEndpoint`, `isLocalStackEndpoint`, unused `LocalAssumeRole`, unused `KubeconfigClientFactory` | Removed hostname credential inference, synthetic STS session, and duplicated unregistered on-demand kubeconfig path; real AWS adapters retained |
| DELETE | Script-created clusters/emulator, Docker lifecycle, `awslocal`, Python fixture server, image aliasing, host endpoint rewriting, DNS patching, process/global cleanup, lifecycle Make wrappers | No replacement procedural infrastructure framework |

Deleted file: `e2e/Run-LocalE2E.ps1` — **1,678 physical lines removed** (1,594
nonblank lines). Its SHA and original line references are preserved in
[LEGACY_HARNESS_ASSERTIONS](LEGACY_HARNESS_ASSERTIONS.md), including 108 explicit
failure statements, 73 functions and 28 result expressions. Source remains
retrievable from the baseline Git tree. The Makefile removes 24 lines and adds
one (23 net removed); those are tooling wrapper lines, separate from script lines.

Relocated fixture files:

- `test/fixtures/terraform/localstack-basic/main.tf` →
  `test/fixtures/terraform/aws-compatible-basic/main.tf`: fixed resource retained;
  explicit provider S3/STS endpoints, external credentials, generic target auth,
  distinct AWS provider region and runtime target region.
- Same-directory `variables.tf`: removed Docker hostname, fixed bucket and Kind
  context defaults; endpoints, bucket and context/connection are supplied.
- Same-directory `.terraform.lock.hcl`: byte-identical relocation; AWS provider
  stays at 5.100.0 and Terraform stays at 1.14.0.

Modified baseline files (16 with semantic changes):

- `Makefile`, `README.md`, `docs/architecture.md`, `go.mod`.
- `cmd/controller/main.go`.
- `internal/artifacts/s3.go`, `internal/terraform/job.go`.
- `internal/target/identity.go`, `internal/target/discovery.go`,
  `internal/target/factory.go`.
- `internal/controller/infrastack_controller.go`.
- `config/manager/networkpolicy.yaml`.
- `web/src/App.tsx`, `web/src/ClassManager.tsx`, `web/src/classTemplates.ts`,
  `web/src/i18n.tsx`.

`go.sum` was normalized locally from CRLF to LF for a clean tidy comparison;
its checksum entries and dependency versions are unchanged (no semantic Git diff).
The credentials module is still required transitively by the AWS SDK and is now
correctly indirect in `go.mod`. No dependency upgrades were made. Web dependency
manifests and lockfile are unchanged.

Retained only three minimal new regression tests in
`internal/artifacts/s3_test.go`, `internal/target/kubeconfig_test.go` and
`internal/terraform/job_test.go`: explicit S3 credentials/endpoint settings,
rejection of changed target identity/profile, and no implicit Job credentials.
Speculative preflight/wiring/SDK tests and tests of unchanged artifact logic
were removed. These three tests total 85 lines; all original test files are unchanged.
The repository retains the four requested deliverables, legacy assertion
extraction, dated README validation record and fixed fixture README. Detailed
JSON inventories and duplicate audit/log documents are kept outside the repository;
76 baseline files remain byte-identical, plus the relocated provider lockfile.

## Remaining scripts and coupling

There are no active `.ps1`, `.sh`, `.bash` or `.py` product/test scripts.
Make only starts Go API/Vite, builds the console and invokes static/unit checks.
Go runner Jobs call Git and Terraform binaries
directly, with no shell orchestration. The existing execution-time AWS role HCL
override is retained as a Phase 2.5B migration item; it must not grow into a module
source generator.

| Coupling | Remaining location / meaning |
|---|---|
| Docker | Dockerfile names, `.dockerignore` and dated historical text only; no Docker CLI, socket or daemon requirement in active code/Make |
| Kind | `kind`, `kind-context`, `local-kind` remain explicit compatibility identities in `internal/target`, startup registration, old test fixtures and a legacy UI option. No cluster creation/deletion or distribution detection. New defaults use generic Kubernetes. |
| LocalStack | Historical reports, extracted legacy assertions, inventory old-path references and the fixture README's historical rename note only. No active health API/bootstrap, `awslocal`, credential or hostname branching. |
| Host OS | No OS detection added. Runner filesystem paths target its existing Linux OCI image. Both image recipes still build Linux/amd64; other image architectures require deployment/build configuration work. |
| Deployment | Manager image `platform-control-plane:phase12-local` remains a template placeholder. DNS/CNI selectors, API ports, source/image hosting, endpoint routing and credentials are supplied prerequisites. |

Base manager NetworkPolicy no longer opens the old emulator/fixture-server ports
4566/18080. DNS/443/6443 and all RBAC remain. Explicit site endpoint egress is
required; this narrows the base policy and can intentionally block local traffic
until deployment configuration is supplied. No live CNI enforcement is claimed.

## Verification

| Check | Baseline | Final cleanup |
|---|---|---|
| Go default tests | PASS | PASS: 44 top-level PASS, one opt-in SKIP |
| Go build | PASS | PASS on Windows/amd64 |
| Go vet | Not separately run | PASS |
| Go race detector | Not run | PASS for default tests |
| Go Linux/amd64 cross-build | Not run | PASS; compile only |
| Console TypeScript/Vite build | PASS | PASS |
| Dependency tidy / module verify | Baseline dependencies built | PASS; no version/checksum changes |
| Terraform fixture format | PASS (baseline copy) | PASS |
| Terraform init with backend disabled | BLOCKED: host 1.15.5, module pin 1.14.0 | Same BLOCKED version mismatch, exit 1 |
| Terraform validate | NOT RUN | NOT RUN because init is blocked |
| Diff whitespace / YAML / protected hashes / active links / coupling scan | Clean Git baseline recorded | PASS |
| Live Kubernetes / MiniStack lifecycle / current browser acceptance | NOT RUN in this cleanup | NOT RUN / Go replacements pending |
| OCI image build/runtime | NOT RUN | NOT RUN; container engine unavailable |

Raw command logs remain at `$TEMP/kpcp-phase25a-20261101`; detailed verification
results and audit inventories are archived in its `review-evidence` subdirectory.
The latest minimized-suite output is `minimal-test-race.jsonl` in the same
local directory; the table above retains outcomes without duplicating logs.


No cleanup-induced Go build, unit-test, vet or console-build failure remains.
The initial Terraform invocation also exposed an existing host CLI configuration
pointing to absent `D:/tf-plugin-cache`. Verification used a temporary empty CLI
configuration and process-local cache override, then restored the invoking
process environment; no user Terraform configuration was edited. Both the
untouched baseline fixture and new fixture then failed init with the same
1.15.5-versus-pinned-1.14.0 error. Version constraints were not weakened.

## Preserved, lost and pending coverage

All original 42 Go tests remain in place. The minimal suite now contains 45
top-level tests: 44 PASS and the original live Ollama test SKIP. No replacement
E2E entry point or synthetic S3 server is retained; integration remains pending.

The deleted active harness means lifecycle, restart recovery, multi-target,
fail-closed, Destroy/finalizer, live API and cleanup suites cannot currently run
with their previous commands. Those executable E2E lanes are **lost pending the
Go port**, while their behavioral requirements and historical evidence are
preserved. The [coverage matrix](ACCEPTANCE_COVERAGE_MATRIX.md) maps every original
Go test, every legacy gate/support behavior and historical TC01–TC15 to required
replacements. Source removal alone is not coverage preservation.

## Compatibility and risks

- API types, all seven CRD schemas/CEL validations, RBAC and console route
  contracts are unchanged. No source bundle, approval, Job UID, retained-state,
  mutation fence or finalizer admission checks were relaxed.
- New `kubernetes` / `kubeconfig-context` values are additive in already-string
  fields. Legacy identity serialization remains unchanged; existing stored specs
  are not rewritten. Kubeconfig discovery now verifies the entire frozen
  identity/profile, so endpoint/incarnation/CA mismatches fail closed.
- Removing implicit dummy credentials intentionally breaks old emulator execution
  until explicit authorized workload credentials are delivered to controller,
  runner and console. Custom endpoints now use path-style S3 uniformly. The
  console's loopback artifact/inference restrictions remain intact.
- MiniStack S3 conditional writes/encryption/metadata, STS, Terraform locking and
  state consistency remain UNKNOWN pending actual integration. Do not weaken
  immutable evidence to work around unsupported APIs.
- Pre-existing deep-copy omissions, context-name incarnation identity, runtime
  prune read/delete UID race, untested concurrency contention and the ephemeral
  provider-role override are explicitly handed off. No production-readiness claim.
- Historical Stage 2 browser acceptance/freeze was already incomplete at baseline.
  No browser clicks, human model-quality judgment or current UI acceptance ran.

## Remaining Phase 2.5B work

[REFACTOR_HANDOFF](REFACTOR_HANDOFF.md) gives exact files, symbols, dependencies
and closing evidence for B1–B9: endpoint/auth identity admission; explicit runner
credentials; fixed-module role configuration; complete target registration and
incarnation verification; deepcopy correctness; supplied deployment/image/egress;
real MiniStack capability probes; all Go lifecycle suites; ownership/diagnostics
and separate browser acceptance. All six safety-critical integration gates must
be proven before declaring the overall migration complete.
