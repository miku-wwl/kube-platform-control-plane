# Platform Control Plane 详细设计 v2.0.3 — FINAL FREEZE LOCKED R3

> **Baseline**
>
> - `v1.6.7`：Phase 1～6.1 Core Contract。
> - 本文：Phase 7～13 Production Overlay。
> - 本文增加一个 **Pre-Phase-7 Core Regression Gate**，但不改变 Phase 编号。
>
> **Normative precedence**
>
> 1. v1.6.7 继续定义 Core lifecycle、安全与基础 execution/runtime contract。
> 2. 本文只在明确写出 override 时覆盖 v1.6.7。
> 3. Phase 7+ 实现必须同时读取 v1.6.7 与本文。
> 4. Phase 7～13 顺序冻结。

---

# 1. Core Invariants

```text
Terraform is executor, not top-level control plane
Plan → Manual Approval → exact saved-plan Apply
Unknown/Indeterminate mutation outcome is never blindly retried
Runtime uses continuous reconciliation
Infrastructure mutation is explicit/gated
ResourceSet owns SSA/readiness/inventory/prune
parent Ready cannot use stale child generation
```

统一使用：

```text
LastConvergedSourceClosure
```

来源：

```text
successful Apply
OR
successful Reconcile NoChange
```

Destroy 必须使用最后一次 converged closure。

---

# 2. Pre-Phase-7 Core Regression Gate

## 2.1 Durable mutation fence

Environment deletion 或 InfraStack 进入 Destroy intent 时，先持久化：

```text
mutationFence=true
```

Fence 必须被：

```text
EnvironmentController
InfraStackController
TerraformRunController final Job admission
ResourceSet runtime mutation admission
```

共同尊重。

```text
fence=true
→ no new Reconcile Apply Job
→ no new runtime mutation
```

已经运行的 mutating Job 只允许继续到 terminal classification。

## 2.2 Delete waits active mutation

```text
deletion
→ durable fence
→ list all active mutating Runs/Jobs for owned InfraStack
→ WAIT
```

只有：

```text
Succeeded
or proven FailedPreMutation
```

才允许 runtime cleanup。

若：

```text
Failed with possible partial mutation
Indeterminate
```

则：

```text
RecoveryRequired=True
→ observe backend/cloud
→ fresh Plan
→ no runtime prune
→ no Destroy
```

## 2.3 Same-generation immutable attempts

恢复 v1.6.7 Run Identity contract：

```text
generation 4
├── Plan A → expired
├── Plan B → stale
├── Plan C → failed
└── Plan D → current
```

使用：

```yaml
generateName: demo-staging-plan-g4-
```

Destroy Plan 同样允许新 attempt。

Kubernetes UID 是 authoritative Run identity。

States：

```text
Active
Succeeded
Failed
Expired
Stale
Superseded
Rejected
Indeterminate
```

Rules：

```text
Expired/Stale
→ Superseded
→ fresh Plan attempt

Failed Plan
→ bounded retry only for explicitly transient pre-mutation error
   OR explicit operator retry
→ new UID

Apply Failed/Indeterminate
→ never retry same saved plan
→ observe
→ fresh Plan
```

## 2.4 NoChange convergence evidence

Reconcile NoChange：

```text
Plan exit=0
→ no Approval
→ no ApplyRun
```

但在 `InfrastructureReady=True` 前必须持久化并验证：

```text
Source Closure
backend snapshot
Effective Plan Input Manifest
execution identities
```

更新：

```text
lastConvergedSourceClosureRef
lastConvergedSourceClosureDigest
lastConvergedRunUID
lastConvergedGeneration
```

若 evidence upload/verification 失败：

```text
ConvergenceEvidenceReady=False
→ InfrastructureReady MUST NOT become True
```

Successful Apply 也更新同一 convergence record。

## 2.5 Final Apply Admission Validator

创建 Apply Job 前统一验证：

```text
mutationFence=false
Plan UID
planDigest
Approval UID
Approval binding
Plan expiry
EffectivePlanInputDigest
SourceClosureDigest
resolvedBackendConfigDigest
variablesSnapshotDigest
secretVariableIdentityDigest
InfrastructureExecutionIdentityDigest
executionPlatformIdentityDigest
runnerServiceAccountIdentityDigest
early state observation / stale check
```

