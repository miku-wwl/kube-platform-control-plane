# Gate A — Exact Plan Approval

## PR #3 后独立批次 — 2026-11-02：PASS（本地范围）

Batch：`kpcp-final-20261102-1815`。先完成真实 Plan → exact approval → Apply → Ready → 独立 Destroy approval → Destroy/finalizer 基线，exit 0、232.75 秒；[基线收据](../evidence/final-acceptance-20261102/baseline.public.json)。11 项专属前置检查均通过，固定源码、Pod Git/S3/STS、backend、artifact、Runner 和 Controller 的身份见[预检](../evidence/final-acceptance-20261102/preflight.public.json)。

| 变更 | 新执行与结果 | 可核对的批次 |
|---|---|---|
| Source Revision | 实际 saved Plan 后修改被 immutable admission 拒绝；正常删除未 Apply 的旧 Stack，使用真实 alternate Git commit 重建；旧 Plan UID approval 无法授权新的 Context / Plan | PASS；[final](../evidence/final-acceptance-20261102/gate-a-final.public.json) |
| Target Incarnation | 受控 desired target incarnation 改变，实际集群 kube-system UID 保持原值；Controller 建立新的 Plan，旧 approval 不产生 Apply | PASS；[initial](../evidence/final-acceptance-20261102/gate-a.public.json) 的 target 子用例 |
| Backend Identity | 同内容的新 ConfigMap 名称/UID；保留直接更新被拒绝的证据，再正常替换 Stack 身份；旧 approval 被新 Plan UID / Context 隔离 | PASS；[corrected](../evidence/final-acceptance-20261102/gate-a-corrected.public.json) 的 backend 子用例 |
| Runner Digest | 不同的真实 OCI index digest，固定同一 runner 文件系统；实际新 Plan 可运行并等待新审批，旧 approval 无效 | PASS；[final](../evidence/final-acceptance-20261102/gate-a-final.public.json)、[两个镜像身份](../evidence/final-acceptance-20261102/runner-identity.public.json) |
| Effective Inputs | 独立 Plan 后改变目标 bucket input；旧 approval 未创建 Apply Run/Job，原目标与改变后目标均无云副作用 | PASS；[initial](../evidence/final-acceptance-20261102/gate-a.public.json) 的 inputs 子用例 |
| Execution Profile | 换用新的本轮 dummy credential Secret identity；Controller 生成新 Context / Plan，旧 approval 无效 | PASS；[initial](../evidence/final-acceptance-20261102/gate-a.public.json) 的 profile 子用例 |

每项都有独立的真实 Plan UID、Plan/Report/Context/Input digests、Approval UID、Run/Job 列表、云资源不存在查询和正常清理结果。Source/Runner 最终 driver 为 `550057e`，Controller 为 clean `4f014c7`；driver-only 增量没有修改对应产品包。旧 UID 的重新创建不是直接修改 immutable 对象，也没有关闭安全断言。

历史执行分别保留：首个 nested selector 未进入新用例，只算生命周期复核，不计 Gate A（[收据](../evidence/final-acceptance-20261102/selector-miss.public.json)）；initial lane exit 1（target/inputs/profile PASS，其余因不可变准入前置失败）；corrected lane exit 1（backend PASS，source/runner 因并发 resourceVersion 冲突失败）；final lane exit 0，source/runner PASS。只补测未通过的用例，没有把整条失败 lane 改为 PASS。

两次保留的未 Apply Stack 都经 Controller 正常 finalizer 清理，先归档原始 artifact、验证没有 Apply/cloud mutation，再删除本轮 bucket/namespace。[initial 清理](../evidence/final-acceptance-20261102/gate-a-initial-cleanup.public.json)、[corrected 清理](../evidence/final-acceptance-20261102/gate-a-corrected-initial-cleanup.public.json)。最后一批 namespace、Class、业务 CR 已清空，正常 Destroy/finalizer 126.77 秒。没有人工 Finalizer patch、force delete 或非 QA 资源操作。

**Gate A：PASS，限定本地主机 Podman/Minikube/MiniStack 的上述六项真实负例。** 不代表真实集群替换、生产 IAM 或 AWS certification。以下章节是此前各批次的原始记录，不覆盖。

## 1. Test Objective

仅允许已经审核的、身份完全一致且未过期的 saved binary Plan 执行一次 Apply；Reconcile 审批不能授权 Destroy。

## 2. Preconditions

Run ID：`kpcp-gates-20261102-0223`。真实 Kubernetes、MiniStack dummy 账户、固定模块提交 `98da3fb6cad0b60c60a58169aeb282aa555dbeb8`、digest 固定 Runner。A/B 主执行使用 clean `d9f0386` Controller；后续独立 Destroy 验证见 Gate F。未调用真实 AWS。

## 3. Test Matrix

