# Platform Control Plane（三合一项目）详细设计文档 v1.6.7 — LIFECYCLE CLOSURE

> 参考项目：`valkey-cluster-operator`、`terraform-provider-kubepatch`、`terraform-resource`
>
> v1.6.7 不改变主架构、不新增 CRD、不新增 cloud，只根据 Phase 1～6 Full Lifecycle E2E 暴露的问题收口 lifecycle correctness：
>
> 1. `PlatformEnvironment` 的 Ready 必须严格等待 current-generation infrastructure / runtime / domain readiness
> 2. Manual Approval 后由 `InfraStackController` 自动创建 ApplyRun；不要求人工创建 ApplyRun
> 3. 删除时由 controller 自动创建 Destroy PlanRun，并走同一套 Approval → exact saved-plan Apply
> 4. `ResourceSet` 删除必须先执行 inventory prune；target unreachable 仅允许既有 bounded-skip contract
> 5. 补齐 Namespace / Service / Valkey 等 runtime readiness policy，避免无意义的 `RuntimeReadinessUnknown`
>
> Phase 1～6 在 **Full lifecycle E2E = PASS 且 Manager restart recovery = PASS** 之前不得 Freeze。v1.6.7 属于 v1.6.6 已允许的 correctness fix，不扩大 MVP scope。

---

# 1. 项目定位

本项目是一个：

> **Kubernetes-native Platform Control Plane**

负责：

```text
Infrastructure
→ Target Cluster Connectivity
→ Runtime Bootstrap
→ Runtime Resources
→ Domain Components
→ Health / Recovery / Deletion
```

MVP 不做：

- GitOps 平台
- 通用 Workflow Engine
- 多云平台
- Terraform Cloud 替代品
- Shared-cluster platform
- AI Serving platform

---

# 2. 源项目设计输入

## `valkey-cluster-operator`

吸收：

- reconciliation pattern
- health gate
- durable Job
- stateful recovery

不复制：

- reconcile 内长时间 polling
- monolithic reconcile
- plaintext secret choice
- incomplete cleanup assumptions

## `terraform-resource`

吸收：

- Plan / Apply separation
- workspace / backend state / locking
- saved plan
- lock timeout
- optional Terraform parallelism
- state lineage / serial as useful observation
- sensitive output handling

不复制：

- Concourse adapter
- direct destroy mutation
- delete-on-failure behavior

## `terraform-provider-kubepatch`

吸收：

- Kubernetes API integration
- Provider / acceptance test 思路

不复制：

- hardcoded resource switch
- one-shot patch runtime model
- incomplete Read/Delete lifecycle

---

# 3. Production Architecture

```text
                            Git / CLI / CI
                                  │
                                  ▼
                     PlatformEnvironment CRD
                                  │
                                  ▼
                      EnvironmentController
                                  │
                 ┌────────────────┴────────────────┐
                 │                                 │
                 ▼                                 ▼
            InfraStack                        ResourceSet
                 │                                 │
                 ▼                                 ▼
           TerraformRun                     Target Kubernetes
                 │                                 │
                 ▼                      ┌───────────┴───────────┐
          Kubernetes Job               ▼                       ▼
                 │                 Runtime Resources       ValkeyCluster
                 ▼                                             │
           Terraform CLI                                      ▼
                 │                                       Valkey Operator
                 ▼
                AWS
```

LocalStack 不进入 production architecture，只存在于 Development / Validation Architecture。

---

# 4. Lifecycle

创建 / 更新：

```text
PlatformEnvironment
→ ensure InfraStack
→ InfraStack creates current-generation PlanRun
→ Plan Job
→ resolved execution snapshot
→ NoChange fast-path
   OR
   WaitingApproval
→ approver creates immutable ChangeApproval
→ InfraStackController validates exact PlanRun UID + planDigest + executionContextDigest
→ InfraStackController automatically creates exactly one ApplyRun
→ exact saved-plan Apply
→ InfrastructureReady=True
→ Target Cluster connectivity / Runtime Bootstrap
→ ResourceSet apply + readiness
→ ValkeyCluster readiness
→ EnvironmentController aggregates current-generation conditions
→ PlatformEnvironment Ready=True
```

删除：

```text
PlatformEnvironment deletionTimestamp
→ Environment finalizer holds parent and required children
→ stop new reconcile/runtime mutation
→ wait active mutating Apply
→ ResourceSet finalizer prunes owned target-runtime inventory
   OR bounded skip when target is unreachable
→ InfraStackController automatically creates Destroy PlanRun
   from retained last-applied bundle
→ NoChange fast-path
   OR
   WaitingApproval
→ approver creates immutable ChangeApproval
→ InfraStackController automatically creates Destroy ApplyRun
→ exact saved-plan Destroy Apply
→ InfrastructureRemoved=True
→ release retained management-side children
→ remove Environment finalizer
```

