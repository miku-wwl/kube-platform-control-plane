# KPCP Phase 2.5 QA — 当前验收索引

15 个用例已按 One Test Case = One Markdown 交付；本轮 **14 PASS、1 FAIL**。PASS 均受各 Case 的实际范围限定，TC03 仅验证项目原生 deterministic 草稿路径。完整 Phase 2.5 **不建议 Freeze**。

历史 [QA_VALIDATION_REPORT.md](../QA_VALIDATION_REPORT.md) 保留 2026-11-01 的 2 PASS / 12 BLOCKED / 1 PARTIAL 和原始证据。当前矩阵不覆盖其结论，也未把历史 Go E2E 提升成本轮执行。

## A. Environment

| 项目 | 本轮记录 |
|---|---|
| 源码基线 | `e40c422327aa648a89d6baabf7268cb3239310d8`；开始及交付前复核 remote/main 相同 |
| QA Run ID | `kpcp-qa-20261101-2253` |
| 实际证据时间 / 时区 | 2026-11-01T22:58:47.137000+13:00 → 2026-11-02T01:10:24.002585+13:00，Pacific/Auckland；每图及每个收据保留 UTC |
| Kubernetes Context / 身份 | `podman-minikube`，一个实际集群；kube-system UID `2c62d6a8-08b6-460d-bb1b-ac59557dc281` |
| MiniStack | `1.5.35`；仅 dummy 测试账户 `000000000000`；未访问真实 AWS |
| Runner Image | `localhost/kpcp-runner@sha256:f94aa16a0136f5414fe10f5851e6cfad89f240168f47da93eb1bbf26702cbc39`；实际 Job imageID 已核对 |
| Browser Mode | Desktop Work 内置 IAB，复用既有 Browser 2；本轮截图来自 Tab 3/4，逐图记录，不重新调查 Browser Host |
| QA Console | `127.0.0.1:5174` → 专用 QA API `8091`、同一产品前端；收尾回到原始 `5173`，API 已连接 |
| Controller / API 二进制 | 复用既有 `5445a5f` 构建，`vcs.modified=true`；至 e40c422 的产品路径 diff 为空。已保存二进制 SHA-256，不声称 clean e40c422 rebuild |
| Draft provider | 项目原生 `deterministic`，页面明确显示确定性生成；现场 Ollama/模型不可用，真实 LLM 意图理解质量 **NOT RUN** |
| Evidence Root | 新公开证据在 `evidence/TCxx/`；原始 JPEG、DOM、私有配置、日志和产物字节保留受控 `work/kpcp-qa-20261101-2253/`，不提交 |
| 所有权 / 最终清理 | 独占 namespace/class/backend/dummy Secret/source/bucket/进程均已清理；原集群、MiniStack、Podman 和原始服务保留 |

前置环境先做只读审查，再建立明确归属的临时夹具。Runner Pod 真实读取固定 Git、访问 MiniStack 并完成 `terraform init -lockfile=readonly` 后，才进入 Draft。固定模块四文件哈希、来源提交、探针和构建身份见[TC02 前置审查](evidence/TC02/environment-preflight.public.json)。未修改产品源码、未复活旧 PowerShell Harness、未扩大 RBAC、未重置共享环境。

## B. Test Matrix

Current 为实际 PNG 文件数，Historical 为对应 Case 引用数；合计 **43 张本轮 PNG + 23 张历史引用**。截图 API 返回 JPEG，原始 JPEG 保留不变；PNG 是其解码像素的无损转存，逐张比较像素及 SHA-256。没有 Mock、历史截图冒充当前或图像补绘。原 23 张只引用一次，其中三张沿用既有公开遮盖副本。

