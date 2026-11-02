# Gate C — Multi-Replica Controller Recovery

## 1. Test Objective

以真实 Kubernetes 两副本 Deployment、Leader Lease 和实际 Terraform Job 验证接管，不能以进程重启替代多副本 failover。

## 2. Preconditions

专属 namespace `kpcp-1793581747323844900`，UID `5982f4d0-2e14-438b-a6f2-72bc2dbd87da`。Deployment UID `5b014b91-b00c-4466-b0c0-5a15ec29b533`；两副本、专属 Lease/RBAC/target Secret。ClusterRole 规则复用产品清单，仅新建 QA role/binding，没有编辑共享 RBAC。

Controller 来源 `580aef4dbe75fca11cf253b2aa55306eb44cf3ab`；实际 OCI manifest `sha256:89ad57997f97fba75c0401824998e3d34016f363a7ea59ad76d5b4fbee339fc7`。Dockerfile 排除 `.git`，这里的来源证明是 clean checkout + 构建命令 + OCI revision label，不能冒称镜像含 Go VCS metadata。

## 3. Test Matrix

| 阶段 | 操作 | 本轮结果 |
|---|---|---|
| 初始两副本 | 两 Pod Running/Ready、恰一有效 Lease holder | PASS |
| Active Plan | 按 UID 链删除本轮 Leader Pod，新 Leader 接管原执行 | PASS |
| Pending Approval | Leader 退出；Plan 和人工审批边界保留 | PASS |
| Active Apply | Leader 退出；原 Run/Job 恢复，无重复 Apply | PASS |
| Ready | Leader 退出；实际 runtime/环境状态保留 | PASS |
| Destroy Pending Approval | 独立 Destroy Plan 保留，未沿用 Reconcile 审批 | PASS |
| Active Destroy | Leader 退出；原终态身份、资源删除与 finalizer 收敛 | PASS |

## 4. Execution Steps

1. 创建专属两副本 Controller Deployment，使用产品 healthz/readyz；等待两个 Ready。
2. 读取 Lease UID/RV/holder；根据 Pod → ReplicaSet → 本轮 Deployment UID 验证归属。
3. 在表中六个业务阶段删除当前 Leader Pod，使用 UID precondition；不删除共享 Controller。
4. 等待两个 Ready、新 holder、同 Lease UID 且 RV 前进，保存前后 Pods/Deployment/Lease 及日志。
5. 列出 Plan/Apply Jobs；断言最初主链只有 Plan+Apply 两 Job、一个 Apply Run。
6. 独立审批 Destroy，恢复同一个 Destroy Apply，核对 terminal、cloud 404、sentinel 和普通清理。

## 5. Expected Results

Standby 可就绪但执行准入关闭；新 Leader 完成全量 execution reconstruction 才能准入。接管不丢审批、不复制 Terraform mutation、不错误释放 finalizer。

## 6. Actual Results

基线失败：两个 Running、一个 Ready。确认产品缺陷 **QA-02**：readyz 强制检查只在 Leader 上执行的 reconstruction，Standby 永不 Ready。

最小修复 `580aef4` 按 manager 的 Elected channel 区分 Standby readiness 与 Leader reconstruction；没有打开 Standby Gate。两个回归测试证明 Standby 无法 acquire mutation slot、新 Leader 未重建仍不可就绪、ambiguous active execution 仍拒绝。

修复后实际 E2E **PASS，261.85 秒**；六次真实 Leader 接管。Lease UID 始终为 `d5ed2e5d-ef07-49f3-a80b-fb20e8aa3ecb`。初始 Plan UID `d9e6fd44-c348-4262-ac16-d66ecb7a04e6`，Plan digest `sha256:3d0c8afa0ee4e84210267bda28065e31b90876c7dac470f92ae50dcb2545dccc`。

## 7. Evidence

- [完整阶段、Pod UID、Lease RV/holder 时间线](../evidence/final-gates-20261102/e2e-deployment-corrected.selected.log)
- [保留的 Controller leader-election 日志摘录](../evidence/final-gates-20261102/controller-election.public.log)
- [Deployment / Pods / Lease / Run / Job 收据](../evidence/final-gates-20261102/e2e-deployment-corrected.receipts.public.json)
- [基线失败证据](../evidence/final-gates-20261102/e2e-deployment-baseline.receipts.public.json)、[基线正常清理](../evidence/final-gates-20261102/c-baseline-cleanup.public.json)
- [镜像来源及摘要](../evidence/final-gates-20261102/controller-readiness-image.public.json)
- [readiness 修复](../../../../cmd/controller/readiness.go)、[回归测试](../../../../cmd/controller/readiness_test.go)、[真实 Deployment driver](../../../../e2e/deployment_test.go)

## 8. Failure / Recovery Behavior

基线失败没有创建业务环境；仅停止本轮 Deployment、正常删 namespace/空桶，独占 RBAC 由 owner GC 删除。成功运行正常删除业务对象，再停止本轮 Deployment 和删除专属 RBAC/namespace；没有强制移除 finalizer。

## 9. Remaining Coverage

这是同一管理集群中两副本的真实 Pod 退出接管。没有模拟整个 API Server 故障、网络分区、节点掉电或跨管理集群 HA，不声明生产 HA 认证。

## 10. Final Verdict

**PASS（上述两副本 Leader failover 范围）**。历史单进程恢复与本轮 Deployment lane 分开记录。