## 4.1 Lifecycle Closure Invariants

### Parent readiness gate

`PlatformEnvironment Ready=True` 仅当：

```text
deletionTimestamp is nil
AND InfraStack is Ready for its current generation
AND ResourceSet is Ready for its current generation
AND every required Domain Component is Ready
```

禁止：

```text
InfraStack still Pending / Applying
→ PlatformEnvironment Ready=True
```

`RuntimeEmpty` 只能表示“当前 spec 确实不要求 runtime/domain resource”，不能作为 infrastructure 尚未完成时的 fallback Ready reason。

### Approval → Apply ownership

MVP **不要求独立的 ChangeApprovalController**。

`ChangeApproval` 是 immutable authorization object；`InfraStackController` 负责：

```text
observe valid ChangeApproval
→ verify exact binding
→ idempotently create one ApplyRun
```

因此成功路径中不允许要求操作员手工创建 ApplyRun。

### Delete ownership

删除 `PlatformEnvironment` 时，Environment / InfraStack / ResourceSet controllers 共同推进同一个 deletion contract：

```text
runtime cleanup
→ Destroy Plan
→ Approval if required
→ Destroy Apply
→ InfrastructureRemoved
→ finalizer release
```

如果 controller 只进入 `RecoveryRequired` 而没有根据 retained last-applied bundle 创建可恢复的 Destroy PlanRun，则 deletion lifecycle 尚未闭环。

---

# 5. MVP Ownership Assumption

一个 `PlatformEnvironment` 拥有一个 dedicated target cluster。

不支持：

- shared EKS
- imported shared cluster
- 多 control plane 共管 target

---

# 6. Sources of Truth

## Management Kubernetes API

保存：

- desired state
- execution requests
- approvals
- conditions
- runtime inventory
- artifact references

## Terraform Backend

保存：

- Terraform persisted state
- workspace state
- backend lock

## AWS API

代表 actual infrastructure state。

## Target Kubernetes API

代表 actual runtime state。

## Object Store

保存：

```text
source-bundle.tar.zst
backend-config.json
plan.binary
plan.json
stdout.log
stderr.log
metadata.json
execution-context.json
runner-result.json
```

全部按 sensitive artifact 处理。

---

# 7. Validation Matrix

```text
                    Unit / EnvTest
                         │
                         ▼
                Fake Terraform Executor
                         │
          ┌──────────────┴──────────────┐
          ▼                             ▼
   AWS Integration                  K8s Runtime
 LocalStack Ultimate                   Kind
          │                             │
          └──────────────┬──────────────┘
                         ▼
                Combined Local E2E
                         │
                         ▼
                     Real AWS
```

Fake Executor：

```text
state machine
timeouts
Indeterminate
stale Plan
controller restart
concurrency gate
100 / 500 / 1000 environment scale
```

LocalStack：

```text
real Terraform CLI
real AWS Provider
saved-plan workflow
S3 backend
S3 lockfile
backend reconstruction
selected AWS APIs
```

Kind：

```text
cross-cluster client
SSA
inventory
readiness
prune
runtime bootstrap
Valkey
```

Real AWS：

```text
IAM
STS
EKS access
VPC/networking
eventual consistency
real service semantics
```

---

# 8. 核心 CRD

```text
PlatformEnvironment
InfraStack
TerraformRun
ResourceSet
ChangeApproval
```

Domain：

```text
ValkeyCluster
```

不因 scheduler、LocalStack 或 validation 增加 CRD。

---

# 9. InfraStack

```yaml
apiVersion: platform.example.io/v1alpha1
kind: InfraStack
metadata:
  name: demo-staging
spec:
  source:
    url: https://github.com/example/platform-infra
    revision: "7e5d9ac3f8d2..."
    path: envs/staging

  backend:
    type: s3
    configRef:
      name: terraform-backend-config
    authRef:
      serviceAccountName: terraform-runner
    lockTimeout: 5m

  workspace: demo-staging

  variables:
    secretRefs:
      - name: demo-staging-tfvars

  executor:
    terraformVersion: "<exact-version>"
    image: ghcr.io/example/terraform-runner@sha256:...
    workDir: /workspace/terraform
    executionTimeout: 60m
    parallelism: 10

  desiredState: Present
  approvalPolicy: Manual
```

`parallelism` 可选；不设置时使用 Terraform 默认值。

---

# 10. Immutable Git Revision

Reconcile source 只接受 full immutable Git commit SHA。

不接受：

```text
main
develop
mutable tag
```

CI / CLI 如需最新 branch：

```text
resolve branch
→ full commit SHA
→ update InfraStack
```

---

# 11. Run Identity

同一 generation 可有多个 PlanRun：

```text
generation 4
├── PlanRun A → expired
├── PlanRun B → stale
└── PlanRun C → current
```