| 场景 | 本轮结果 | 证据类型 / 限定 |
|---|---|---|
| 无审批 | PASS | 实际 Plan 完成后零 Apply Run；只有一个 Plan Job；cloud bucket 不存在 |
| 错 UID、Plan digest、context、input、report digest/ref、Plan ref | PASS | 7 个错误审批同时保留；集合断言仍零 Apply/新增 Job/cloud side effect，保存每份绑定 |
| Expired / Superseded | PASS | 测试专属 Plan 的 expiry 受控变更；旧 Plan Superseded，旧审批没有授权 Apply |
| 重复审批 | PASS | API 重复请求返回相同 Approval UID，只有一个实际 Apply |
| Reconcile 审批用于 Destroy、错误 Destroy digest | PASS | 保留原 Reconcile 审批和错误 Destroy 审批，Destroy Apply 仍未创建 |
| changed source / target / backend / runner / inputs | 单测 PASS；真实故障注入 NOT RUN | `TestApprovalBindsCompleteSnapshotAndDeduplicatesPlan`，不能提升为 live E2E |
| wrong operation / NoChange / evidence missing | 单测 PASS | 完整操作类型与证据拒绝单测；NoChange 另有本轮真实 E2E PASS |

## 4. Execution Steps

1. 显式提交测试环境，等待 `ChangesPresent`，保存 Plan UID 和全部摘要。
2. 创建七份仅修改一项绑定的测试审批，等待协调；列出 Apply、Job，并 HEAD 本轮 cloud bucket。
3. 对独立 Plan fixture 注入过期时间，观察新 immutable attempt；旧审批不得复用。
4. 调用真实 API exact approve，再重复同一请求；比较 Approval UID、Apply Run 和 Job 数量。
5. 删除环境，等待 Destroy Plan；保留 Reconcile 审批并加入错误 Destroy digest，确认仍等待独立审批。

## 5. Expected Results

错误审批没有 Terraform mutation；合法审批绑定同一个 saved Plan；审批幂等；拒绝和产物身份可追溯。

## 6. Actual Results

`A_ExactApproval` 本轮 PASS，`B_ImmutableEvidence` 内过期用例 PASS；首个全套运行最终在 F 因传输夹具错误 FAIL，该全套结果保持 FAIL。后续独立 F 和双副本 C/F 运行 PASS，不能将其改写成首轮全套 PASS。

## 7. Evidence

- [A/B 执行断言、原始摘要与实际摘要](../evidence/final-gates-20261102/e2e-host-current.selected.log)
- [七份审批、零 Apply 与幂等收据](../evidence/final-gates-20261102/e2e-host-current.receipts.public.json)
- [独立 Destroy 审批收据](../evidence/final-gates-20261102/e2e-fault-corrected.receipts.public.json)
- [测试实现](../../../../e2e/lifecycle_test.go)、[准入单测](../../../../internal/controller/execution_test.go)
- 浏览器操作历史：[TC09](../TC09_EXACT_APPROVAL.md)、[TC13](../TC13_DESTROY_APPROVAL.md)，未冒充本批次新 UI 运行。

## 8. Failure / Recovery Behavior

错误的 UID/digest/ref 不建立匹配授权；产物故障不会通过静默重 Plan 继续使用旧审批。过期 Plan 允许建立新的 immutable attempt，但必须重新审核。中断 10 小时后的真实过期恢复另见 Gate F。

## 9. Remaining Coverage

在 Plan 完成后真实更换 source revision、target incarnation、backend/runner 身份的完整执行矩阵尚未运行。七份错误审批使用集合副作用断言，没有为每份审批提供独立 Controller rejection condition。不得将这些缺口计入 PASS。

## 10. Final Verdict

**PARTIAL**。已执行的 exact binding、幂等及独立 Destroy 审批 PASS；完整 live 变更矩阵未关闭。

## 11. 2026-11-02 Final Closure Attempt

| 本轮要求场景 | 本轮结果 | 证据 / 原因 |
|---|---|---|
| source revision 改变 | BLOCKED | 无固定 Pod 可达 Git source transport |
| target incarnation 改变 | BLOCKED | 本轮未创建 QA-owned Plan / target |
| backend identity 改变 | BLOCKED | 本轮没有 QA backend 或 active Controller |
| runner digest 改变 | BLOCKED | runner 镜像可在节点 cache 核对，但没有运行中的 QA Controller；不创建未经完整配置的生命周期 |
| effective input / execution profile 改变 | BLOCKED | 未创建 live Plan 与审批资源 |

只读复核发现 KPCP CRD 已安装、集群 Ready，但没有 QA namespace、Class、PlatformEnvironment、InfraStack、TerraformRun、Approval、Controller/Runner Pod 或 Job。MiniStack 为空且运行；Pod 专用 14566 transport 未监听。完整收据见[只读基础设施预检](../evidence/deepseek-final-20261102/infrastructure-preflight.public.json)。

**本轮 Gate A 仍为 PARTIAL**；未修改 immutable object、未创建 TerraformRun/Apply、未做云端变更。上述 BLOCKED 不覆盖已有真实 exact binding 结果，也不将其升级成新的 PASS。
