# kube-platform-control-plane — Gap Analysis v2.0.3 FINAL FREEZE LOCKED R3

> 基于当前 source ZIP、v1.6.7 Core Contract、Halter reference repos 与 R3 Production Design。
>
> Phase 7～13 顺序不调整。

# 1. Highest Priority

Phase 7 前关闭：

```text
durable mutation fence
NoChange convergence evidence
unique same-generation Plan attempts
final Apply admission
Plan expiry completion-time anchor
frozen backend reconstruction
exclusive Environment↔InfraStack ownership
child owner/UID adoption safety
Job UID binding
TerraformRun / ChangeApproval immutability
InfrastructureRemoved parent-ready guard
```

---

# 2. Core Regression Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| Delete vs active Apply / durable fence | Critical | Pre-7 |
| NoChange → LastConvergedSourceClosure | Critical | Pre-7 |
| Same-generation fresh Plan attempts | Critical | Pre-7 |
| Final Apply admission revalidation | Critical | Pre-7/9 |
| Plan expiry anchored to real completion | High/Critical | Pre-7/9 |
| Frozen backend reconstruction | Critical | Pre-7/9 |
| Exclusive Environment↔InfraStack ownership | Critical | Pre-7/11 |
| Child owner UID verification | Critical | Pre-7 |
| Job name+UID execution binding | Critical | Pre-7/8 |
| TerraformRun / Approval immutability | Critical | Pre-7/9 |
| InfrastructureRemoved parent-ready guard | Critical | Pre-7 |

Additional required behavior：

```text
NoChange evidence failure → not InfrastructureReady
Failed/Indeterminate Apply → RecoveryRequired → no Destroy
foreign same-name child → conflict, never adopt
```

---

# 3. Runtime / HA Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| Runtime Mutation Admission Gate | Critical | 7 |
| Immutable LegacyRuntimeProfile | High | 7 |
| Stable SSA ownership | Critical | 7 |
| Current-generation Valkey readiness | Critical | 7 |
| Wave / readiness / delete semantics | Critical | 7 |
| Kubernetes name collision safety | High | 7 |
| HA adopts actual active truth | Critical | 8 |
| Multiple active Apply safety violation | Critical | 8 |
| readyz reconstruction semantics | High | 8 |
| Pod / NetworkPolicy hardening | High | 8 |
| Secret RBAC hardening | Critical | 8/11 |

---

# 4. Terraform Contract Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| EffectivePlanInputDigest | Critical | 9 |
| Source Closure / local+remote modules | Critical | 9 |
| Implicit tfvars rejection | Critical | 9 |
| Path/symlink containment | Critical | 9 |
| Runner ServiceAccount wiring | Critical | 9 |
| Source-helper credential isolation | Critical | 9 |
| Key-level SecretKeyRef | High/Critical | 9 |
| Apply Failed recovery | Critical | 9 |
| Durable terminal-result artifact | Critical | 9 |
| Sanitized plan-report evidence | High | 9 |
| Approval binds planReport/Input digest | Critical | 9 |
| Terraform runtime version verification | High | 9 |
| Manual-only production approval | High | 9/11 |
| Non-terminal execution protected from routine GC | High | 9 |

Current source-specific issues behind these gaps：

```text
PlanRun name fixed per generation
Plan expiry created when controller captures result
Apply validator does not use full validation helper
backend ConfigMap still mounted on Apply/Destroy
runner ServiceAccount ref is not wired to Job
whole Secret envFrom is used
terminal result comes from Pod logs
Source Bundle is built after Plan from terraformWorkDir
```

---

# 5. Identity / Target / Artifact Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| Infrastructure vs Runtime identity separation | Critical | 9/10 |
| Backend identity vs Provider role | Critical | 9/10 |
| Private source/registry auth | High | 9 |
| Real AWS artifact hardening | High | 9/13 |
| Target discovery after NoChange | Critical | 10 |
| RuntimeTargetIdentity vs ConnectionProfile | Critical | 10 |
| Cluster incarnation detection | Critical | 10 |
| Dedicated target cardinality | Critical | Global/10 |
| Multi-account STS/EKS identity | Critical | 10 |