使用：

```yaml
generateName: demo-staging-plan-g4-
```

Kubernetes UID 是最终 identity。

ApplyRun 同样使用唯一 UID。

---

# 12. PlanRun Source Contract

`PlanRun.spec.source` 必须显式区分：

```text
GitCommit
RetainedBundle
```

## Reconcile

```yaml
source:
  type: GitCommit
  url: https://github.com/example/platform-infra
  revision: <full-commit-sha>
  path: envs/staging
```

## Destroy

```yaml
source:
  type: RetainedBundle
  ref: runs/<last-successful-apply-run>/source-bundle.tar.zst
  digest: sha256:...
```

规则：

> Destroy Plan **MUST** 使用最后一次成功 Apply 的 retained bundle。

禁止 Destroy 时重新解析当前 Git desired source。

---

# 13. PlanRun.spec

immutable request：

```text
stackRef
infraStackGeneration
operation=Plan
planMode=Reconcile|Destroy
source contract
backend config input/ref
workspace
variable identity inputs
Terraform version
runner image digest
absolute workDir
runtime OS
runtime architecture
lockTimeout
executionTimeout
optional terraformParallelism
```

---

# 14. PlanRun.status

成功后记录：

```text
resolvedRevision / retainedBundleRef
sourceBundleRef
sourceBundleDigest
resolvedBackendConfigRef
resolvedBackendConfigDigest
terraformLockfileDigest
variablesIdentityDigest
backendConfigIdentityDigest
executionTargetIdentityDigest
executionPlatformIdentityDigest
observedStateLineage
observedStateSerial
stateObservationTime
executionContextDigest
planRef
planDigest
planSummary
hasChanges
planCreatedAt
planExpiresAt
```

`observedStateLineage / Serial` 是 diagnostic / early stale observation，不是 exact Plan identity。

---

# 15. ApplyRun.spec

创建时复制并冻结：

```text
stackRef
infraStackGeneration
operation=Apply
planMode
planRunUID
sourceBundleRef
sourceBundleDigest
resolvedBackendConfigRef
resolvedBackendConfigDigest
terraformLockfileDigest
variablesIdentityDigest
backendConfigIdentityDigest
executionTargetIdentityDigest
executionPlatformIdentityDigest
observedStateLineage
observedStateSerial
workspace
Terraform version
runner image digest
absolute workDir
runtime OS
runtime architecture
lockTimeout
executionTimeout
optional terraformParallelism
executionContextDigest
planRef
planDigest
approvalRef
approvalUID
```

创建后 immutable。

---

# 16. ChangeApproval

绑定：

```text
PlanRun UID
planDigest
executionContextDigest
```

ApplyRun 创建时冻结：

```text
approvalRef
approvalUID
```

RBAC：

```text
developer
→ cannot create ChangeApproval

approver
→ create/get/list/watch
→ no update/patch/delete

controller
→ read-only consumer
```

管理员级别权限不属于平台自身 RBAC trust boundary。

---

# 17. Execution Target Identity

## Real AWS

```text
provider=aws
mode=standard
accountId=<12-digit-account>
region=<region>
```

## LocalStack

```text
provider=aws
mode=custom-endpoint
accountId=000000000000
region=us-east-1
endpointProfileDigest=sha256:...
```

计算：

```text
executionTargetIdentityDigest
```

Plan / Apply 不一致则 mutation 前拒绝。

---

# 18. LocalStack Endpoint Profile

canonicalize：

```text
LocalStack gateway endpoint
AWS Provider endpoint map
Terraform backend endpoint map
S3 addressing mode
AWS account ID
region
EKS endpoint translation policy
```

计算：

```text
endpointProfileDigest
```

LocalStack-generated overlay 不进入 source bundle。

---

# 19. Execution Platform Identity

包含：

```text
runtimeOS
runtimeArch
absoluteWorkDir
TerraformVersion
runnerImageDigest
```

生成：

```text
executionPlatformIdentityDigest
```

MVP 推荐：

```text
OS=linux
arch=amd64
workDir=/workspace/terraform
```

Apply 不一致：

```text
ExecutionPlatformMismatch=True
→ reject
```

---

# 20. Execution Context

包含：

```text
sourceBundleDigest
resolvedBackendConfigDigest
Terraform version
lockfile digest
variablesIdentityDigest
backendConfigIdentityDigest
workspace
runner image digest
executionTargetIdentityDigest
executionPlatformIdentityDigest
terraformParallelism if explicitly set
```

不包含：

```text
operation
temporary credentials
observedStateLineage
observedStateSerial
```

state observation 不属于 exact execution context。

---

# 21. Resolved Backend Config

Plan Job 必须把 non-secret backend config resolve 成 immutable snapshot：

```text
backend type
bucket
key / workspace prefix
region
use_lockfile
backend endpoint settings
other non-secret init arguments
```