任一 mismatch：

```text
Rejected
→ no Job
```

必须只有一条 authoritative validation path。

## 2.6 Plan time / expiry

Plan expiry 不从 controller “看到结果”的时间开始。

可信 anchor：

```text
Job.status.completionTime
and/or
verified terminal-result.finishedAt
```

规则：

```text
planCreatedAt = actual completion time
planExpiresAt = planCreatedAt + planExpiry
```

可信 timestamp 缺失：

```text
fail closed for approval/apply
```

Production default：

```text
24h
```

## 2.7 Frozen backend reconstruction

Plan：

```text
live backend config ref
→ parse
→ BackendSnapshot.Validate()
→ immutable non-secret backend artifact
```

Apply / Destroy：

```text
NO live backend ConfigMap dependency
→ download snapshot
→ verify digest
→ materialize writable file
→ obtain fresh backend auth
→ terraform init
```

Persisted snapshot 禁止：

```text
access_key
secret_key
session_token
```

## 2.8 PlatformEnvironment ↔ InfraStack exclusive ownership

Legacy Mode：

```text
PlatformEnvironment UID
↔ InfraStack ownerEnvironmentUID
```

Invariant：

```text
one InfraStack
→ at most one PlatformEnvironment owner
```

只有 owner 才能修改 Destroy intent / delete InfraStack。

`ownerEnvironmentUID` 创建后 immutable。

## 2.9 Management-plane child adoption

任何 controller 通过 name 找到已存在 child 时，必须验证：

```text
controller ownerReference UID
expected parent UID
expected immutable identity/spec
```

不匹配：

```text
ChildOwnershipConflict
→ never adopt
→ never mutate/delete foreign object
```

适用于：

```text
PlatformEnvironment → ResourceSet
InfraStack → TerraformRun
TerraformRun → Job
```

## 2.10 Active Job identity

TerraformRun status 必须保存并验证：

```text
Job name
Job UID
TerraformRun ownerReference UID
terraform-run-uid label
```

同名 Job 被删除/recreate：

```text
UID mismatch
→ never treat replacement as original execution
→ mutation result may become Indeterminate
```

## 2.11 API immutability

补齐 v1.6.7 Core contract：

```text
TerraformRun.spec immutable
ChangeApproval.spec immutable
```

优先 CEL/admission validation。

Normal approver：

```text
create/get/list/watch
no update/patch/delete
```

## 2.12 Parent readiness

Normal Environment path 只接受：

```text
InfraStack.spec.desiredState=Present
AND current-generation InfrastructureReady=True
```

不得把：

```text
InfrastructureRemoved=True
```

当成 normal Ready。

若 `PlatformEnvironment.spec.desiredState` 不再是正式 API，则 deprecate；删除统一通过 `deletionTimestamp`。

## 2.13 Pre-7 acceptance

```text
delete during active Apply
delete during Failed/Indeterminate Apply
NoChange first convergence then Delete
NoChange evidence upload failure
expired/stale Plan → fresh same-generation attempt
queued Apply expires before Job admission
backend ConfigMap changed/deleted after Plan
two Environments claim same InfraStack
foreign same-name ResourceSet/PlanRun/Job
Job same-name replacement/new UID
TerraformRun spec mutation rejected
ChangeApproval spec mutation rejected
Destroyed InfraStack cannot satisfy Environment Ready
```

---

# 3. Production Target Cardinality

```text
1 PlatformEnvironment
→ 1 dedicated Target Cluster
```

一个 Management Control Plane 可管理多个独立 target；不支持 shared-target tenancy。

---

# 4. Runtime Intent / Ownership

Phase 7～10：

```text
immutable/versioned LegacyRuntimeProfile
→ LegacyRuntimeResolver
→ ResolvedRuntimeConfig
```

Profile freeze：

```text
profile revision/digest
runtime baseline revision
Valkey settings
operator source/image/manifest digests
network policy profile
data retention policy
```

Phase 11+：

```text
PlatformEnvironment + EnvironmentClass
→ EnvironmentClassResolver
→ ResolvedRuntimeConfig
```

RuntimeObject：

```text
GVK
namespace
name
wave
readinessPolicy
deletionPolicy
ownershipID
object
```

Enums：

```text
RequireReady | ApplyOnly
Prune | Retain
```