---

# 6. Golden Path / Tenancy Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| Typed Golden Path API | High | 11 |
| Class/Legacy XOR migration | Critical | 11 |
| EnvironmentClass transitive immutability | Critical | 11 |
| Internal InfraStack materialization | Critical | 11 |
| runnerProfile → tenant SA materialization | Critical | 11 |
| tenant namespace RBAC boundary | Critical | 11 |
| quota / allowed class-region-capacity policy | High | 11 |

---

# 7. Day-2 / Production Gaps

| Gap | Severity | Phase |
|---|---:|---:|
| Read-only drift detection | Medium/High | 12 |
| Fresh-Plan rollback | High | 12 |
| Durable cleanup evidence | High | 12 |
| Stateful bounded-skip override | Critical | 12 |
| Cloud-volume Delete/Retain evidence | Critical | 12/13 |
| Chaos / soak | High | 12 |
| Halter Operator production profile | High | 13 |
| DR safe mode | High | 13 |
| RPO / RTO | High | 13 |
| SLI / SLO / alerts | High | 13 |

---

# 8. Reproducibility Gap

Current ZIP contains 45 Go test functions, including unit, controller, LocalStack-conditional and Kind-conditional tests, but does not contain：

```text
README.md
Makefile
.github/workflows CI
stable top-level test/e2e harness
```

Required from Pre-7 onward：

```text
one-command local gates
documented prerequisites
stable fixtures
CI lanes
release evidence metadata
```

**Severity:** High
**Phase:** Pre-7 through 13

---

# 9. Final Phase Plan

```text
Pre-Phase-7
Core Regression Gate

Phase 7
Runtime Composition
+ Runtime Admission
+ Stable Ownership
+ Operator Compatibility

Phase 8
HA
+ Actual-Truth Reconstruction
+ Security / readyz / Observability

Phase 9
Terraform Contract
+ Effective Input / Source Closure
+ Immutable Approval / Execution
+ Runner Identity
+ Durable Evidence

Phase 10
Runtime Target Discovery
+ Multi-Target / Multi-Account

Phase 11
EnvironmentClass
+ Typed Golden Path
+ Tenant Boundary / Quotas

Phase 12
Drift / Rollback
+ Cleanup / Stateful
+ Chaos / Soak

Phase 13
Full Real AWS
+ Operator Production Profile
+ DR / RPO / RTO
+ SLI / SLO / Alerts
```

---

# 10. Verdict

```text
Phase 1～6.1 baseline
→ keep as historical Core validation baseline

Pre-Phase-7
→ must close current-source regressions/gaps above

Phase 7～13
→ ordering remains frozen
```

普通实现发现作为 correctness/security/test fix；只有真实 E2E、安全或 Real AWS evidence 证明 architecture contract 错误时，才重新 architecture review。

## 11. Phase 12 AWS Portability Exit Gate Addendum v2.0.4

Closed locally:

- AWS-native S3 artifact-store construction with empty endpoint and no synthetic credentials.
- Provider-aware Kind/AWS target materialization, identity separation, target-discovery evidence, trusted ResourceSet propagation, and wrong-target fail-closed checks.
- Injectable STS AssumeRole, EKS DescribeCluster verification, EKS token/client boundary, and multi-target resolver isolation.
- Terraform retained source/backend closure and Destroy target-discovery retention. Destroy evidence is required before finalizer removal.
- Controller-managed AWS runner role annotation boundary and scoped local Job credential behavior.

Validated with unit tests, fake SDK adapters, Kind, LocalStack Ultimate, real Terraform CLI against LocalStack, live manager restart, safe update, approval, Apply, Destroy, and finalizer cleanup.

Deferred to Phase 13:

- Real AWS IAM/STS/EKS/KMS/VPC/EC2/S3 semantics, quotas, throttling, networking, cost controls, and production identity/account isolation.
- Real AWS production Halter storage profile, DR/RPO/RTO, SLI/SLO/alert evidence, and production rollback/failure injection.

No Phase 13 code path was started in this work cycle.
