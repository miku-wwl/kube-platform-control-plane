# Phase 2.5 architecture contract

Authoritative direction for Phase 2.5A and the subsequent Phase 2.5B refactor.
Cleanup is an intermediate source checkpoint, not completed migration acceptance.

## Responsibility boundaries

| Layer | Owns | Must not own |
|---|---|---|
| Go Operator / controller-runtime | Desired-state reconciliation, exact approval admission, TerraformRun/Job ownership, recovery, concurrency, mutation fences, discovery, runtime inventory, Destroy and finalizers | Cluster/engine/host detection, emulator startup, host DNS changes, Terraform module generation |
| Fixed versioned Terraform HCL module | Provider configuration, declared cloud resources, typed variables and allowlisted outputs | Approval, Kubernetes Job scheduling, controller recovery or top-level environment lifecycle |
| Go Terraform runner in Kubernetes Jobs | Pinned CLI execution, source/backend restoration and verification, saved-plan execution, immutable terminal evidence | Replanning during Apply, silent retry of ambiguous mutation, approval decisions |
| Standard Kubernetes APIs | Desired objects, immutable execution/approval specs, Jobs, status, owner references, RBAC, leader election and finalizers | Distribution-specific lifecycle assumptions |
| MiniStack / supplied AWS-compatible emulator | S3/STS APIs required by the selected modules, backend and artifact store | Execution authority, Kubernetes lifecycle or approval |
| Deployment administrator | Supplied management/target clusters, OCI image availability, workload credentials, backend/artifact buckets, explicit endpoint routing, network policy and kubeconfig mounting | Bypassing business finalizers or claiming unexecuted acceptance |
| Developer tooling / CI | Build, installation, static checks and Go test invocation; Podman is preferred for OCI builds | Infrastructure Plan/Apply/Destroy, recovery, approval automation or global teardown |
| Console and advisory AI | Typed draft, explicit submit, Kubernetes read models, sanitized plan display, exact ChangeApproval creation | Direct Apply endpoint or AI execution authority |

The product must remain independent of Kubernetes distribution, container engine
and host OS. Linux paths in OCI runner Pods describe the execution image, not a
requirement for the developer workstation. Dockerfile syntax is an OCI build
input; no Docker daemon or socket is part of the runtime contract.

## Safety requirements

1. **Exact Plan Approval.** Bind Plan name/UID, plan digest, execution-context
   digest, effective-input digest, report reference/digest, approval UID and plan
   expiry. Validate in controllers; a UI badge is insufficient. Destroy requires
   its own Plan and approval. Changed inputs require a new immutable attempt.
2. **Immutable Artifact Evidence.** Retain source bundle, lockfile/module
   revision, resolved non-secret backend snapshot, saved plan, report, discovery
   and terminal result. Preserve run/Job UID, image/version identity, conditional
   writes and verified reads. Never downgrade immutability to accommodate an emulator.
3. **Controller Restart Recovery.** Reconstruct persisted active Jobs/runs and
   reservations before new execution admission. Completed work and evidence
   survive restart. Missing evidence or ambiguous mutation requires recovery,
   never automatic blind Apply.
4. **Multi-target Isolation.** Infrastructure credentials, runtime identity,
   connection profile, backend state/workspace and ownership are separate. Only
   explicitly registered exact target profiles resolve clients. Do not fall back
   to the current kubeconfig context.
5. **Fail-Closed.** Reject stale approvals, mismatched digests, unknown targets,
   wrong accounts/incarnations, incomplete discovery and durable mutation fences.
   Preserve duplicate Job/Apply prevention and concurrency limits.
6. **Terraform Destroy / Finalizer.** Fence new mutation, wait for active Apply,
   prune only owned runtime inventory, use the last converged retained closure,
   separately approve the Destroy saved plan, record removal, then release
   finalizers. Never force-remove a finalizer to make test cleanup pass.