Stable ownership：

```text
ownershipID = PlatformEnvironment UID
fieldManager = pcp-runtime-<stable hash>
```

Platform-owned resources 注入 ownership labels；Operator children 不进入 Platform inventory。

Naming：

```text
readable prefix + stable short hash
```

禁止 simple truncation collision。

---

# 5. Phase 7 — Runtime Mutation Admission / Operator

Runtime mutation only when：

```text
current-generation InfrastructureReady=True
InfraStack desiredState=Present
RuntimeTargetIdentity current
TargetAccessible=True
mutationFence=false
Environment deletionTimestamp=nil
RecoveryRequired!=True
```

否则：

```text
ResourceSet may exist
but MUST NOT mutate Target
```

特别是：

```text
Plan pending
WaitingApproval
Apply queued/running
Apply Failed/Indeterminate
Destroy intent
deleting
```

期间 runtime mutation = 0。

Readiness：

```text
RequireReady → blocks aggregate readiness
ApplyOnly    → apply success is enough
```

Wave：

```text
apply wave N
→ wait all RequireReady in wave N
→ wave N+1
```

Valkey：

```text
Wave 0 → Namespace/CRD/bootstrap
Wave 1 → Operator
Wave 2 → ValkeyCluster
```

DomainReady：

```text
Available=True
AND operator observed current generation
```

如果 upstream 缺 generation acknowledgement，允许 minimal compatibility patch + pinned reproducible image。

Delete 使用 reverse dependency waves。

---

# 6. Phase 8 — HA / Reconstruction / Runtime Security

Authoritative truth：

```text
TerraformRun objects
+
actually-active Jobs
```

Leader takeover：

```text
gateReady=false
→ list/adopt ALL actually-active Jobs
→ reconstruct actual counts
→ detect conflicts
→ gateReady=true only after classification
```

Capacity 只控制 **new admission**。

若：

```text
actual active > configured capacity
```

则：

```text
OverCapacity=True
→ keep all active Jobs adopted
→ block new admission
→ drain naturally / alert
```

同一 stack 多个 active Applies：

```text
SafetyViolation=True
→ observe all
→ block new mutation
```

`/readyz`：

```text
cache synced
AND reconstruction complete
AND gate initialized
```

Pod hardening：

```text
runAsNonRoot=true
allowPrivilegeEscalation=false
capabilities.drop=ALL
seccompProfile=RuntimeDefault
readOnlyRootFilesystem where practical
resource requests/limits
```

NetworkPolicy：

```text
default deny
```

Secret read：

```text
namespace-scoped Role/RoleBinding
```

Observability 至少：

```text
pcp_gate_ready
pcp_gate_reconstruction_duration_seconds
pcp_execution_slots_used
pcp_reconcile_total
pcp_reconcile_errors_total
pcp_terraform_runs
pcp_terraform_run_duration_seconds
pcp_indeterminate_total
pcp_target_access_failures_total
```

---

# 7. Phase 9 — Terraform Execution Contract

## 7.1 Effective Plan Input

Plan 前冻结：

```text
exact Git commit
validated source.path
terraformWorkDir
SourceClosureDigest
lockfile digest
resolved module manifest
variablesSnapshotDigest
secretVariableIdentityDigest
resolvedBackendConfigDigest
workspace
InfrastructureExecutionIdentityDigest
executionPlatformIdentityDigest
runnerServiceAccountIdentityDigest
Terraform exact version
runner image digest
```

计算：

```text
EffectivePlanInputDigest
```

Plan 后重新验证 source/input 未变化。

## 7.2 Source Closure

```text
checkoutRoot != terraformWorkDir
```

Closure：

```text
sanitized required repository source
+ .terraform.lock.hcl
+ required local module tree
+ pinned remote module closure/manifest
```

支持：

```text
nested workDir
../../ local modules
pinned remote module
fresh-Pod Apply
retained-source Destroy
```

排除：

```text
.git
provider cache/binaries
state
credentials/tokens
secret tfvars
test/LocalStack overlays
temporary files
```

Production 不接受 undeclared implicit：

```text
terraform.tfvars
*.auto.tfvars
```

Path/filesystem：

```text
reject absolute path
reject ../ traversal
reject workDir outside trusted root
reject symlink escape
reject special files
preserve required normal file mode safely
```

