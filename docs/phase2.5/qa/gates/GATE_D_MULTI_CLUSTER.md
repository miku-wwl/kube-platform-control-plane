# Gate D — True Multi-Cluster Isolation

## 1. Test Objective

在两个真正独立的 Kubernetes API 上验证身份、凭据、runtime ownership、state/workspace、并发门禁、重启恢复及逐目标 Destroy。

## 2. Preconditions

| 身份 | Cluster A | Cluster B |
|---|---|---|
| Context | `podman-minikube` | `kpcp-gates-20261102-0223-b` |
| kube-system UID | `2c62d6a8-08b6-460d-bb1b-ac59557dc281` | `60e4ef1d-fb2f-4231-9d48-71a87696d6e7` |
| Node UID | `ddd9911b-a2fe-471c-940b-83a82b87b287` | `52860859-364e-4aa1-830c-0790cf1590c1` |
| Kubernetes | v1.37.0 / Ready | v1.37.0 / Ready |

主机可用内存约 12.7 GB；B 使用 2 CPU / 2048 MB、独立 profile/MINIKUBE_HOME/kubeconfig/CA/client certificate，复用只读缓存副本。未改全局 minikube config、现有 profile 或引擎服务。Rootless Podman/containerd 与独立 `MINIKUBE_HOME` 的用法依据 [Minikube Podman 文档](https://minikube.sigs.k8s.io/docs/drivers/podman/)及[配置文档](https://minikube.sigs.k8s.io/docs/handbook/config/)。

## 3. Test Matrix

| 检查 | 本轮实际结果 |
|---|---|
| 两个独立集群 | PASS；实际 kube-system UID 不同，非两个 namespace 代替 |
| 两个已注册 target | PASS；固定 profile/context/incarnation 注册 |
| 独立凭据 | PASS；CA/client cert fingerprint 均不同；交换证书读取均 Unauthorized |
| runtime ownership | PASS；直接读取两集群 ConfigMap，目标资源存在、另一目标资源不存在 |
| workspace / durable state / runner identity | PASS；两个 workspace、runner SA、实际 S3 state key 分离 |
| overlapping approvals / concurrent Plan | PASS；两个 Plan 先完成、随后连续审批，两者并发调度 |
| global Apply limit=1 | PASS；观察一个 active、另一个 waiting，任何观测 active 不超过 1 |
| contention 中 Controller 重启 | PASS；原 Job UID 保留，各目标只有一个 Reconcile Apply Run/Job |
| incarnation mismatch | PASS；同批独立启动拒绝 lane 实际读取 kube-system 后拒绝，非运行中 cluster replacement |
| per-target Destroy / cleanup | PASS；两个独立 Destroy Plan/审批，各自 cloud 删除、环境及 B namespace 正常消失 |

## 4. Execution Steps

1. 只读确认主集群 UID、原 Podman/MiniStack 容器 ID、主机容量和旧第二 context 不可用。
2. 在独立 QA home 创建 `kpcp-gates-20261102-0223-b`，不重置现有集群。读取 B 实际 UID/Node Ready；合并的是仅本轮私有 kubeconfig，不修改用户默认 context。
3. 正常凭据分别读各集群；交换 client cert/key，保留目标 CA，必须得到认证拒绝。公开只保留 certificate fingerprint，不包含密钥/证书正文。
4. 在 A/B 各创建同名但 UID 不同的专属 namespace，分别注册 target；创建 `concurrent` 和 `second` 环境及固定模块 Plan。
5. 对两份 Plan 连续创建独立 exact approval；观察 global limit=1 时 active/waiting，并在真实 contention 中重启专属 Controller。
6. 从两个真实 API 读取 `concurrent-runtime` / `second-runtime`，核对 owner 值及对端资源缺失；读取两个实际 state object。
7. 各目标删除、生成新 Destroy Plan、独立审批；核对正常 finalizer、cloud 404、B namespace 消失。
8. 在全部业务清理后，按 container ID/profile/cluster UID 验证归属，再普通删除本轮 B profile。禁止 `delete --all`、共享 reset 或 finalizer 强制删除。

## 5. Expected Results

凭据不能跨集群读取；runtime mutation 只落在正确 target；state/approval/Job 不串线；重启不复制 Apply；逐目标清理不影响另一集群。

## 6. Actual Results

最终 lane 使用 E2E 源码 `5deadde`，实际 Controller clean binary 来源 `fd90a86`，两者产品源码差异为空。namespace `kpcp-1793582521855488800`：A UID `829964c2-262a-466a-b9da-4fd4b0381c17`，B UID `9bdf8dc7-53dc-4fe6-a99f-26f4437be4c7`。

两次前置尝试未进入 D，均保留 FAIL：第一次把持久化 Job intent 误判为 active Job；第二次尚未等到 InfraStack WaitingApproval 就审批，API 正确返回 409。最小测试修复要求实际 Job UID/owner/active 状态、重启后的审批状态，并按 owner UID 统计 Job；没有改产品准入规则。这两批均零 Apply，正常 finalizer 清理后重新执行。

最终实际 D 阶段 **PASS（283.80 秒）**，完成独立凭据拒绝、双目标 Plan/Apply、contention restart、真实 API 读隔离、state 分离及逐目标 Destroy。包含主环境清理的整次 lane **PASS（512.02 秒，exit 0）**，UTC 01:21:55–01:30:34。

## 7. Evidence

- [两个真实 Cluster / Node 身份](../evidence/final-gates-20261102/two-cluster-identities.public.json)
- [证书 fingerprint、双 Plan、contention、恢复后 Run/Job、runtime、state 收据](../evidence/final-gates-20261102/e2e-two-cluster-final.receipts.public.json)
- [逐目标 Destroy Plan / Approval / Job 快照](../evidence/final-gates-20261102/d-independent-destroy.public.json)
- [真实执行命令 / exit code](../evidence/final-gates-20261102/e2e-two-cluster-final.command.public.json)、[阶段日志](../evidence/final-gates-20261102/e2e-two-cluster-final.selected.log)
- [首个前置失败恢复](../evidence/final-gates-20261102/d-initial-recovery.public.json)、[第二次前置失败清理](../evidence/final-gates-20261102/d-admission-timing-cleanup.public.json)
- [实际 Runner Pod imageID](../evidence/final-gates-20261102/actual-runner-images.public.json)
- [最终 QA 资源与 B profile 清理](../evidence/final-gates-20261102/final-cleanup.public.json)

## 8. Failure / Recovery Behavior

第一次创建 B 因 Podman remote 临时文件路径不可读失败；第二次因未挂载的本轮残留卷冲突失败。保留 exit 80 原日志，只对带本轮标签且身份已确认的空闲卷清理，使用本轮 TMPDIR 后创建成功；未重启引擎或改共享网络设置。业务测试时序失败没有被改写为 PASS。

收尾的 `minikube delete` exit 0 但节点容器仍运行；实际资源检查拒绝将其当作清理 PASS。验证原记录的 container ID/QA 标签后，使用显式 remote Podman 普通 stop → rm，仅删除该测试节点，再核对并移除已无挂载/连接的测试卷和网络。没有 force removal；最终主集群 UID/Node Ready、原容器 ID、MiniStack 和原服务均正常。

## 9. Remaining Coverage

两集群共享同一物理主机和 Podman 引擎，但 CA、API、存储 namespace 与 kube-system UID 独立；这足以验证本轮逻辑隔离，不证明不同故障域/云账号隔离。运行中真实替换 cluster incarnation、跨区域网络分区、生产 IAM/多租户权限矩阵尚未验收。

## 10. Final Verdict

**PASS（上述两个独立本地 Kubernetes 集群范围）**。全套 exit code=0；普通业务清理通过，B profile 的收尾身份核对见清理收据。不提升为生产 portability/安全认证。