| TC | Name | Status | Screenshots (Current / Historical) | Evidence | Document |
|---|---|---|---|---|---|
| TC01 | Application Health | PASS | 2 / 3 | [结果与哈希](evidence/TC01/result.json) | [TC01](TC01_APPLICATION_HEALTH.md) |
| TC02 | Environment Class | PASS | 3 / 15 | [结果与哈希](evidence/TC02/result.json) | [TC02](TC02_ENVIRONMENT_CLASS.md) |
| TC03 | AI Draft | PASS | 2 / 2 | [结果与哈希](evidence/TC03/result.json) | [TC03](TC03_AI_DRAFT.md) |
| TC04 | Draft Isolation | PASS | 1 / 0 | [结果与哈希](evidence/TC04/result.json) | [TC04](TC04_DRAFT_ISOLATION.md) |
| TC05 | Environment Submit | PASS | 2 / 0 | [结果与哈希](evidence/TC05/result.json) | [TC05](TC05_ENVIRONMENT_SUBMIT.md) |
| TC06 | Terraform Plan | PASS | 3 / 0 | [结果与哈希](evidence/TC06/result.json) | [TC06](TC06_TERRAFORM_PLAN.md) |
| TC07 | Approval Gate | PASS | 2 / 0 | [结果与哈希](evidence/TC07/result.json) | [TC07](TC07_APPROVAL_GATE.md) |
| TC08 | Plan Topology | PASS | 2 / 0 | [结果与哈希](evidence/TC08/result.json) | [TC08](TC08_PLAN_TOPOLOGY.md) |
| TC09 | Exact Approval | PASS | 2 / 0 | [结果与哈希](evidence/TC09/result.json) | [TC09](TC09_EXACT_APPROVAL.md) |
| TC10 | Apply / Ready | PASS | 3 / 0 | [结果与哈希](evidence/TC10/result.json) | [TC10](TC10_APPLY_READY.md) |
| TC11 | Update / NoChange | PASS | 6 / 0 | [结果与哈希](evidence/TC11/result.json) | [TC11](TC11_UPDATE_NOCHANGE.md) |
| TC12 | Delete Request | PASS | 4 / 0 | [结果与哈希](evidence/TC12/result.json) | [TC12](TC12_DELETE_REQUEST.md) |
| TC13 | Destroy Approval | PASS | 2 / 0 | [结果与哈希](evidence/TC13/result.json) | [TC13](TC13_DESTROY_APPROVAL.md) |
| TC14 | Destroy / Cleanup | PASS | 6 / 0 | [结果与哈希](evidence/TC14/result.json) | [TC14](TC14_DESTROY_CLEANUP.md) |
| TC15 | Language & Errors | FAIL | 3 / 3 | [结果与哈希](evidence/TC15/result.json) | [TC15](TC15_LANGUAGE_ERRORS.md) |

TC03–TC14 属于同一条实际生命周期：Draft → 隔离核对 → Submit → Plan → 审批栅栏 → 拓扑 → exact approval → Apply/Ready → generation 2 NoChange → Delete → 独立 Destroy 审批 → 普通 finalizer 清理。初始 Plan 因会话等待超过有效期，被 Controller 自然标记 Superseded；实际审批只用于新的有效 Plan，身份见 TC06/TC09。

## C. Engineering Gate Matrix