Kubernetes `v1alpha1` resource names/fields, immutability CEL, RBAC and console
routes are preserved in 2.5A. Existing `kind` / `kind-context` / `local-kind`
values remain compatibility identities with unchanged serialization. New
kubeconfig targets use `kubernetes` / `kubeconfig-context` / `kubeconfig`; this is
explicit configuration, with no inference about how a cluster was created.
Discovery for kubeconfig targets now checks the entire frozen identity/profile,
including incarnation and endpoint. Existing inconsistent discovery fails closed.

## AWS-compatible endpoint boundary

These independent configuration surfaces are explicit; configuring one does not
silently reroute the others:

| Consumer | Configuration and credentials |
|---|---|
| Controller/runner artifact store | `PCP_ARTIFACT_ENDPOINT` and region/bucket/KMS settings; runner receives existing corresponding flags. Explicit HTTP(S) endpoint uses path-style S3. Empty endpoint retains SDK native resolution. SDK credential chain supplies identity; hostnames never inject credentials. |
| Existing Go STS/EKS adapters | SDK configuration, including service-specific `AWS_ENDPOINT_URL_STS` / `AWS_ENDPOINT_URL_EKS`; credentials remain SDK/workload identity. No emulator adapter or EKS simulation is assumed. |
| Terraform provider | Fixed module inputs and explicit S3/STS endpoints; no host-global endpoint rewriting. `test/fixtures/terraform/aws-compatible-basic` requires supplied endpoints and resource names. |
| Terraform S3 backend | Referenced non-secret `backend.hcl`, retained and hashed for execution. Explicit backend endpoint settings, region, bucket, key/workspace and `use_lockfile=true`. Do not put credentials in the ConfigMap or artifact. |
| Local console artifact preview | Existing loopback HTTP restriction remains in `internal/console/server.go`. A remote emulator requires an operator-supplied loopback transport; loosening the console boundary is outside cleanup. |

`AWS_ENDPOINT_URL_*` support was inspected in the installed pinned Go SDK;
actual emulator compatibility remains unverified. Credentials must be supplied by
the deployment to each consumer. The deleted dummy-credential injection is not
replaced by inherited workstation secrets. Local runner credential delivery and
its identity binding require Phase 2.5B work.

Base manager egress retains DNS, TCP 443 and TCP 6443. Site-specific emulator,
source, and API ports require explicit deployment NetworkPolicy rules. No script
opens ports or patches DNS. NetworkPolicy effectiveness still depends on the
supplied cluster and CNI and has not been tested here.

## MiniStack compatibility gates: all pending real integration

MiniStack is the intended local emulator, not yet verified in this checkout.
Do not assume feature parity with historical LocalStack runs. Verify at least:

- S3 path-style addressing, regional bucket behavior, Put/Get/Head/List/Delete,
  metadata, encryption headers, conditional `If-None-Match: *`, idempotent same
  content retries, conflicting-content rejection and version identifiers.
- Terraform S3 backend state/workspace behavior, `use_lockfile` acquisition,
  contention, release and state consistency after failure/restart.
- STS GetCallerIdentity; AssumeRole only where required, with account/role
  validation, complete temporary credentials and expiry checks.
- Pinned Terraform 1.14.0 / AWS provider 5.100.0 init, Plan, approved saved-plan
  Apply, NoChange and independently approved Destroy on newly owned fixtures.
- Pod-to-endpoint routing and externally managed source/image availability.

Synthetic HTTP tests are Go client contract evidence only. No real AWS resources
may be used to close these local gates. Historical evidence and pre-existing
environments must remain untouched.

## Deliberately unresolved in 2.5A

The existing ephemeral AssumeRole provider override in
`internal/terraform/executor.go:prepareExecutionProviderOverride` remains to
preserve the AWS execution path and its tests. Replace it with a fixed-module
authentication contract in 2.5B; do not expand it into a source generator.
`internal/terraform/identity.go:EndpointProfile` is currently not wired end to
end. Kubeconfig registration still uses context-based local identities, not a
verified cluster incarnation UID. See [REFACTOR_HANDOFF](REFACTOR_HANDOFF.md).