## 7.3 Runner identity

```text
InfraStack runner profile
→ resolved ServiceAccount
→ TerraformRun immutable binding
→ Job.serviceAccountName
→ workload identity
```

Plan 与 Apply 绑定同一 expected runner identity。

Source-fetch component：

```text
MUST NOT inherit Target Provision credentials
```

## 7.4 Variables / secrets

Non-secret：

```text
variables-snapshot.json
variablesSnapshotDigest
```

Secret：

```yaml
secretVariables:
  - variable: db_password
    secretKeyRef:
      name: prod-tf-secrets
      key: db_password
```

只注入：

```text
TF_VAR_db_password
```

禁止 whole-Secret `envFrom`。

Secret binding：

```text
namespace/name
UID
resourceVersion/version identity
key
Terraform variable name
```

## 7.5 Apply Failed recovery

Default：

```text
Apply Failed
→ MutationMayHaveOccurred
→ never retry same saved plan
→ preserve evidence
→ observe backend/cloud
→ fresh Plan
```

只有明确发生在 mutation 前才分类：

```text
FailedPreMutation
```

## 7.6 Durable terminal evidence

Runner 写 immutable：

```text
terminal-result.json
```

至少：

```text
run UID
operation
startedAt
finishedAt
outcome
exit classification
artifact digests
runner identity digest
state observation
```

Pod logs diagnostics only。

## 7.7 Plan review / Approval

```text
plan.binary       → restricted/encrypted
raw plan.json     → transient by default
plan-report.json  → sanitized durable review artifact
```

TerraformRun.status：

```text
planReportRef
planReportDigest
bounded sanitized summary
```

ChangeApproval 绑定：

```text
PlanRun UID
planDigest
executionContextDigest
EffectivePlanInputDigest
planReportDigest
```

Approval object 本身 immutable。

Production：

```text
Manual Approval only
```

## 7.8 Execution snapshot

至少冻结：

```text
SourceClosureDigest
EffectivePlanInputDigest
resolvedBackendConfigDigest
terraformLockfileDigest
variablesSnapshotDigest
secretVariableIdentityDigest
InfrastructureExecutionIdentityDigest
executionPlatformIdentityDigest
runnerServiceAccountIdentityDigest
workspace
Terraform expected version
runner image digest
absolute workDir
OS/arch
lockTimeout
executionTimeout
parallelism
state observation metadata
```

Runner 实际执行：

```text
terraform version -json
```

验证 expected version。

## 7.9 Active execution retention

Non-terminal TerraformRun / Job 在 trusted terminal evidence 持久化前不得被 routine GC。

Finished Job 只有：

```text
terminal-result verified
TerraformRun terminal status persisted
required artifact retention confirmed
```

后才可 TTL/GC。

---

# 8. Identity / Auth / Artifact Boundaries

```text
InfrastructureExecutionIdentityDigest
RuntimeTargetIdentityDigest
TargetConnectionProfileDigest
```

三者禁止混用。

Backend vs provider：

```text
Runner Base Identity
→ Management S3/KMS backend/artifacts

Runner Base Identity
→ STS AssumeRole
→ Target Provision Role
```

Private Git/module/registry auth：

```text
runtime-only
never source closure/status/logs/events
```

Real AWS artifact store：

```text
standard AWS endpoint resolution
no LocalStack override
SSE-KMS
Block Public Access
Versioning
create-once
digest verification
least privilege
```

AWS integration：

```text
AWS SDK for Go v2
```

---

# 9. Phase 10 — Runtime Target Discovery / Multi-Target

任何 successful convergence：

```text
successful Apply
OR
successful NoChange
→ read-only target discovery
```

Discovery 必须使用该 convergence 对应的：

```text
LastConvergedSourceClosure
frozen backend snapshot
EffectivePlanInput identity
```

而不是 mutable current Git/config。

```text
terraform output -json
→ allowlist non-sensitive outputs
→ trusted AWS DescribeCluster verification
→ target-discovery.json
```

RuntimeTargetIdentity：

```text
provider
accountId
region
clusterArn
incarnationID
```

TargetConnectionProfile：

```text
endpoint
CA data/digest
auth mode
network route/profile
```

ResourceSet inventory/prune 只绑定：

```text
RuntimeTargetIdentityDigest
```

Multi-target：

