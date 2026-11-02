# Gate F — Terraform Destroy / Finalizer

## PR #3 后独立批次 — 2026-11-02：F01–F03 PASS，F04 BLOCKED

Batch `kpcp-final-20261102-1815`；clean Controller `7cdad59`、driver `f4e067d`，相关源码一致。新增故障在真实 saved Destroy Plan 和独立 exact approval 后注入，不以 Destroy Plan failure 代替 Destroy Apply failure。

| Case | 真实执行与检查 | 本轮结果 |
|---|---|---|
| F01 Partial Destroy Apply | 环境先真实 Ready、两个 bucket 都存在；Destroy Apply 删除依赖资源后，对最后一个 bucket DELETE 注入精确 403；Terraform 非零退出、mutationMayHaveOccurred=true，一项资源仍在、另一项确实消失；Controller Indeterminate，Environment/Stack 两个 finalizer 保留，无重复 Destroy Apply | PASS（保护），189.37 秒 |
| F02 Missing Terminal | Terraform Destroy 真正 exit 0、QA bucket 消失；仅 terminal PUT 被 403 拒绝，对象实际不存在；Controller 未信任 stdout 成功、保持 Indeterminate/EvidenceCaptured=false、两个 finalizer 保留 | PASS（保护），与 F03 共 197.99 秒 |
| F03 Ambiguous Restart | 重启专属 Controller，核对同 Lease UID、新 holder；原 Destroy Run/Job UID 保留，Destroy Apply 仍恰一次，RecoveryRequired 和两个 finalizer 未释放 | PASS（保护） |
| F04 Normal Recovery/Cleanup | 四条 post-mutation/缺失可信终态业务链已正常发起删除，当前实现持续 RecoveryRequired；没有可执行的受支持 fresh-observation recovery/新审批路径。没有补写不可信 terminal、删历史 Run、patch status/finalizer 或 force delete | **BLOCKED / SKIP**，不计 PASS |

同 lane 的普通主环境仍通过独立 Destroy 审批、真实 Destroy 和正常 finalizer 清理（114.92 秒）；这不能替代上述四条故障链的清理。整个 E/F Go 命令 exit 0、1006.39 秒，只表示执行的保护 assertions 通过，F04 SKIP 不会提升为完整 Gate F PASS。

[完整 E/F 收据](../evidence/final-acceptance-20261102/gate-ef.public.json)、[真实输出](../evidence/final-acceptance-20261102/gate-ef.selected.public.txt)、[保留 Environment/Class/Stack/Run/Job/Plan 身份与 cloud/归档 SHA](../evidence/final-acceptance-20261102/retained-resources.public.json)。保留 namespace `kpcp-1793611893090112000`；准确 UID 见机器收据，没有对它做强制清理。

**Gate F = PARTIAL；Freeze = NO-GO。** 人工恢复先只读核对 retained state 的 lineage/serial、实际 bucket、完整 source/backend/Plan/Job 身份；需要受支持且审查过的 fresh observation → 新不可变 Plan → 独立 Destroy approval → 正常 finalizer 流程。把缺失 terminal 推断为成功或用错 state/目标可能导致漏删或重复 mutation。见[最终 Freeze 与恢复说明](../PHASE25_FINAL_FREEZE.md)。下方历史记录保留。

## 1. Test Objective

删除必须使用 retained convergence/source、独立 exact Destroy approval 和 saved binary Destroy Plan；失败时持有 finalizer，成功后只清理本轮资源。

## 2. Preconditions

真实 MiniStack S3、固定 Runner、明确归属 namespace/bucket/sentinel。Destroy 故障通过本轮专属 artifact transport 开关注入。未调用真实 AWS，没有手工移除任何业务 finalizer。

## 3. Test Matrix

| 场景 | 本轮结果 | 范围 |
|---|---|---|
| 独立 Destroy approval | PASS | Reconcile 审批和错误 Destroy digest 均不能准入 |
| 临时 artifact 503 | PASS | Controller 正常读取证据；Pod 所访问的精确 QA bucket 返回 503 |
| Destroy Job failure | PASS，限定 Destroy **Plan** Job | Job UID `c36cb291-3969-403d-b72a-150382e44c6a`；finalizer/原 cloud 仍保留 |
| 恢复、新 immutable Plan、重新审批 | PASS | 故障开关撤销；审核新 Plan，而非复用失效审批 |
| Pending / Active Destroy 进程重启 | PASS | 原 Run/Job/Plan terminal identity 一致 |
| Pending / Active Destroy 两副本 failover | PASS | 独立 Gate C lane，真实 Leader Pod 退出 |
| unrelated sentinel / 正常 cleanup | PASS | sentinel 不变，cloud 404，业务对象消失，正常删除 namespace/桶 |
| 部分执行后的 Destroy Apply 失败/不确定终态 | 单测 PASS；live NOT RUN | Plan Job 失败不能替代该场景 |