保存：

```text
backend-config.json
resolvedBackendConfigRef
resolvedBackendConfigDigest
```

Apply / Destroy：

```text
download same backend config snapshot
→ verify digest
→ obtain fresh workload auth
→ terraform init with same non-secret config
```

不能只凭 `backendConfigIdentityDigest` 猜测 backend 配置。

---

# 22. Backend Credential Boundary

真实 AWS credential 禁止出现在：

```text
resolved backend config
-backend-config persisted artifact
CR status
source bundle
saved execution metadata
```

禁止持久化：

```text
access_key
secret_key
session_token
```

允许：

```text
IRSA
EKS Pod Identity
AWS environment credential chain
STS workload credentials
```

原则：

```text
BackendConfig
= stable non-secret config

BackendAuth
= runtime-only ephemeral identity
```

---

# 23. S3 Backend Contract

MVP：

```text
S3 backend
+
use_lockfile=true
+
bucket versioning enabled
```

参数分离：

```text
lockTimeout
executionTimeout
terraformParallelism
```

---

# 24. Terraform Lock / Parallelism

Plan：

```bash
terraform plan \
  -lock-timeout=<lockTimeout> \
  [-parallelism=<terraformParallelism>] \
  -detailed-exitcode \
  -out=plan.binary
```

Apply：

```bash
terraform apply \
  -lock-timeout=<lockTimeout> \
  [-parallelism=<terraformParallelism>] \
  plan.binary
```

区别：

```text
maxConcurrentApplies
= 同时运行多少个 Terraform ApplyRun

terraformParallelism
= 单个 Terraform Run 内部并行多少 resource operations
```

不能混淆。

---

# 25. State Observation Contract

`lineage / serial` 只作为 early stale observation。

Plan 后可以记录：

```text
observedStateLineage
observedStateSerial
stateObservationTime
```

Apply 前在完成 backend init / workspace select 后再次观察：

```text
terraform state pull
```

若明显变化：

```text
EarlyStalePlan=True
→ reject and fresh PlanRun
```

但：

> state observation 相同 ≠ saved plan 一定仍有效。

Terraform `apply plan.binary` 自身的 saved-plan validation 是 authoritative final check。

因此 `lineage / serial` 不进入 `executionContextDigest`。

---

# 26. `-detailed-exitcode` Semantics

```text
0 = NoChange
1 = Error
2 = ChangesPresent
```

Reconcile + NoChange：

```text
PlanReady=True
InfrastructureReady=True
→ no Approval
→ no ApplyRun
```

Destroy + NoChange：

```text
PlanReady=True
InfrastructureRemoved=True
→ no Approval
→ no ApplyRun
```

---

# 27. Source Bundle Builder

Bundle builder 有两个受控输入：

```text
Pristine Git Checkout
        │
        ├── root Terraform source
        └── .terraform.lock.hcl

Initialized Ephemeral Workdir
        │
        └── whitelisted resolved module source / metadata
                │
                ▼
        Source Bundle Builder
```

明确排除：

```text
LocalStack-generated override
*_override.tf from test harness
.terraform/providers
.terraform/terraform.tfstate
backend init metadata
credentials
temporary tokens
```

禁止：

```bash
tar .terraform/
tar *.tf
```

---

# 28. Source Bundle Retention

InfraStack.status 保存：

```text
lastAppliedSourceBundleRef
lastAppliedSourceBundleDigest
```

规则：

```text
successful Apply
→ update protected last-applied bundle

active InfraStack
→ protected bundle cannot be lifecycle-deleted

Destroy Plan
→ MUST use protected last-applied bundle

Destroy success
→ protection may be released
```

bundle 缺失：

```text
RecoveryRequired=True
→ do not guess
```

---

# 29. Artifact Immutability

Artifact keys：

```text
runs/<terraformRunUID>/source-bundle.tar.zst
runs/<terraformRunUID>/backend-config.json
runs/<terraformRunUID>/plan.binary
runs/<terraformRunUID>/plan.json
...
```

规则：

```text
create once
never overwrite
digest verify on read
private
encrypted
audited
lifecycle controlled
```

Artifact ref 至少保存：

```text
object key
digest
optional object version ID
```

---

# 30. Provider Availability Assumption

Provider binary 不进入 source bundle。

MVP 假设：

```text
Terraform Registry
or configured Provider Mirror
```

Apply / Destroy 时可用。

Cold fresh-Pod E2E 必须证明 lockfile + runner 能重建 provider environment。

---

# 31. LocalStack Routing

Provider routing 与 backend routing 分离：

```text
AWS Provider endpoints
→ LocalStack

Terraform S3/STS backend endpoints
→ LocalStack
```

两者都必须通过 target preflight。

正式 automation 优先：

