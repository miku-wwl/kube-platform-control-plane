# Phase 2.5B implementation

2026-11-01, Pacific/Auckland. Working branch: `main`; base
`54e1b7e0c496f0af47d132ccc82421a22ccd6a94`. Changes remain local and uncommitted.
`PHASE_2_5B = PARTIAL`; [validation](VALIDATION_REPORT.md) controls acceptance.

| Before | Implemented now |
|---|---|
| Provider override `.tf` authored at execution | Fixed `aws-compatible-basic` module with declared string inputs and predefined optional role configuration; runner writes tfvars JSON only |
| Incomplete handwritten deep copies | Generated complete copies; handwritten helper removed |
| Partial approval/input context | Full execution intent plus credential Secret UID/resourceVersion bound to the Plan; complete Apply snapshot equality and verified required artifacts |
| Apply deduplication depended on approval UID | One deterministic Apply name per Plan UID, ownership check and existing-run identity validation |
| Reconstruction before leader acquisition | Leader-owned reconstruction closes admission until persisted Jobs/reservations are read |
| Job create/status ambiguity | Persist Job intent before the create RPC; observe existing Jobs and verified durable terminal evidence; ambiguous execution fails closed |
| Runtime read/delete race; unavailable-target cleanup could skip | UID/resourceVersion deletion preconditions; inventory finalizer retained on unavailable targets |
| Context-name incarnation for new generic targets | Explicit JSON profiles; verify kube-system UID at registration and again whenever resolving a runtime client |
| ApplyOnly runtime objects remained Unknown | Honor the existing ApplyOnly policy after confirming existence; RequireReady continues to require an adapter/condition |
| An unrelated invalid approval blocked a later exact API submission | Ignore differently named invalid records, preserve canonical-name conflicts and idempotent exact retries; records are never overwritten |
| Lost Destroy execution inputs | Complete retained execution snapshot in status, decoded as TerraformRunSpec; no TerraformRun spec immutability rule embedded in replaceable status |

The Operator remains the sole orchestrator. Standard Jobs run the pinned OCI
runner and Terraform 1.14.0; Apply restores the approved binary plan, source and
backend snapshot. Fixed HCL declares the S3 resource and provider; MiniStack
provides cloud APIs. No reconciler provisions clusters, starts engines/emulators,
detects host OS/distribution, or invokes host Terraform.

New `executionProfile`/`inputs` fields are additive. Existing resource kinds,
routes and legacy serialized target values remain. New execution admission
requires emulator configuration, dummy credential Secret and an immutable runner
image. Digests now include the complete intent: old pending approvals must be
replaced by a fresh configured Plan/approval, not silently migrated. Generic
targets require a real incarnation UID. Real AWS execution was not validated.

Artifact clients use explicit emulator endpoints/dummy credentials and reject
AWS/empty endpoints or ambient profiles. Immutable writes use conditional PUT;
matching metadata on retry is insufficient without content verification. Reads
verify SHA-256, including saved Plan, source, backend, inputs, report and terminal
evidence. Runner terminal operation/UID/timestamps/outcome are checked. Failed
post-mutation evidence publication cannot become Ready or trigger blind retry.
Source bundles exclude Git metadata, runtime state, overrides and implicit vars.

The API/console retain explicit draft/validate/submit/approve flows. Class forms
accept execution profile/input JSON only; endpoints and inline sensitive inputs
are validated/redacted. No Apply bypass or visual redesign was introduced.

The removed Phase 2.5A PowerShell lifecycle harness stays removed. Make targets
remain build/check wrappers. Local WSL Podman commands build/load standard OCI
images; temporary source/endpoint transport is ignored developer evidence under
`tmp/phase25b`, outside the product and test transition logic. Existing clusters
and MiniStack containers are preserved. Tests own unique namespaces/classes,
cloud/state/artifact buckets and their dedicated controller processes.

Only necessary tests were added: one controller safety file, one opted-in live
artifact contract file, and one Go lifecycle suite; existing executor/source tests
were strengthened. Generated API/CRD updates account for substantial mechanical
diff. No archived raw logs or duplicate inventories are added to Git.

Remaining work: cross-cluster execution/runtime isolation, complete legacy browser/update/discovery/fence
acceptance, Deployment leader failover and broader credential/role scenarios.
The supplied `podman-minikube` now runs the additional HTTP API and single-cluster
concurrency lanes. The same isolation helper covers either distinct clusters or
separate namespaces on one cluster; those results are reported separately.
Optional scoped transport faults exercise finalizer retention and recovery.
No completion claim follows from a green one-target Go test command.
