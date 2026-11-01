# Phase 2.5 acceptance coverage matrix

Baseline: `9d149cc82a3cec21998ae4a780a444c631cad6b4`. Original line references
below refer to that tree, not the modified checkout. The removed script can be
read with `git show <baseline>:e2e/Run-LocalE2E.ps1`. Every explicit legacy failure
assertion and result expression is retained in
[LEGACY_HARNESS_ASSERTIONS](LEGACY_HARNESS_ASSERTIONS.md): 108 failure statements,
73 functions and 28 result expressions across 1,678 physical script lines.

**Phase 2.5B status: PARTIAL.** Original Go tests remain; the two provider-override
tests now verify the equivalent fixed-input role contract and absence of source
generation. New essential tests cover admission, snapshots, recovery and emulator
integrity. `e2e/lifecycle_test.go` exercises a real supplied Kubernetes API,
Operator process restarts and MiniStack-backed Runner Jobs. A skipped test is
not acceptance. See [current results](VALIDATION_REPORT.md).

| Current executable coverage | Evidence / remaining gap |
|---|---|
| A: `TestApprovalBindsCompleteSnapshotAndDeduplicatesPlan`, `TestLifecycle/A_ExactApproval` | Snapshot mutations/expiry/NoChange unit negatives; live five mismatched approvals, saved-plan Apply and separate Destroy approval. Full live source/target-change matrix remains open. |
| B: `TestMiniStackIntegration`, `TestLifecycle/B_ImmutableEvidence` | PASS for live object integrity/conditional writes/locking and hashes of Plan/source/backend/report/terminal; corrupt Plan bytes and missing source both reject Apply before Job creation. |
| C: `TestRecoveryReconstructsConcurrencyAndRejectsForeignJobs`, `TestLifecycle/C_RestartRecovery` | Four real Operator process restarts, retained Plan UID and exactly one Apply. Multi-replica Deployment failover is not claimed. |
| D: target verifier tests, reconstruction concurrency test, optional `TestLifecycle/D_MultiTarget` | Two-target lane checks distinct cluster UIDs, cross-target runtime absence, independent states, overlapping approvals, observed Apply contention and cleanup. Live execution remains BLOCKED without SecondContext; this lane is NOT RUN here. |
| E: `TestFixedModuleAndBackendFailClosed`, `TestLifecycle/E_FailClosed` | PASS for unit unsafe endpoint/backend/input guards and live missing/wrong approvals, invalid source and unreachable endpoint with no Apply. Full live discovery/account/fence matrix remains open. |
| F: class-finalizer/runtime-failure tests, `TestLifecycle/F_DestroyFinalizer` | PASS for real independently approved Destroy, runtime-first cleanup and cloud absence; scoped transport 503 produces a failed Job while retaining finalizer/cloud resource, then recovers through a fresh Plan and exact approval. Full legacy deletion matrix remains open. |
| NoChange: `TestLifecycle/NoChange` | Real generation update, NoChange convergence, no second Apply, retained closure used for Destroy. |
| Single-cluster concurrency: `TestLifecycle/SingleClusterConcurrency` | PASS on podman-minikube: two overlapping approvals, observed waiting/active Apply contention with a global limit of one, separate durable workspace state keys and runner identities, intended namespace writes only, ordinary cleanup. Does not close D cross-cluster acceptance. |
| Live HTTP platform API: `PlatformAPI_DraftAndTemplate`, `PlatformAPI_ExactApproval`, main lifecycle | PASS with real Kubernetes/MiniStack: class dry-run/create/duplicate/in-use protection, persisted template identity/delete, advisory draft without execution, explicit submission, actual graph UID/digest, stale/exact approval, read models, stale/current update, NoChange, UID-bound deletion and independent Destroy approval. |
| Scoped transient cloud fault: `F_DestroyFinalizer` with `FaultFile` | PASS, separate 204.25-second lane on podman-minikube. The test-owned transport injects 503 only for its artifact bucket; MiniStack is never reset or stopped. |

The tables below retain the original migration inventory. Planned file names are
requirements, not claims that separate test files or all legacy lanes exist.

## Original Phase 2.5A migration requirements

