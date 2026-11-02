# Gate B — Immutable Artifact Evidence

## PR #3 后独立批次 — 2026-11-02：真实缺陷已修复，live 复测待完成

Batch：`kpcp-final-20261102-1815`；driver `8760807`，clean Controller `4f014c7`。实际运行 988.55 秒、exit 1；[完整脱敏收据](../evidence/final-acceptance-20261102/gate-b.public.json)。首轮 10 个新增子场景中 9 PASS、1 FAIL；失败保留，不将整个 lane 写成 PASS。

| 类别 | 首轮实际结果 | 证据边界 |
|---|---|---|
| 摘要一致的错误 Run UID / Job UID / Operation | PASS | 暂停专属 Controller；真实 Plan Job 完成后改写本轮 terminal，重新计算并核验 S3 content digest，再恢复 Controller；无 Apply/cloud mutation |
| 摘要一致的错误 Plan UID | **FAIL** | 将真实、摘要匹配的 binary Plan 放到另一个 UID 路径；terminal `planRef` 指向该对象；Controller 错误接受为 ChangesPresent。没有批准该 Plan，没有执行 Apply |
| 摘要一致的错误 Plan Digest | PASS | terminal 自身摘要正确，引用的 Plan 内容摘要与声明不一致，被拒绝 |
| 中断上传 / 缺少 terminal / 不确定写入结果 | PASS（3/3） | 真实 Runner/Pod 经专属传输注入；不产生可信成功、不 Apply；恢复产生不同 UID 的新 Plan，不冒充旧 Plan |
| 不确定写入后的相同重试 / 冲突重试 | PASS | 对实际已写入对象执行相同 bytes 的 immutable retry，再以不同 bytes 重试，后者被拒绝 |
| Source / Backend snapshot drift | PASS（2/2） | artifact bytes 与 metadata 同时改变，实际内容摘要可自洽；原 Plan 的固定 digest 仍阻止 Apply Job，云资源不存在 |

最小修复：可信完整终态的 Plan 必须引用 `runs/<当前 Run UID>/plan.binary`；Apply 必须引用冻结在其 spec 中的已批准 Plan 路径。没有放松摘要、Job、时间或审批校验，没有改动失败终态的 fail-closed 分类。新增 Plan/Apply 两个语义回归，修复前均 FAIL、修复后 Controller 单测全集 PASS；本轮 live 重跑及 Gate C/D 重验另行追加结果。

当前不能据此 Freeze：修复后的真实终态负例及受影响的 C/D 尚待重验。以下为历史批次的原始记录。

## 1. Test Objective

验证 saved Plan/source/backend/report/terminal 的不可变证据链。读取失败或摘要不符时不得重 Plan 后沿用旧审批。

## 2. Preconditions

与 Gate A 同一真实集成环境，故障仅注入 `kpcp-1793541857329652200` 测试桶中的指定对象。原始字节和损坏夹具归档本地，不公开 Secret、完整 backend/kubeconfig。

## 3. Test Matrix

| 场景 | 本轮结果 | 实际检查 |
|---|---|---|
| Plan binary tamper / missing | PASS | 原 digest 与实际 digest/缺失均记录；拒绝 Apply，无 Job/cloud mutation |
| source bundle tamper / missing | PASS | 同上；不得从当前 Git 偷换 retained source |
| backend snapshot tamper | PASS | 指定对象字节损坏，原 metadata 保留；内容校验拒绝 |
| report missing / corrupt | PASS | 未把失去报告的旧 Plan 当作可审批执行 |
| terminal corrupt | PASS | 错误摘要不能形成 trusted terminal evidence |
| credential UID/RV drift | PASS | Plan 后轮换专属 Secret 的 RV；冻结身份不符时拒绝 |
| 同内容 retry / immutable-write conflict | PASS | 真实 MiniStack integration；同字节可重试，异字节冲突拒绝 |
| saved Plan consistency | PASS | Apply/Destroy terminal 的 Plan ref/digest 与审核版本相同 |
| 正确内容摘要但伪造 terminal 身份 | 单测 PASS；live NOT RUN | Run UID、Job UID、operation、时间、exit/outcome 八项单测 |