## 4. Execution Steps

1. 删除 UID 匹配的环境；先清理所拥有的 runtime，保留与 Terraform 相关的 finalizer。
2. 本轮代理对精确 artifact bucket 返回 503，等待实际 Destroy Plan Job failed；核对 cloud 仍在、零额外 Apply。
3. 撤销故障，等待新的有效 Destroy Plan；核对错误审批不能授权。
4. 重启专属 Controller/退出本轮 Leader，确认仍 Pending Approval。
5. 对新 saved Plan 明确审批；在 active Destroy 时再次重启/接管。
6. 核对 terminal Run UID/Job UID、Plan ref/digest、exit 0、artifactsReady；读取 cloud 404 和 sentinel。
7. 归档测试产物后正常删除 Class/namespace/桶，不 patch finalizer。

## 5. Expected Results

缺少审批、存储不可用和不确定 mutation 时不得释放 finalizer；恢复只执行被审批的 saved Plan；非归属资源不变。

## 6. Actual Results

首轮全套 F 因 host/Pod 故障范围不当 FAIL，未把它改成 PASS。正确传输 lane **PASS（总计 255.98 秒，F 146.03 秒）**。Destroy Apply Run UID `5daadbb1-3b67-4641-938c-5f4fe4149811`，Job UID `624f2050-a06a-44a0-babe-fc5523d2b939`，终态 Succeeded/exit 0。

配额中断后，首轮旧 Destroy Plan 已真实过期。Controller 自然 Supersede 并生成新 Plan；在内置浏览器查看 delete 拓扑并点击独立 exact approval 后正常删除。该人工恢复 **PASS**，原失败执行仍是 FAIL。152 个对象归档后才删除专属证据桶。

## 7. Evidence

- [失败 Job / 恢复 / 命令结果](../evidence/final-gates-20261102/e2e-fault-corrected.selected.log)
- [独立审批、active Run/Job 和 terminal 收据](../evidence/final-gates-20261102/e2e-fault-corrected.receipts.public.json)
- [中断后真实恢复](../evidence/final-gates-20261102/interrupted-recovery-result.public.json)、[正常夹具清理](../evidence/final-gates-20261102/interrupted-fixture-cleanup.public.json)
- [多副本 Destroy failover](GATE_C_CONTROLLER_RECOVERY.md)

![当前中断恢复：审核新 Destroy Plan 与 delete 拓扑](../screenshots/final-gates-20261102/interrupted-recovery-destroy-review.png)

操作：检查 Plan UID、digest、有效期与 delete 1；点击独立审批按钮。截图保留审核上下文，确切审批及执行身份以收据为准。

![实际点击独立审批后的状态](../screenshots/final-gates-20261102/interrupted-recovery-destroy-approved.png)

![实际资源清理后的页面](../screenshots/final-gates-20261102/interrupted-recovery-environment-removed.png)

## 8. Failure / Recovery Behavior

首轮 Controller pre-Job 503 保持 finalizer。临时故障恢复后新 Plan 需要新审批；过期恢复同样没有绕过人工门禁。两次 D 前置测试时序失败也按“从未准入 Apply”的正常业务路径清理，未强制 finalizer。

## 9. Remaining Coverage

没有真实注入 Terraform Destroy Apply 已部分执行后失败、terminal evidence 丢失后恢复的完整矩阵。不能将成功 Destroy 或 Destroy Plan failure 宣称为这些负例 PASS。

## 10. Final Verdict

**PARTIAL**。独立审批、503/Plan failure、进程与 Leader 恢复、普通 finalizer 清理 PASS；不确定/部分 Destroy Apply 的 live 负例未关闭。

## 11. 2026-11-02 Final Closure Attempt

| 本轮要求场景 | 本轮结果 | 证据 / 原因 |
|---|---|---|
| Destroy Apply 部分执行后失败 | BLOCKED | 本轮没有独立 QA environment、Runner/backend transport 或可注入的专属 cloud resource |
| Destroy 已执行但缺 terminal evidence | BLOCKED | 不在共享对象上移除/伪造终态；没有 QA-owned Run/Job |
| Ambiguous Destroy 时重启 dedicated Controller | BLOCKED | 当前没有 dedicated Controller；不能用共享服务重启代替隔离恢复测试 |
| Cleanup | PASS（只读核对） | 当前 KPCP business objects/Jobs 均为零；MiniStack bucket list 为空；没有本轮 QA mutation 要清理 |

本轮未创建/删除任何 Kubernetes 或云模拟资源，也未 patch finalizer。共享 Podman、Minikube、MiniStack 未重启。详见[只读预检收据](../evidence/deepseek-final-20261102/infrastructure-preflight.public.json)。

**本轮 Destroy 故障场景 BLOCKED；Gate F 仍 PARTIAL。** Cleanup 的 PASS 仅表示复核时无需回收本轮未创建的业务资源，不代表重新运行了完整 Destroy 生命周期。