| Gate | Original test references | Required replacement (planned path) | Phase 2.5A proof / gap |
|---|---|---|---|
| Exact Plan Approval | `Assert-NoApply` L371; `Assert-ApprovalBinding` L421; wrong UID at `Invoke-FullLifecycle` L1275–1280; stale report at `Invoke-Platform` L555–577; `New-Approval` L1064 | `e2e/approval_test.go`: verify all seven approval identity fields, no early Job/runtime, wrong UID/digest/report/context/input, expired/superseded Plan; separate Destroy approval | Existing console exact/stale binding unit tests preserved. Live controller admission PENDING. |
| Immutable Artifact Evidence | `Assert-Artifact` L395; `Assert-PlanEvidence` L405; `Assert-SavedPlanApply` L430; `Assert-Convergence` L449 | `e2e/artifacts_test.go`: hash actual plan/source/report/backend/terminal/discovery objects; cross-check run and Job UID/image/args; conditional writes/tamper/overwrite negatives | Existing source-bundle/runner tests preserved; endpoint/credential regression retained. Real MiniStack semantics PENDING. |
| Controller Restart Recovery | `Restart-Manager` L1159; `Invoke-FullLifecycle` L1245–1272 and L1290–1307 | `e2e/recovery_test.go`: dedicated supplied controller restart during active Plan, after completed Plan and after Ready; same Plan UID/digests, one Job/Apply, stable discovery/inventory/approval | Product reconstruction/gate code retained. No prior direct gate tests; full restart PENDING. |
| Multi-target Isolation | `Invoke-MultiTarget` L1347–1373; runtime negative checks in platform lane; `Assert-TargetService` L1134 | `e2e/isolation_test.go`: separate SA/backend/workspace/target profiles, positive expected target and negative other target, explicit overlapping Apply contention | The minimal verifier test rejects mismatched endpoint/incarnation. Live isolation/concurrency PENDING. Legacy lane approvals were sequential; limit enforcement was not explicitly asserted. |
| Fail-Closed | `Invoke-FailClosed` L1383–1412; `New-FailClosedResourceSet` L1375; invalid approval gate L1280 | `e2e/failclosed_test.go`: unknown target, incomplete discovery, durable mutation fence, wrong account/profile/incarnation and stale artifacts; zero side effects in both targets | Existing approval/AI tests preserved; new exact kubeconfig verifier rejects endpoint/incarnation changes. Real reconciler negative E2E PENDING. |
| Terraform Destroy / Finalizer | `Delete-EnvironmentLifecycle` L1182–1221; `Wait-DestroyApply` L1100; `UPDATE_DELETE` L1344 | `e2e/finalizer_test.go`: prune runtime first, finalizers hold, no unapproved Destroy, separate approved retained-source saved plan, verified exit/evidence, only owned cloud object removed, children/Jobs GC | Executor Destroy/saved-plan and class-finalizer unit tests retained. Complete deletion PENDING. |

## All other legacy acceptance and support behavior

The paths/statuses below retain the Phase 2.5A inventory. Current implementation
uses the consolidated lifecycle suite above; unlisted legacy scenarios remain
unverified, including live platform API and browser acceptance.