```text
plain terraform
+
platform-generated ephemeral overlay
+
explicit backend endpoint config
```

`lstk terraform` 只用于 developer smoke/debug。

---

# 32. LocalStack Network Reachability

TerraformRun Job 位于 Management Kind Pod 网络。

流程：

```text
resolve candidate LocalStack endpoint
→ Probe Pod
→ health / STS / required endpoint probe
→ PASS
→ allow mutation
```

禁止假设：

```text
Pod localhost:4566
```

等于 host LocalStack。

---

# 33. Local Network Profiles

test-only：

```text
external-host
in-cluster-service
docker-network-with-explicit-dns
```

CRD 中不出现 `localstack=true`。

---

# 34. Terraform Job Contract

```yaml
restartPolicy: Never
backoffLimit: 0
activeDeadlineSeconds: <executionTimeout>
```

Plan timeout：

```text
Failed/TimedOut
```

Apply timeout：

```text
Indeterminate
```

Controller 不同步长轮询。

---

# 35. Runner Result Protocol

Runner 输出 terminal result：

```json
{
  "operation": "Apply",
  "terraformExitCode": 0,
  "executionOutcome": "Succeeded",
  "artifactsReady": true
}
```

分类：

```text
trusted terminal result + exit 0
→ Succeeded

known non-zero before ambiguous mutation
→ Failed

mutating Job 无可信 terminal result
→ Indeterminate

Terraform success + artifact upload failure
→ Applied=True
  ArtifactsReady=False
```

---

# 36. Plan Execution

```text
Create unique PlanRun
→ acquire Plan concurrency slot
→ target / platform preflight
→ resolve source
→ materialize fixed workDir
→ terraform init with frozen non-secret backend config
→ terraform workspace select/new
→ optional state observation
→ terraform plan -detailed-exitcode -out=plan.binary
→ NoChange fast-path OR
→ build source bundle
→ build backend config snapshot
→ terraform show -json
→ upload immutable artifacts
→ write resolved snapshot
→ release slot
```

---

# 37. Apply Exact Plan

正确顺序：

```text
acquire Apply concurrency slot
→ target / platform preflight
→ materialize source bundle at same absolute workDir
→ verify sourceBundleDigest
→ download resolved backend config
→ verify resolvedBackendConfigDigest
→ obtain fresh backend auth
→ terraform init -lockfile=readonly
→ terraform workspace select <workspace>
→ optional current state observation / early stale check
→ download immutable plan.binary
→ verify planDigest
→ verify executionTargetIdentityDigest
→ verify executionPlatformIdentityDigest
→ verify executionContextDigest
→ terraform apply saved plan
→ release slot
```

禁止：

```text
state pull before init/workspace
re-plan
reuse old Pod filesystem
```

---

# 38. Cold Fresh-Pod Saved-Plan Acceptance

```text
Plan Pod A
→ init / plan
→ upload bundle + backend snapshot + plan
→ Pod A deleted

Fresh Apply Pod B
→ no old filesystem
→ no provider cache
→ restore exact workDir
→ restore backend snapshot
→ fresh init
→ workspace select
→ apply exact saved plan
→ PASS
```

Negative cases：

```text
wrong workDir
wrong OS/arch
wrong runner image
wrong target
wrong backend snapshot digest
```

都必须在 mutation 前拒绝。

---

# 39. Target / Platform Preflight

LocalStack：

```text
mode=custom-endpoint
provider/backend endpoints match profile
test account matches
region matches
Probe Pod PASS
```

Real AWS：

```text
mode=standard
no LocalStack override
account allowlist
region allowlist
expected caller identity
```

Platform：

```text
runtime OS
runtime architecture
runner image digest
Terraform version
absolute workDir
```

任意 mismatch：

```text
HARD FAIL before mutation
```

---

# 40. Plan Expiry

默认：

```text
24h
```

过期：

```text
PlanExpired=True
→ reject Apply
→ fresh PlanRun
```

Approval 不延长 Plan 生命周期。

---

# 41. Failure Recovery

Known failure：

```text
Failed
```

Unknown mutation outcome：

```text
Indeterminate
```

处理：

```text
observe backend / cloud
→ fresh PlanRun
```

禁止：

```text
retry same saved Plan
automatic Destroy
delete_on_failure
```

---

# 42. Execution Concurrency / Backpressure

不增加 scheduler CRD。

```text
maxConcurrentPlans = N
maxConcurrentApplies = M
```

同时：

```text
per InfraStack:
max active mutating Apply = 1
```

Pending Run：

```text
no slot
→ remain Pending
```

controller restart：

```text
adopt active Jobs
→ reconstruct active slot usage
→ continue scheduling
```

---

# 43. Leader Election

MVP concurrency gate 使用 controller-manager leader model：

```text
LeaderElection=true
```

目的：

