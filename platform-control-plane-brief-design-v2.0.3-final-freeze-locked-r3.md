# Platform Control Plane 简略设计 v2.0.3 — FINAL FREEZE LOCKED R3

> `v1.6.7` 继续定义 Phase 1～6.1 Core Contract。
> 本文冻结 Phase 7～13，并在 Phase 7 前增加一个 **Core Regression Gate**。
> 不新增 Phase、不新增 CRD、不重排 roadmap。

## 1. Production Model

```text
1 PlatformEnvironment
→ 1 dedicated Target Cluster
```

Infrastructure mutation：

```text
Plan → Manual Approval → exact saved-plan Apply
```

Successful Apply **或** NoChange 都必须形成：

```text
LastConvergedSourceClosure
```

Destroy 只从最后一次 converged closure 开始。

## 2. Pre-Phase-7 Core Regression Gate

Phase 7 前关闭：

```text
durable mutation fence
delete waits/recover active Apply
NoChange convergence evidence
unique same-generation Plan attempts
final Apply admission revalidation
expiry anchored to actual Plan completion
frozen backend reconstruction
Environment↔InfraStack exclusive ownership
child owner/UID adoption checks
TerraformRun / ChangeApproval immutability
InfrastructureRemoved parent-ready guard
```

## 3. Phase 7～13

```text
Phase 7   Runtime Composition
          + Runtime Mutation Admission
          + Stable Ownership / Waves
          + Real Valkey Operator

Phase 8   HA
          + Durable Reconstruction
          + Runtime Security / readyz / Observability

Phase 9   Terraform Execution Contract
          + Effective Input / Source Closure
          + Runner Identity Isolation
          + Durable Terminal / Review Evidence
          + Supply-Chain Hardening

Phase 10  Runtime Target Discovery
          + Apply OR NoChange discovery
          + Multi-Target / Multi-Account

Phase 11  EnvironmentClass
          + Typed Golden Path
          + Tenant Boundary / Quotas
          + Internal InfraStack Materialization

Phase 12  Day-2
          + Drift / Rollback
          + Durable Cleanup Evidence
          + Stateful Delete / Retain
          + Chaos / Soak

Phase 13  Full Real AWS
          + Operator Production Profile
          + DR / RPO / RTO
          + SLI / SLO / Alerts
```

## 4. Key Contracts

Runtime mutation：

```text
InfrastructureReady(current)=True
AND Target current/accessible
AND no mutation fence
AND not deleting
```

Plan 前冻结：

```text
source/modules
+ variables/secrets
+ backend
+ execution identities
→ EffectivePlanInputDigest
→ Plan
```

Approval binds：

```text
PlanRun UID
planDigest
executionContextDigest
EffectivePlanInputDigest
planReportDigest
```

Runner：

```text
resolved ServiceAccount
→ TerraformRun immutable binding
→ Job.serviceAccountName
→ workload identity
```

Identity 分离：

```text
InfrastructureExecutionIdentityDigest
RuntimeTargetIdentityDigest
TargetConnectionProfileDigest
```

## 5. Production Definition of Done

```text
Core regression gate PASS
+ Runtime admission/readiness PASS
+ HA reconstruction PASS
+ Effective Input / Source Closure PASS
+ immutable execution/approval PASS
+ runner identity isolation PASS
+ Multi-target / multi-account PASS
+ Golden Path / tenancy PASS
+ Day-2 / stateful PASS
+ Full Real AWS PASS
+ DR / RPO / RTO PASS
+ SLI / SLO / alerts PASS
+ Reproducible E2E / CI PASS
```

## Phase 12 AWS Portability Addendum v2.0.4

This is an additive portability closure. The v2.0.3 freeze remains the historical architecture baseline; no Phase 13 implementation is started here.

- AWS-native artifact storage uses the standard AWS SDK endpoint resolution when the endpoint is empty. LocalStack uses an explicit endpoint and test-only credentials scoped to local runner Jobs.
- Target materialization is provider-aware. Kind remains `local`/`kind-context`; AWS requires account, region, cluster identity, HTTPS endpoint, CA data, and AWS EKS authentication. There is no AWS-to-Kind fallback.
- Terraform discovery is immutable evidence. The runner sanitizes the allowlisted discovery object and records source-closure, backend-snapshot, and effective-input digests. ResourceSet receives only the trusted discovered identity/profile.
- Destroy reuses the retained target-discovery artifact from the last convergence closure. Terraform Destroy output is not treated as a new target-discovery source.
- Infrastructure execution identity and runtime target identity remain separate, with injectable STS/AssumeRole, EKS verification, and target-client resolver boundaries.

Real IAM/STS/EKS/KMS/network verification remains explicitly deferred to Phase 13.