| Original gate/function | Reference | Required Go test or external prerequisite | Status |
|---|---|---|---|
| `AUTO_INFRASTACK` / materialization | L382, L1243–1244 | `e2e/lifecycle_test.go`: top-level environment owns stack; source commit/path, workspace, backend, identities and capacity match class | PENDING |
| `FULL_LIFECYCLE` / `TERRAFORM_OUTPUT` | L449–458, L1224–1289 | `e2e/lifecycle_test.go`: actual Plan Job, manual exact approval, Apply Job, emulator bucket, allowlisted discovery digests, ResourceSet inventory and current Ready | PENDING |
| `RECOVERY_AFTER_PLAN` | L1265–1271 | `e2e/recovery_test.go`: preserve completed plan/source digest and exactly one Job after leader restart | PENDING |
| `RESTART_RECOVERY` | L1290–1306 | `e2e/recovery_test.go`: all three restart phases and no duplicate Apply | PENDING |
| `UPDATE_RUNTIME` | L1308–1329 | `e2e/lifecycle_test.go`: new class version changes Service annotation through SSA; Service/inventory UID stable, bucket retained, no extra Apply | PENDING |
| `UPDATE_DELETE` | L1330–1344 | `e2e/lifecycle_test.go` + `e2e/finalizer_test.go`: capacity generation yields NoChange, only initial Apply, then independent Destroy | PENDING; never an autoscaling claim |
| `PLATFORM_CLASS_API` | L470–497 | `e2e/platform_test.go`: real API dry-run/create/current Ready/list/detail; in-use class deletion denied | Unit admission/deletion tests retained; live API PENDING |
| `PLATFORM_TEMPLATE_API` | L499–514 | `e2e/platform_test.go`: ConfigMap storage, no environment/class side effects, duplicate rejection, exact name/UID deletion | Existing code retained; live storage/API PENDING |
| `PLATFORM_READ_MODEL` | L516–535 | `e2e/platform_test.go`: draft is advisory; explicit submit only; materialized stack, waiting approval and absent runtime/Apply | Unit explicit-submit/readmodel tests retained; live PENDING |
| `PLATFORM_PLAN_GRAPH` | L537–553 | `e2e/platform_test.go`: actual run UID/digest, plan and architecture endpoints, exact S3 create node and attribute allowlist | Plan parser tests retained; live artifact/API PENDING |
| `PLATFORM_APPROVAL_API` | L555–577 | `e2e/platform_test.go`: stale report 409, exact immutable approval 201, saved-plan Apply and Ready inventory; no other-target mutation | Unit binding tests retained; live PENDING |
| `PLATFORM_UPDATE_API` | L579–588 | `e2e/platform_test.go`: stale generation 409, accepted UID/generation update, NoChange and one Apply | Live API PENDING |
| `PLATFORM_DELETE_API` | L589–595 | `e2e/platform_test.go`: UID-bound deletion, finalizer-driven independent Destroy approval, API 404 and unused-class cleanup | Live API PENDING |
| `BROWSER_SESSION` | L812–846 | External developer starts local API/web on chosen ports; future Go fixtures supply owned backend state. Separate real browser/manual evidence | Hold-file/process workflow removed; no current browser acceptance |
| `TERRAFORM_DNS_PREFLIGHT` | L702–745 | Supplied cluster administrator provides DNS and connectivity; read-only Go checks may verify routing | DNS mutation deliberately deleted; no replacement DNS controller |
| `Test-Dependencies`, `Test-LocalStack`, `New-Clusters`, `Build-Images`, `Prepare-GitImage`, `Merge-TargetKubeconfig`, `Install-Manager`, `New-GitSource`, `Start-SourceServer`, `New-BackendConfig` | L612–1001 | External cluster/emulator/image/source/backend provisioning; fixed committed modules; explicit test-owned state keys | Engine/cluster creation, Python Git server, Docker image aliasing and LocalStack bootstrap deleted; not ported as scripts |
| `Wait-Until` / `Wait-Condition` | L163, L598 | Go contexts/deadlines, current observed generation, terminal-failure fast exit and useful diagnostics | Lifecycle observation/deadlines PENDING |
| `Save-SourceState` / `Save-ImageEvidence` | L787–810 | Go test evidence: source diff/hash, module lock/commit, deployed image ID and actual runner Job image | Baseline source inventory captured; live image proof PENDING |
| `Save-Snapshot` / `Save-FailureDiagnostics` | L1415–1469 | Go scoped, redacted snapshots and evidence retention; diagnostic failure must not hide primary failure | PENDING; no broad secret dump |
| `Save-Ownership`, `Assert-OwnershipRecord`, `Remove-RecordedRun`, `Verify-Cleanup` | L227, L1478–1570 | `e2e/ownership_test.go`: record ownership first, UID-bound intent cleanup, preserve external clusters/emulator/buckets/evidence; canonical path checks if local temp cleanup is needed | Old cleanup assertions preserved; host/container cleanup deliberately removed |
| `CLEAN_MODE` / `CLEANUP` | L1581–1617, L1662–1670 | `e2e/ownership_test.go`: crashed-run reconciliation from exact recorded resource UIDs; report blocked cleanup when emulator/approval unavailable | No active cleanup command; previous external resources remain untouched |
| `SUITE_*` and `LOCAL_E2E_EXECUTION` summary | L461–468, L1620–1678 | Go test exit/result aggregation and machine-readable acceptance report; skipped lanes must stay NOT RUN | No aggregate lifecycle PASS target in this phase |