```text
Environment A → dedicated Target A
Environment B → dedicated Target B
```

每 target 独立：

```text
client
credential session
rate limiter
timeout
failure state
identity digest
```

Real AWS identity smoke：

```text
AssumeRole success
wrong account/role deny
S3/KMS access
credential expiry
optional temporary EKS auth/RBAC smoke
```

---

# 10. Phase 11 — EnvironmentClass / Golden Path / Tenancy

EnvironmentClass：

```text
cluster-scoped
platform-owned
spec immutable
versioned
```

Class in-use 时禁止 destructive delete。

Typed PlatformEnvironment API：

```yaml
spec:
  classRef:
    name: standard-eks-valkey-v1
  region: ap-southeast-2
  capacity:
    nodeCount: 3
  valkey:
    enabled: true
    shards: 1
    replicas: 1
```

避免 arbitrary parameter bag。

Migration：

```text
Class Mode XOR Legacy Mode
```

Class Mode：

```text
classRef set
legacy infraStackRef/target absent
```

Legacy Mode：

```text
infraStackRef + target
classRef absent
```

Production 最终 Class Mode only。

EnvironmentClass freezes：

```text
Terraform source SHA
Terraform version
runner image digest
runnerProfile
backend config version/digest
runtime baseline
operator source/image/manifest digests
allowed regions
capacity bounds
dataRetentionPolicy
approvalMode=Manual
```

Transitive immutability：

```text
backend config
runner profile
runtime profile
policy config
operator package
```

全部 versioned + digest-bound。

Runner profile 不直接跨 namespace 引用 ServiceAccount。

Platform 在 tenant namespace materialize：

```text
ServiceAccount
Role/RoleBinding
workload identity binding
```

Internal InfraStack：

```text
typed PlatformEnvironment
+ EnvironmentClass
+ tenant context
→ ResolvedInfrastructureConfig
→ deterministic internal InfraStack
```

Tenant boundary：

```text
one management namespace = one tenant/application team
```

Developer only：

```text
PlatformEnvironment CRUD
read own sanitized status/report
```

Developer cannot：

```text
create arbitrary Pod/Job
read runner Secret
impersonate runner ServiceAccount
create/update InfraStack
create TerraformRun
create ResourceSet
create ChangeApproval
```

Approver 与 Developer 分离。

Quota / policy：

```text
max PlatformEnvironments
max concurrent Plans
max concurrent Applies
allowed EnvironmentClasses
allowed regions
capacity limits
```

---

# 11. Condition Model

```text
ClassResolved
InfrastructureReady
InfrastructureRemoved
ConvergenceEvidenceReady
RecoveryRequired
TargetAccessible
RuntimeReady
DomainReady
DriftDetected
CleanupBlocked
CleanupSkipped
Ready
Degraded
Deleting
```

所有 current-state condition current-generation aware。

Normal Ready：

```text
desiredState=Present
InfrastructureReady=True
ConvergenceEvidenceReady=True
RecoveryRequired!=True
TargetAccessible=True
RuntimeReady=True
DomainReady=True
not deleting
```

---

# 12. Phase 12 — Day-2 / Stateful / Chaos

## 12.1 Drift

```text
read-only Plan
→ Drift report
→ no automatic mutation
```

Outcomes：

```text
NoDrift
DriftDetected
DriftCheckFailed
```

## 12.2 Rollback

```text
select previous known-good desired class/source/parameters
→ fresh Plan
→ Manual Approval
→ Apply
```

禁止复用 old saved plan。

## 12.3 Durable cleanup evidence

Final cleanup 必须生成：

```text
cleanup-evidence.json
```

Parent status 只保存 ref/digest/bounded summary。

## 12.4 Stateful bounded-skip override

```text
no external stateful effect
→ bounded skip may continue

Delete policy + unverified storage cleanup
→ BLOCK infra Destroy

Retain
→ require durable retained-volume evidence
```

Retain evidence：

```text
PVC/PV identity
StorageClass/reclaimPolicy
CSI volumeHandle / EBS volume
KMS dependency
restore evidence
```

Intentional retained volume != orphan。

## 12.5 Chaos / soak

至少：

```text
AWS 429/5xx
S3 unavailable
credential expiry
target API unavailable
DNS failure
leader death
runner Pod loss
missing terminal evidence
plan expiry
approval substitution
artifact mismatch
target identity mismatch
network partition
actual active > configured capacity
```

