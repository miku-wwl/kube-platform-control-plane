# Platform Control Plane（三合一项目）简略设计文档 v1.6.6 — FINAL CONTRACT REPAIR

> 参考项目：`valkey-cluster-operator`、`terraform-provider-kubepatch`、`terraform-resource`

## 1. 项目目标

构建一个 **Kubernetes-native Platform Control Plane**：

```text
Cloud Infrastructure
        ↓
Target Kubernetes Runtime
        ↓
Domain Components
```

核心原则：

- Kubernetes 是顶层 Control Plane。
- Terraform 只作为 infrastructure executor。
- Runtime 使用持续 reconciliation。
- Terraform mutation 走 `Plan → Approval → exact saved-plan Apply`。
- Valkey 是第一个 Domain Component。
- LocalStack Ultimate 是正式本地 AWS integration validation layer，不进入 production runtime。

---

## 2. 总体架构

```text
                 Git / CLI / CI
                       │
                       ▼
            PlatformEnvironment
                       │
                       ▼
            EnvironmentController
                       │
         ┌─────────────┴─────────────┐
         ▼                           ▼
     InfraStack                  ResourceSet
         │                           │
         ▼                           ▼
   TerraformRun               Target Kubernetes
         │                           │
         ▼                 ┌─────────┴─────────┐
    Kubernetes Job         ▼                   ▼
         │             Runtime            ValkeyCluster
         ▼                                    │
     Terraform                                 ▼
         │                               Valkey Operator
         ▼
        AWS
```

---

## 3. Terraform 安全执行模型

```text
PlanRun.spec
→ immutable request

PlanRun.status
→ resolved source / backend / target / platform snapshot

ChangeApproval
→ exact PlanRun UID + planDigest + executionContextDigest

ApplyRun.spec
→ frozen snapshot + approvalUID
```

关键约束：

- Plan / Apply 固定 source、workspace、backend、target、Terraform version、provider selection、runner image、OS/arch、absolute workDir。
- Apply 先恢复 source、执行 `init + workspace select`，再观察当前 state。
- state `lineage/serial` 只用于 early stale detection；Terraform saved-plan validation 是最终裁决。
- NoChange Plan 不进入 Approval / Apply。
- Destroy 必须基于最后一次成功 Apply 的 retained bundle。
- backend non-secret config 必须冻结；credentials 只来自 runtime workload identity。
- S3 backend 使用 lockfile；`lockTimeout` 与 `executionTimeout` 分离。

---

## 4. 本地验证模型

```text
                 Unit / EnvTest
                       │
                       ▼
               Fake Terraform Executor
                       │
            ┌──────────┴──────────┐
            ▼                     ▼
     AWS Integration         K8s Runtime
   LocalStack Ultimate          Kind
            │                     │
            └──────────┬──────────┘
                       ▼
               Combined Local E2E
                       │
                       ▼
                   Real AWS
```

- **Fake**：failure injection、scale、backpressure、Indeterminate。
- **LocalStack**：真实 Terraform CLI + AWS Provider + S3 backend / locking。
- **Kind**：SSA、bootstrap、inventory、Valkey。
- **Real AWS**：IAM、STS、EKS access、VPC/networking、最终 E2E。

---

## 5. 关键设计规则

- MVP 只接受 full Git commit SHA。
- `TerraformRun` 使用唯一 Run ID。
- `executionContextDigest` 绑定 execution target 与 execution platform。
- Destroy source 明确为 `RetainedBundle`，不重新依赖当前 Git。
- artifact create-once、digest-verified、不可覆盖；last-applied bundle 在 Destroy 成功前保留。
- LocalStack-generated override 不进入 source bundle。
- Approval 独立 RBAC，ApplyRun 冻结 `approvalUID`。
- `maxConcurrentPlans` / `maxConcurrentApplies` 提供 backpressure；controller-manager 启用 leader election。
- Job 只有在 controller 捕获 terminal result / artifacts 后才允许 TTL cleanup。
- Environment finalizer 保证 Destroy 完成前 management-side children 不被提前 GC。
- Runtime 默认禁止引入会在 EKS 外创建外部云资源的 controller。
- Infrastructure 是 event-driven；Runtime 是 continuous。

---

## 6. 实施路线

```text
Phase 0  Architecture Freeze
Phase 1  Control Plane Skeleton
Phase 2  Terraform Execution + LocalStack Validation
Phase 3  Remote Runtime Plane
Phase 4  Runtime Bootstrap + Local E2E
Phase 5  Valkey Integration
Phase 6  Reliability / Scale / Backpressure
Phase 7  Real AWS E2E
```

---

## 7. MVP 明确不做

- Web UI
- 多云统一抽象
- GitOps / Helm / Kustomize Engine
- 通用 Workflow Engine
- Shared target cluster
- Periodic Terraform drift scan
- LocalStack production dependency
- AI workload 本身

---

## 8. 最终定位

> **A Kubernetes-native platform control plane for exact saved-plan Terraform execution, deterministic destroy/recovery, immutable approvals and artifacts, LocalStack-backed AWS integration validation, cross-cluster reconciliation, and explicit failure recovery.**