## Historical console acceptance cases

Original test definitions: `docs/STAGE2_TEST_DESIGN.md`; recorded outcomes and
screenshots: `docs/STAGE2_VALIDATION_REPORT.md`; incomplete freeze:
`docs/STAGE2_FREEZE_EVIDENCE.md`. All remain unchanged. The following browser
checks are NOT RUN for Phase 2.5A; API/parser unit tests do not replace clicks.

| Original case | Phase 2.5 replacement requirement |
|---|---|
| TC01 AI draft | Real configured advisory model, typed valid form and separate human quality observation |
| TC02 no resource before submit | Observe Kubernetes absence after draft; browser refresh must not submit |
| TC03 submit | Explicit user submit and one owned PlatformEnvironment |
| TC04 Plan awaiting approval | Actual Job/evidence and current waiting state |
| TC05 no unapproved execution | No Apply Job or runtime mutation before exact approval |
| TC06 / TC06-A / TC06-B topology | Real sanitized artifact identity; separate multi-resource create/update/delete visual fixture |
| TC07 approved execution | Exact approval followed by matching saved-plan Job |
| TC08 Ready | Infrastructure/discovery/runtime inventory and current-generation UI state |
| TC09 restart | Backend recovery suite plus correct refreshed UI |
| TC10 isolation | Backend two-target negative assertions plus UI identity separation |
| TC11 abnormal safety | Rejected stale approval/unknown target/fence with no side effect |
| TC12 delete | UID-bound request, held finalizer and pruned runtime |
| TC13 Destroy approval | Separate exact Destroy plan/approval; never reuse create approval |
| TC14 removed | Verified cloud absence, terminal evidence, finalizer removal and API NotFound |
| TC15 cleanup | Only newly test-owned resources absent; previous state/evidence retained |

## Discovered gaps and deliberate regressions

- Historical README reported broad local PASS; Stage 2 freeze documents already
  recorded incomplete current browser acceptance. Neither establishes current
  Phase 2.5 compatibility.
- The old multi-target lane was sequential and did not independently prove
  concurrent-limit contention. Baseline controller unit coverage was only 3.4%;
  runtime, target, reliability and artifact packages were 0% directly covered.
- Removing implicit dummy credentials means emulator Jobs require explicit
  deployment credential delivery. That replacement is B2, not silently assumed.
- LocalStack health/startup, Docker host networking, Kind cluster lifecycle and
  script cleanup no longer exist. Supplied routing, images, backend and source
  hosting are deployment/test prerequisites.
- Exact frozen profile verification is stricter for endpoint/incarnation/CA
  mismatches. This is intentional fail-closed behavior, not a migration fallback.
- Handwritten deep-copy omissions and the runtime prune read/delete UID race are
  baseline risks recorded in REFACTOR_HANDOFF; this cleanup does not claim to fix them.

## Original Go test inventory

The table below is generated from all baseline `*_test.go` declarations. Each
original file is retained byte-for-byte; all run by default except the explicitly
opted-in live Ollama test. Current command outcomes are in CLEANUP_REPORT.