- 避免多个 controller replicas 各自计算独立 slot
- 避免重复创建 mutation Job
- 让 in-memory concurrency gate 有清晰一致性边界

Job adoption / backend lock 仍保留，不能只依赖 leader election 做 correctness。

---

# 44. Terraform Job Retention / Cleanup

Job terminal 后不能立即删除。

顺序：

```text
Job terminal
→ controller captures exit/result
→ verifies/uploads artifacts
→ persists TerraformRun terminal status
→ marks evidence captured
→ only then allow Job cleanup / TTL
```

可配置：

```text
ttlSecondsAfterFinished
```

但 TTL 必须足够长，且只有 terminal evidence 已持久化后才启用/接受清理。

---

# 45. Parent / Child Deletion Sequencing

Environment finalizer 保证：

```text
PlatformEnvironment
→ InfraStack
→ ResourceSet
→ TerraformRuns
→ Approval / retained artifacts
```

在 Destroy 所需信息使用完之前不会被 ownerRef GC 提前删除。

规则：

```text
Environment deletion starts
→ retain InfraStack and required execution records
→ runtime cleanup
→ Destroy Plan / Approval / Apply
→ InfrastructureRemoved
→ release retained children
→ remove Environment finalizer
```

management-cluster ownerReferences 必须与此顺序兼容；不能只依赖默认 garbage collection。

---

# 46. LocalStack Version / Test Isolation

正式 evidence pin：

```text
LocalStack version/image digest
Terraform version
AWS Provider version
runner image digest
Kind version
Kubernetes version
```

每个 suite 独立：

```text
backend bucket/key prefix
workspace
artifact prefix
resource prefix
test profile
```

避免状态污染和假 PASS。

---

# 47. Target Cluster Identity vs Connection

Cluster identity：

```text
provider
account
region
clusterName
clusterArn / stable provider ID
createdAt / incarnation marker
CA digest
```

Connection properties：

```text
reported endpoint
resolved endpoint
auth mode
connection metadata
```

endpoint 改变不自动等价 `TargetReplaced=True`。

LocalStack 测试允许 test-only endpoint translation，并将 translation policy 纳入 endpoint profile digest。

---

# 48. Runtime Secret Boundary

ResourceSet 禁止 inline：

```yaml
kind: Secret
data: ...
stringData: ...
```

MVP 只引用 target 中已存在 Secret。

---

# 49. Runtime External-Side-Effect Boundary

MVP runtime 只支持：

> 对外部云环境没有独立生命周期副作用，或其副作用完全由 Terraform Infrastructure Plane 管理的组件。

因此 MVP 不默认安装：

```text
Crossplane providers
AWS Controllers for Kubernetes
other controllers that create external AWS resources from target-cluster CRs
```

原因：

```text
target cluster unreachable
→ runtime cleanup may be skipped
→ EKS destroyed
```

如果 runtime controller 在 EKS 外创建云资源，可能产生 orphan。

未来支持此类 controller 时，必须先扩展 deletion contract。

---

# 50. SSA Ownership

```text
fieldManager=pcp-rs-<stable-id>
forceOwnership=false
```

409：

```text
SSAConflict=True
```

平台默认不抢 ownership。

---

# 51. Runtime Readiness

`ResourceSet Ready=True` 必须由 inventory 中所有 **RequireReady** 项共同决定；`ApplyOnly` 项只要求 apply 成功，不阻塞 aggregate Ready。

Namespace：

```text
phase=Active
AND deletionTimestamp is nil
```

Service：

```text
object exists
AND deletionTimestamp is nil
```

通用 Service reachability / endpoint health 不作为默认 ResourceSet readiness；需要服务级 health 时由专用 adapter 定义。

Deployment：

```text
observedGeneration >= generation
AND Available=True
```

StatefulSet：

```text
observedGeneration >= generation
AND readyReplicas == replicas
AND currentRevision == updateRevision
```

Job：

```text
Complete=True → Ready
Failed=True   → NotReady
```

CRD：

```text
Established=True
```

ValkeyCluster：

```text
Available=True → Ready
Degraded=True → NotReady/Degraded
```

若 Valkey Operator 版本使用不同 condition schema，则由 Valkey-specific readiness adapter 归一化。

Unknown：

```text
Applied=True
Ready=Unknown
```

Unknown 默认阻塞 `ResourceSet Ready=True`，除非该 item 显式 `ApplyOnly`。

这样 Namespace / Service 等 baseline resource 不应无理由把整个 ResourceSet 留在 `RuntimeReadinessUnknown`。

---

# 52. ResourceSet Inventory Bound

默认：

```text
maxInventoryItems=500
```

并设置 serialized status size guard。

超过：

```text
InventoryLimitExceeded=True
```

未来再做 inventory sharding / external store。

---

# 53. Cross-Cluster Inventory / Prune

Inventory：

