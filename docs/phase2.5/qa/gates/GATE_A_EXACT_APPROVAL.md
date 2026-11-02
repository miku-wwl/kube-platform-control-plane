# Gate A — Exact Plan Approval

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