| Original test | Preservation / verification | Required Phase 2.5 integration complement |
|---|---|---|
| `cmd/terraform-runner/main_test.go:8` / `TestSanitizeTargetDiscoveryAllowlist` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`: actual runner output/discovery allowlist |
| `cmd/terraform-runner/main_test.go:28` / `TestSanitizeTargetDiscoveryRejectsEmptyOutput` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`: actual runner output/discovery allowlist |
| `internal/console/classes_test.go:46` / `TestClassValidationCreateAndCopy` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/classes_test.go:97` / `TestClassCreationRejectsInvalidPoliciesAndForgedStatus` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/classes_test.go:138` / `TestClassDeletionChecksFreshUsageAndUID` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/classes_test.go:170` / `TestClassReadinessAndSensitiveDetail` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/classes_test.go:200` / `TestClassCredentialReferencesAllowedAndInlineValuesRejected` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/console_test.go:26` / `TestApprovePlanCreatesExactImmutableBinding` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:63` / `TestApprovePlanRejectsStaleBinding` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:80` / `TestApproveDestroyPlanBehindMutationFence` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:103` / `TestApproveReconcilePlanRejectedBehindMutationFence` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:122` / `TestEnvironmentSummaryRequiresApprovalForChangesPresentPlan` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:131` / `TestReadModelIgnoresMismatchedApprovalBinding` | RETAINED; unit verification in cleanup log | `e2e/approval_test.go`, `e2e/failclosed_test.go` |
| `internal/console/console_test.go:147` / `TestDraftValidationUsesReadyClassAndPolicyBounds` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real class admission/immutability/usage |
| `internal/console/console_test.go:173` / `TestArtifactEndpointRejectsNonLocalHost` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real API/readmodel/explicit-submit boundary |
| `internal/console/console_test.go:184` / `TestDraftRequiresSeparateExplicitEnvironmentSubmit` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real API/readmodel/explicit-submit boundary |
| `internal/console/console_test.go:225` / `TestOllamaDraftGeneratorContract` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:273` / `TestOllamaStructuredOutputFailsClosed` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:300` / `TestOllamaDraftAppliesExplicitCacheIntentDeterministically` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:326` / `TestMalformedOllamaDraftDoesNotCreateResources` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:364` / `TestOllamaProviderConfiguration` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:386` / `TestOllamaResponseFailsClosed` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:421` / `TestOllamaUnavailableDoesNotFallBack` | RETAINED; unit verification in cleanup log | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:434` / `TestOllamaLiveDraftScenarios` | RETAINED; live test skipped without opt-in | Separate opted-in model tests and browser draft checks; advisory only |
| `internal/console/console_test.go:559` / `TestSlugIsDNSLabel` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: real API/readmodel/explicit-submit boundary |
| `internal/controller/environmentclass_controller_test.go:20` / `TestClassFinalizerProtectsUsageAndReleasesAfterEnvironmentRemoval` | RETAINED; unit verification in cleanup log | `e2e/finalizer_test.go`: real Kubernetes admission/finalizers |
| `internal/planview/plan_test.go:8` / `TestParseActionsMetadataAndDependencyGraph` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: sanitized real PlanGraph artifact/API |
| `internal/planview/plan_test.go:58` / `TestParseAcceptsWholeValueSensitiveMasks` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: sanitized real PlanGraph artifact/API |
| `internal/planview/plan_test.go:75` / `TestParseRejectsUnsupportedMajorFormat` | RETAINED; unit verification in cleanup log | `e2e/platform_test.go`: sanitized real PlanGraph artifact/API |
| `internal/terraform/executor_test.go:18` / `TestPrepareExecutionProviderOverrideIsEphemeralAndCredentialFree` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:44` / `TestPrepareExecutionProviderOverrideRejectsUnsafeRoleARN` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:68` / `TestPlanUsesInitWorkspaceAndDetailedExitCode` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:96` / `TestPlanNoChangeIsFastPath` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:111` / `TestDestroyPlanUsesDestroyFlagBeforeExecution` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:128` / `TestApplyNeverCreatesWorkspaceOrReplans` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:150` / `TestApplyIncludesTerraformDiagnosticsOnFailure` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:163` / `TestApplyCreatesMissingWorkspaceWithoutReplanning` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/executor_test.go:185` / `TestVerifyTerraformVersionUsesMachineReadableOutput` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/sourcebundle_test.go:15` / `TestBuildSourceBundleExcludesRuntimeAndOverlayFiles` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/sourcebundle_test.go:69` / `TestBuildSourceBundleDigestIsStable` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/sourcebundle_test.go:87` / `TestRestoreSourceBundleRejectsUnsafeEntries` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
| `internal/terraform/sourcebundle_test.go:94` / `TestBuildSourceBundleRejectsImplicitTerraformVariables` | RETAINED; unit verification in cleanup log | `e2e/artifacts_test.go`, `e2e/lifecycle_test.go`, `e2e/finalizer_test.go` |