```text
targetIdentity
GVK
namespace
name
UID
```

Prune 前验证：

```text
target identity
ownership label
UID
protected resource policy
```

Namespace / PVC / CRD 默认 protected。

---

# 54. DestroyAll Finalizer

target reachable：

```text
PlatformEnvironment deletion starts
→ ResourceSet finalizer holds cleanup
→ validate inventory ownership
→ prune owned runtime resources
→ persist prune result
→ remove ResourceSet finalizer
→ create Destroy PlanRun from retained bundle
→ Approval if required
→ auto-create Destroy ApplyRun
→ destroy infra
```

禁止在 target reachable 且 inventory 可用时：

```text
delete ResourceSet object
→ skip prune
→ directly destroy infra
```

target unreachable：

```text
bounded timeout
→ RuntimeCleanupSkipped=True
→ record orphan inventory
→ remove ResourceSet finalizer
→ continue Terraform Destroy
```

该 bounded-skip 路径仅对满足 Runtime External-Side-Effect Boundary 的组件安全。

---

# 55. Valkey

```text
ResourceSet
→ Target Kubernetes
→ ValkeyCluster
→ Valkey Operator
```

Readiness：

```text
Available=True → Ready
Degraded=True → Degraded
StorageLimited=True → warning
```

MVP auth 可关闭。

---

# 56. Phase 2 LocalStack Acceptance

必须覆盖：

```text
-detailed-exitcode 0 / 1 / 2
NoChange fast-path
cold fresh-Pod saved-plan Apply
backend snapshot reconstruction
correct Apply init/workspace/state order
same workDir / OS / arch
workspace isolation
provider routing
backend routing
target preflight
S3 use_lockfile
lockTimeout
backend lock contention
advisory state stale detection
source bundle overlay exclusion
artifact immutability
approval UID binding
Destroy from RetainedBundle
```

---

# 57. Phase 6 Reliability / Scale

## 6A Fake Failure Injection

```text
restart
timeout
stale Plan
expired Plan
artifact failure
Indeterminate
target unavailable
delete during Apply
```

## 6B Backpressure / Scale

```text
100 / 500 / 1000 PlatformEnvironments
maxConcurrentPlans=N
maxConcurrentApplies=M
optional terraformParallelism
simulated latency / throttling
```

验证：

```text
active Plans <= N
active Applies <= M
per-stack mutating Apply <= 1
queue drains
leader restart/failover
active Jobs adopted
slot usage reconstructed
duplicate mutation = 0
lost execution = 0
```

## 6C LocalStack Reliability

```text
multiple InfraStacks
S3 lock contention
saved-plan staleness
routing mismatch
network endpoint failure
Destroy lifecycle
```

---

# 58. Deferred Real AWS E2E Validation Target

该项 **不属于 Phase 1～6 Freeze Gate**。具体放入 Phase 7～13 的哪个阶段，必须等 Phase 1～6 Freeze 后完成三个 Halter reference repositories 的 gap analysis，再重新设计。

后续验证目标仍包括：

```text
real IAM
real STS
real EKS access
real VPC/networking
real service semantics
eventual consistency
```

最终：

```text
PlatformEnvironment
→ PlanRun
→ Approval
→ ApplyRun
→ Real AWS VPC/EKS
→ Runtime Bootstrap
→ ResourceSet
→ ValkeyCluster
→ Ready
```

---

# 59. Controller Responsibilities

## EnvironmentController

- orchestration
- create/ensure management-side child resources
- strict dependency gating
- current-generation condition aggregation
- must not publish `Ready=True` before Infrastructure + Runtime + required Domain readiness
- deletion sequencing
- hold Environment finalizer until runtime cleanup and infrastructure removal are terminal

## InfraStackController

- Reconcile PlanRun creation for current generation
- Destroy PlanRun creation from retained last-applied bundle
- latest valid Plan tracking
- NoChange fast-path
- approval validation
- observe immutable `ChangeApproval`
- idempotently create exactly one ApplyRun for a valid approval
- automatically create Destroy ApplyRun after valid destroy approval
- retained bundle selection
- recovery gate

`ChangeApproval` does not require a standalone controller in MVP; InfraStackController is the lifecycle consumer.

## TerraformRunController

- Job create/adopt
- concurrency gate
- target/platform preflight
- backend snapshot handling
- state observation
- runner result classification
- timeout classification
- artifact immutability
- terminal evidence capture

## ResourceSetController

- target client / identity
- endpoint resolution
- SSA / waves
- kind-specific readiness adapters
- readiness aggregation
- inventory
- prune on update/delete
- finalizer
- bounded cleanup skip only when target is unreachable under the defined safety boundary

---

# 60. Failure Matrix