| Gate | 对应用例与 Current Execution | Historical Evidence / Go E2E | 本轮状态与剩余范围 |
|---|---|---|---|
| A Exact Plan Approval | [TC07](TC07_APPROVAL_GATE.md)、[TC09](TC09_EXACT_APPROVAL.md)、[TC13](TC13_DESTROY_APPROVAL.md)；[Reconcile 七项绑定](evidence/TC09/exact-binding.public.json)、[独立 Destroy 绑定](evidence/TC13/exact-binding.public.json) | [A_ExactApproval](../../../e2e/lifecycle_test.go#L389)、[PlatformAPI_ExactApproval](../../../e2e/lifecycle_test.go#L503)，历史已实现负例 PASS | 当前正向、审批前零执行、单次准入及独立 Destroy PASS；本轮未重跑全部错误 UID/digest/report/context/input/过期注入矩阵 |
| B Immutable Artifact Evidence | [TC06](TC06_TERRAFORM_PLAN.md)、[TC08](TC08_PLAN_TOPOLOGY.md)、[TC10](TC10_APPLY_READY.md)、[TC11](TC11_UPDATE_NOCHANGE.md)、[TC14](TC14_DESTROY_CLEANUP.md)；四份 Plan 的五类产物均校验；[Apply 身份](evidence/TC10/ready-proof.public.json)、[Destroy 终态](evidence/TC14/destroy-cleanup-proof.public.json) | [B_ImmutableEvidence](../../../e2e/lifecycle_test.go#L418)，历史产物篡改/缺失拒绝 PASS；[历史摘要](../evidence/public/engineering-evidence-summary.json) | 当前摘要、UID、saved plan、retained source 正向 PASS；本轮没有再篡改对象或注入缺失产物 |
| C Controller Restart Recovery | 关联 [TC06](TC06_TERRAFORM_PLAN.md)、[TC10](TC10_APPLY_READY.md)；本轮不执行重启故障测试 | [C_RestartRecovery](../../../e2e/lifecycle_test.go#L557)，历史四次专用 Operator 进程重启及唯一 Apply PASS；[执行收据](../evidence/qa-20261101/e2e-result-public.json) | **Current NOT RUN**；历史支持限定进程重启路径，不证明多副本 Deployment failover。收尾停止临时进程不是重启恢复验收 |
| D Multi-target Isolation | 当前仅一个实际集群；TC10/14 的对象归属不替代第二独立集群 | [D_MultiTarget](../../../e2e/lifecycle_test.go#L808) 历史 SKIP；SingleClusterConcurrency 历史 PASS；[实际配置](../evidence/public/execution-config-public.json) | **独立双集群 NOT RUN**；不能用两个 namespace、workspace 或单集群并发关闭此 Gate |
| E Fail-Closed | [TC02](TC02_ENVIRONMENT_CLASS.md)、[TC04](TC04_DRAFT_ISOLATION.md)、[TC07](TC07_APPROVAL_GATE.md)、[TC11](TC11_UPDATE_NOCHANGE.md)、[TC12](TC12_DELETE_REQUEST.md)、[TC15](TC15_LANGUAGE_ERRORS.md)；[Draft 无副作用](evidence/TC04/isolation-comparison.public.json)、[无审批零 Apply](evidence/TC07/no-apply.public.json) | [E_FailClosed](../../../e2e/lifecycle_test.go#L819) 与历史 A/B 负例；实现范围见[覆盖矩阵](../ACCEPTANCE_COVERAGE_MATRIX.md) | 当前固定版本拒绝、Draft 边界、无审批与 NoChange 保护 PASS；完整 live discovery/account/profile/incarnation/fence 矩阵仍未关闭 |
| F Terraform Destroy / Finalizer | [TC12](TC12_DELETE_REQUEST.md)、[TC13](TC13_DESTROY_APPROVAL.md)、[TC14](TC14_DESTROY_CLEANUP.md)；[审批前保留](evidence/TC13/pre-destroy-approval.public.json)、[销毁及 finalizer](evidence/TC14/destroy-cleanup-proof.public.json)、[最终清理](evidence/TC14/final-cleanup.public.json) | [F_DestroyFinalizer](../../../e2e/lifecycle_test.go#L887) 普通路径历史 PASS；另一次 scoped 503 运行见[验证报告](../VALIDATION_REPORT.md) | 当前独立审批、runtime-first、桶缺失与正常 finalizer PASS；本轮没有故障注入，不能把另一次 503 测试写成本轮执行 |

历史主运行身份：2026-11-01 20:58:15–21:11:37 NZDT、产品 `5445a5fdf169a9f12296dc44df1ba1b2d2819342`、`TestLifecycle` 799.07 秒，Plan UID `fbaecc62-a2ac-4b76-a55a-0a384cb62d95`。其 D_MultiTarget 为 SKIP，且该主运行没有 FaultFile；另一次 204.25 秒 scoped 503 运行应独立阅读。本轮只审查这些 Go 证据，没有重新运行 Go E2E。既有公开摘要中的“当前浏览器未执行生命周期”是前一轮快照，不能代入本轮。

## D. Open Defects / Blockers

| ID / 缺口 | 实际状态 | 复测入口 |
|---|---|---|
| QA-01 / P2 | **OPEN，本轮复现**：中文模式的 source.revision 服务端错误和 Backend 警告仍为英文；语言切换及输入保留通过，TC15 按完整本地化预期 FAIL | [TC15 逐步指导及截图](TC15_LANGUAGE_ERRORS.md) |
| 真实模型服务 | **NOT RUN**：deterministic 原生工作流通过，无法证明 live LLM 意图提取和质量 | [TC03 范围](TC03_AI_DRAFT.md)；需要已配置模型后按同字段复测 |
| 独立双集群 | **NOT RUN**：缺少第二个独立集群/SecondContext，单集群并发不能替代 | Gate D、[既有覆盖矩阵](../ACCEPTANCE_COVERAGE_MATRIX.md) |
| 完整安全负例 / failover | 超出当前已执行范围的 live discovery/account/fence、source/target 变更和多副本接管未关闭 | Gate A/C/E 和[架构契约](../ARCHITECTURE_CONTRACT.md) |
| OBS-01 / 展示约定待确认 | Apply 后概览的 Plan 报告/摘要显示未记录，而原 Plan 报告存在且已校验；未当作产物缺失或 Apply 失败 | [TC10 图02与说明](TC10_APPLY_READY.md) |

前一轮 Controller、Ready Class、Backend/Secret、Git 与 PodEndpoint 阻塞已通过本轮独占前置环境解除并实际执行。测试结束后这些临时夹具已清理，复测需重新准备；没有遗留待清理测试基础设施。

## E. Final Freeze Recommendation

**文档交付完整；Phase 2.5 完整产品验收 NO-GO / 不建议 Freeze。** 单集群固定模块 UI 生命周期已真实完成，但 QA-01 未关闭、独立双集群与真实模型质量未运行，扩展安全负例和多副本接管仍有覆盖缺口。14 个限定范围 PASS 不代表全量平台能力通过。

清理证据独立记录在 [TC14](TC14_DESTROY_CLEANUP.md)：云桶由已审批 Terraform Destroy 删除；35 个原始产物先归档，再删除仅本轮夹具桶；namespace 以 UID precondition 正常删除；没有 patch finalizer、重启共享容器或调用旧 Harness。共享模拟器仍健康、原 cluster UID/Node Ready 保持，原 5173 服务仍 API 已连接。

交付检查见[文档/图片验证收据](evidence/document-validation.public.json)。历史总报告、工程报告和覆盖矩阵保持原字节；本 PR 仅新增 `docs/phase2.5/qa/`。原始配置、日志、DOM、JPEG 和产物字节不在本次公开变更中。