## 4. Execution Steps

1. 为每种故障建立独立测试环境并生成有效 Plan。
2. 保存五类引用和原始摘要；仅覆盖/删除该测试 Plan 的指定对象。
3. 使用原 Plan 绑定审批，等待 `Rejected`；检查没有对应 Apply Job、cloud bucket 仍不存在。
4. 对非 expiry 的产物故障，断言同 stack 仍只有原 Plan，没有悄悄建立新 Plan。
5. 独立运行 `TestMiniStackIntegration/ImmutableArtifacts`，验证同内容重试、冲突及内容损坏拒绝；另验证 backend lock 竞争返回 412。

## 5. Expected Results

对象 metadata 不能替代实际字节校验；失败诊断和原身份保留；旧审批不能授权替代 Plan。

## 6. Actual Results

`B_ImmutableEvidence` 实际运行 **PASS（439.17 秒）**。每种损坏的 saved Plan 和 rejected Apply 收据均保留。源 bundle 的共同原摘要为 `sha256:58d0e8159d83d2042b9abd0745e6768e0bdf67ab966807f9a671070e31fddd80`，实际破坏摘要在日志逐项列出。真实 MiniStack immutable writer/lock integration exit 0。

## 7. Evidence

- [逐项 originalDigest / actualDigest / missing](../evidence/final-gates-20261102/e2e-host-current.selected.log)
- [saved Plan 与 rejected Apply UID/condition](../evidence/final-gates-20261102/e2e-host-current.receipts.public.json)
- [集成命令与 exit code](../evidence/final-gates-20261102/clean-build.public.json)
- [真实 MiniStack integration 原始测试输出](../evidence/final-gates-20261102/ministack-integration.selected.log)
- [真实 MiniStack integration](../../../../internal/artifacts/ministack_test.go)、[terminal 身份单测](../../../../internal/controller/execution_test.go)

## 8. Failure / Recovery Behavior

所有非 expiry 产物故障维持拒绝；没有创建替代 Plan 来复用旧审批。先通过正常业务删除清理，再归档测试对象。归档包含故意损坏的负例，不能声称归档内每个 metadata digest 都正确。

## 9. Remaining Coverage

本轮 live terminal 故障是字节/摘要损坏；使用“重新计算为正确摘要的假 Run/Job 身份”进行端到端攻击尚未运行。部分上传失败、版本化对象存储及跨实现兼容性不在本地 MiniStack PASS 范围。

## 10. Final Verdict

**PARTIAL**。真实 artifact 篡改/丢失/重试/冲突矩阵 PASS；语义身份伪造的 live 路径仍缺口。

## 11. 2026-11-02 Final Closure Attempt

| 本轮要求场景 | 本轮结果 | 证据 / 原因 |
|---|---|---|
| hash 一致但 Run UID 错误 | BLOCKED | 未创建 QA-owned Terraform execution |
| hash 一致但 Job UID / operation / Plan UID / Plan digest 错误 | BLOCKED | 无 active Controller/Runner 和 Pod 可达固定 source/backend；不注入伪造终态 |
| 中断 artifact upload / uncertain retry / immutable conflict | BLOCKED | 本轮没有 QA bucket 对象；MiniStack 当前 bucket list 为空 |
| source/backend snapshot 改变后复用审批 | BLOCKED | 没有本轮 live Plan 或审批 |

预检确认集群 KPCP CRD 已安装但业务对象为零；MiniStack 1.5.35 本身健康，Pod 可访问 transport 不存在。详细身份及限制见[只读基础设施预检](../evidence/deepseek-final-20261102/infrastructure-preflight.public.json)。

**本轮 Gate B 扩展场景 BLOCKED；Gate 总状态仍 PARTIAL。** Controlled tests 与已有 artifact 故障证据保留，不能代替上述 live identity 攻击。