| Failure | Expected |
|---|---|
| Apply tries state pull before init/workspace | test failure; sequence invalid |
| observed state lineage/serial changes | EarlyStalePlan; fresh PlanRun |
| observed state unchanged but saved plan stale | Terraform apply rejects; classify stale/failed without retry |
| current Git differs from last successful Apply during Destroy | Destroy still uses RetainedBundle |
| backend snapshot digest mismatch | reject before init/mutation |
| backend credentials found in persisted config | validation failure |
| LocalStack Plan reused for Real AWS | reject |
| Plan / Apply platform differs | reject |
| artifact overwrite attempted | reject |
| protected retained bundle missing | RecoveryRequired |
| Approval same name/new UID | old ApplyRun does not bind replacement |
| no execution slot | Pending |
| non-leader controller | does not schedule mutation Jobs |
| terminal Job cleaned before evidence capture | test failure |
| Environment deleted during pending Destroy | finalizer retains required children |
| runtime external-side-effect controller present | reject / unsupported in MVP |
| Deployment old generation Available | not Ready |
| StatefulSet old revision ready | not Ready |
| ResourceSet inventory exceeds limit | InventoryLimitExceeded |
| Apply Pod lost without trusted result | Indeterminate |
| InfraStack not Ready but PlatformEnvironment reports Ready | invalid; parent Ready=False with InfrastructureNotReady/Pending reason |
| valid ChangeApproval exists but operator must manually create ApplyRun | invalid; InfraStackController must auto-create exactly one ApplyRun |
| PlatformEnvironment deletion does not create Destroy PlanRun | invalid; controller must create Destroy PlanRun from retained bundle |
| target reachable but ResourceSet is deleted without inventory prune | invalid; ResourceSet finalizer must hold until prune result is persisted |
| Namespace / Service baseline objects remain RuntimeReadinessUnknown | invalid unless explicitly ApplyOnly; use defined readiness policy |

---

# 61. Evidence Metadata

正式 validation 保存：

```text
git commit
controller image digest
runner image digest
Terraform version
AWS Provider version
LocalStack version/image digest
Kind version
Kubernetes version
runtime OS / arch
absolute workDir
resolved backend config digest
execution target identity
execution platform identity
state observation metadata
test profile
```

---

# 62. License / Reuse

- `valkey-cluster-operator`: MIT
- `terraform-resource`: MIT
- `terraform-provider-kubepatch`: MPL-2.0

MVP 对 kubepatch：

> 学习设计思想，重新实现 Dynamic Client + SSA，不直接复制 MPL-covered source file。

---

# 63. 实施路线

```text
Phase 0    Architecture Freeze
Phase 1    Control Plane Skeleton
Phase 2    Terraform Execution + LocalStack Validation
Phase 3    Remote Runtime Plane
Phase 4    Runtime Bootstrap + Local E2E
Phase 5    Valkey Integration
Phase 6    Reliability / Scale / Backpressure
Phase 6.1  Lifecycle Closure
           - parent readiness gating
           - Approval → automatic ApplyRun orchestration
           - automatic Destroy PlanRun / ApplyRun orchestration
           - ResourceSet prune-on-delete
           - runtime readiness closure
           - final full-lifecycle local E2E
Freeze Gate Full lifecycle PASS + Manager restart recovery PASS
Post-Freeze Halter 3-repo gap analysis
Phase 7+   Production-grade roadmap redesign
           (Real AWS E2E remains a later validation target)
```

---

# 64. Architecture Freeze

v1.6.7 继续保持 v1.6.6 的 scope freeze：

- 不新增 CRD
- 不新增 cloud
- 不新增 shared-cluster model
- 不新增 GitOps / Helm / Workflow Engine
- 不新增 AI workload
- 不把 LocalStack 变成 production dependency
- 不继续通过纯文档 review 扩大 scope

但 **Phase 1～6 尚未 Freeze**，直到 Phase 6.1 release gate 同时满足：

```text
Full lifecycle E2E = PASS
Manager restart recovery = PASS
go test ./... = PASS
go vet ./... = PASS
git diff --check = PASS
```

v1.6.7 允许且要求修复本次 E2E 已证实的 lifecycle correctness gaps。

后续优先通过：

```text
code
EnvTest
LocalStack
Kind E2E
failure injection
```

发现和修复问题。

Phase 1～6 Freeze 后，再用三个 Halter reference repositories 做 gap analysis，并据此重新设计 Phase 7+；在此之前不提前扩大 production-grade scope。

允许修改：

- bug fix
- correctness fix
- security hardening
- testability
- observability
- implementation simplification

---

# 65. 最终定位

> **A Kubernetes-native platform control plane with exact saved-plan Terraform execution, controller-orchestrated approval/apply and retained-bundle destroy lifecycles, strict parent readiness gating, deterministic cross-cluster cleanup, immutable approvals and artifacts, bounded execution concurrency, LocalStack-backed AWS integration validation, and explicit failure recovery.**