Soak：

```text
mixed Plan/Apply
multiple targets
approval delay
failure/recovery waves
long queue drain
100/500/1000 fake environments
```

---

# 13. Phase 13 — Full Real AWS / Operator Profile / DR / SLO

Full lifecycle：

```text
PlatformEnvironment
→ EnvironmentClass
→ internal InfraStack
→ Plan
→ Manual Approval OR NoChange
→ durable convergence evidence
→ RuntimeTarget discovery
→ dedicated EKS
→ Runtime Composer
→ Halter Operator
→ current-generation Valkey Ready
→ Update
→ Stateful cleanup/retention evidence
→ Destroy
```

Halter Operator production profile：

```text
replica / leader-election semantics
PDB
resource requests/limits
securityContext
metrics/alerts
```

如果 upstream 无法安全多副本：

```text
document availability limitation
```

DR：

```text
restore management cluster
→ mutationEnabled=false
→ reconnect S3/KMS/state/artifacts
→ reconstruct active truth
→ classify terminal evidence
→ unknown mutation = Indeterminate
→ read-only observation/fresh Plan
→ operator review
→ mutationEnabled=true
```

RPO / RTO：

```text
RPO = management API/CR backup loss window
RTO = management loss → safe-mode visibility restored
```

具体数字仅在真实 backup/restore 后冻结。

SLI：

```text
leader availability
reconcile success rate
reconcile latency
Terraform terminal classification latency
target access success rate
cleanup success rate
approval wait
```

Safety invariants：

```text
duplicate mutation = 0
wrong InfrastructureExecutionIdentity mutation = 0
wrong RuntimeTarget mutation = 0
unapproved mutation = 0
foreign-child adoption = 0
```

Release test 验证 metrics/alerts/failover/error-budget calculation path，不用短期测试声称证明长期 99.9%。

Alerts 至少：

```text
Apply Indeterminate
RecoveryRequired
approval stuck
execution saturation/OverCapacity
gateReady false too long
leader unavailable
target access failure
cleanup blocked/skipped
artifact verification failure
drift detected
```

---

# 14. Reproducible E2E / CI

Repository 必须建立：

```text
README.md
Makefile or equivalent task entrypoints
test/e2e/
CI workflow
```

Pre-7：

```text
delete during active Apply
Failed/Indeterminate blocks Destroy
NoChange first convergence then Delete
NoChange evidence failure
expired Plan → fresh same-generation Plan
queued Apply expires before admission
backend ConfigMap deleted after Plan
exclusive InfraStack ownership
foreign same-name child
Job UID replacement
spec immutability
```

Phase 7：

```text
Apply pending → runtime mutation=0
wave barrier
stable ownership
Valkey current-generation readiness
```

Phase 8：

```text
leader takeover
actual active > capacity
multiple active Applies
readyz gate
Pod/RBAC security checks
```

Phase 9：

```text
nested workDir
local/remote modules
implicit tfvars rejection
path/symlink escape
fresh-Pod Apply
retained-source Destroy
runner ServiceAccount binding
source-helper credential isolation
key-level secrets
approval report binding
Apply Failed recovery
terminal-result durability
```

Phase 10+：

```text
NoChange target discovery
RuntimeTarget mismatch
multi-target
tenant isolation
stateful cleanup
Real AWS identity/full-lifecycle gates
```

---

# 15. Phase Order / Scope

```text
Pre-Phase-7 Core Regression Gate
→ Phase 7 Runtime
→ Phase 8 HA/Security
→ Phase 9 Terraform Contract
→ Phase 10 Target Discovery
→ Phase 11 Golden Path/Tenancy
→ Phase 12 Day-2/Stateful
→ Phase 13 Real AWS/DR/SLO
```

不做：

```text
shared target cluster
Web Portal
multi-cloud abstraction
generic workflow engine
Terraform Cloud replacement
Crossplane/ACK
generic Helm/Kustomize engine
arbitrary app delivery
AI workload platform
```

---

# 16. Freeze Rule

后续普通 implementation detail 作为 bug/security/test fix 处理。

只有真实 E2E、安全测试或 Real AWS evidence 证明 architecture contract 错误时，才重新 architecture review。
