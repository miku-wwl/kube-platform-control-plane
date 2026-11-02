<!-- FINAL_ACCEPTANCE_20261102_CURRENT -->
# Phase 2.5 PR #3 后最终验收 — 2026-11-02

**Freeze：NO-GO。** 本轮已恢复真实 UI 与工程测试，补齐指定安全故障；当前 Controller 在 post-mutation failure/缺失可信终态后正确保持 `RecoveryRequired`，但没有可执行的受支持恢复路径将四条业务链按正常审批与 finalizer 机制收尾。F04 / Resource Cleanup 为 BLOCKED，没有批准例外，不能 Freeze。

适用范围仅为 **KPCP Phase 2.5 Local Podman + Minikube + MiniStack Acceptance**。Production AWS Portability **NOT VALIDATED**。

## 1. 最终验收矩阵

| Area | Result | 本轮范围 / 证据 |
|---|---|---|
| DeepSeek Live Intent | PASS（复用） | 历史 DS01–DS07 7/7、1,948 tokens，未重跑七次收费请求 |
| DeepSeek Browser UI | PASS | 本轮 IAB 两次真实请求、8 张原始 JPEG；EN/ZH、typed intent/YAML/advisory、显式 Submit、零自动资源变更 |
| Gate A | PASS | 六类上下文变化均实测拒绝旧审批；历史失败与独立复测分开保存 |
| Gate B | PASS（修复后） | 首轮发现 Plan UID 语义绑定缺陷；最小修复后 10/10 live 子场景 PASS |
| Gate C | PASS（重验） | 新 Controller 两真实副本、六生命周期阶段和跨目标 contention 接管 |
| Gate D | PASS（重验） | 两个独立 UID/CA/凭据；真实 runtime/state/并发/逐目标 Destroy |
| Gate E | PASS（保护行为） | wrong account、Git outage、foreign Job、ambiguous Apply、partial Apply 五项 live 保护通过；不声称生产 IAM |
| Gate F | PARTIAL | F01–F03 live 保护通过；**F04 正常恢复/清理 BLOCKED** |
| Clean Build | PASS | clean `99dc281`，Go/unit/race/vet/build、前端 7 tests/typecheck/build、Terraform 1.14.0、MiniStack；三个二进制 modified=false |
| CI | PENDING | 本地回归已完成；PR 最终 checks 发布后核验，不把默认 opt-in SKIP 记作 live PASS |
| Resource Cleanup | BLOCKED（部分） | 正常 lane、临时第二集群完成清理；四条不确定/部分变更业务链及其证据桶保留 |
| Evidence Integrity | PASS | 真实截图解码与 SHA、raw receipt/log SHA、实际 Controller imageID 与 binary 来源均核验 |

## 2. 来源与执行边界

- Batch：`kpcp-final-20261102-1815`，NZDT；起始及最终 fetch 的 main 为 `21231fc792b9f461c42ef335d99fd79b211862f8`（PR #3 merge）。
- 独立 branch：`qa/phase25-final-acceptance`，worktree `D:/workshop/nov/kpcp-final-acceptance`；原 main 工作树两份运行日志保留，不提交、不还原、不覆盖。
- DeepSeek UI clean API：`f8c307fd6555ff118249f667dd2833d41f650e76`、provider `deepseek`，独立 QA `5174 → 8091`；原 `5173/8090` 保留。生产 API 未暴露本轮 served model/token usage，配置模型为 `deepseek-flash`，没有推测 usage。
- live 修复后 Controller：clean `7cdad59ef8035391cafc1e4d8727ebe715fd9f32`；B driver 同 SHA、C/D driver `99dc281`、E/F driver `f4e067d5b5a51027b5d3a887c7ad53d796e1e7e9`。这些 driver 的相关产品/E2E 源码与 Controller 构建源码一致，后续报告提交不冒充重新执行。
- [Controller OCI/binary/source 证明](evidence/final-acceptance-20261102/controller-bound-image.public.json)、[clean regression 收据](evidence/final-acceptance-20261102/clean-regression.public.json)。镜像内实际二进制 hash 与 clean Linux binary 相同；Go VCS metadata 为 modified=false。

## 3. UI 验收与最小修复

本聊天新建 IAB tab，实际完成 example.com 标题/AX smoke、原 KPCP 读取和真实截图保存，再操作独立 DeepSeek QA UI。没有使用 native Computer Use、独立 Playwright 或 API 代替 UI 操作。

本轮只增加 **2 次**真实收费调用。第一次发现显式 `Do not enable Valkey` 被共享短语匹配器反向解释；最小补齐否定短语后，第二次真实 UI 返回 Valkey Disabled、2 nodes、typed intent/YAML。真实 API 停止产生的 502 最初未本地化，增加 `APIUnavailable` 分类及 EN/ZH 文案，保留技术详情。两项 UI 修复均有失败/修复截图与非计费回归。

[TC03 独立批次](TC03_AI_DRAFT.md#11-pr-3-后独立验收批次--2026-11-02)、[DeepSeek live/UI](DEEPSEEK_LIVE_VALIDATION.md)、[8 图及 No-Submit 收据](evidence/final-acceptance-20261102/deepseek-ui.public.json)。没有点击 Submit；Draft 前后 KPCP 业务对象为零，唯一额外 Job 是明确归属的连通性 probe。

## 4. 工程 Gate 与失败保留

Gate A 的不可变字段更新被 admission 拒绝，测试改用正常删除未 Apply 对象、同名新 UID/上下文与旧审批，未降低断言；两次夹具失败及其正常清理留存，六项各自 PASS 才合并结果。Gate B 首轮 9/10，通过真实摘要一致的外来 Plan 路径发现缺陷；只增加 Plan 当前 UID 路径及 Apply 冻结 PlanRef 检查，两个语义单测先红后绿，live 重跑 10/10 PASS。

因 Controller 终态身份校验改变，本轮重新建立独立第二集群并重验 C/D。新集群 UID `32d75cbe-8c49-439c-885e-6b36329da3d8` 与主集群 `2c62d6a8-08b6-460d-bb1b-ac59557dc281` 不同；CA、客户端证书也不同，TLS 验证保留。实际 C/D/F lane 566.40 秒、exit 0；第二集群 container/volume/network 已确认消失，主集群身份不变。

E/F 使用真实 saved Plan、exact approval、Runner Job 与 MiniStack bucket。E04/F02 的 Terraform mutation 真实完成，但 terminal PUT 被精确 403 拒绝，Controller 为 Indeterminate；E05/F01 在两个有依赖的资源中完成一项 mutation 后失败。没有把 Plan failure 当作 Apply/Destroy Apply failure。Controller 重启后 Run/Job 身份保留，没有重复 mutation，两个业务 finalizer 保留。

E/F lane 的 Go 命令 exit 0 只表明七个执行保护子测试 PASS；`F04_RecoveryAndCleanup` 是明确 **SKIP / BLOCKED**，不属于 PASS。详见 [Gate A](gates/GATE_A_EXACT_APPROVAL.md)、[Gate B](gates/GATE_B_IMMUTABLE_EVIDENCE.md)、[Gate C](gates/GATE_C_CONTROLLER_RECOVERY.md)、[Gate D](gates/GATE_D_MULTI_CLUSTER.md)、[Gate E](gates/GATE_E_FAIL_CLOSED.md)、[Gate F](gates/GATE_F_DESTROY_FINALIZER.md)及[最终 E/F 收据](evidence/final-acceptance-20261102/gate-ef.public.json)。历史失败、旧 Browser BLOCKED 与旧 Gate PARTIAL 保留在各文档原批次。

## 5. 清理与恢复阻塞

保留 namespace：`kpcp-1793611893090112000`，UID `4797a13a-e3ec-4773-a6f8-ad867edad61a`；artifact bucket 同名。四条业务链的完整 Environment/Stack/Plan/Apply/Job UID、审批绑定、digest、finalizer、cloud 状态与本地归档摘要见[保留资源清单](evidence/final-acceptance-20261102/retained-resources.public.json)。原始 state、backend、source bundles、binary Plans 与 Pod/Controller 日志归档于受控本地 work 目录，公开仓库仅有脱敏投影与 hash。

当前 `InfraStack.reconcileDestroy` 对任何 Indeterminate Apply 或可能发生 mutation 的 Failed Apply 返回 RecoveryRequired；现有 API 没有安全清除此阻塞并产生 fresh-observation recovery Plan 的操作。不能用补写 stdout 为可信 terminal、删历史 Run、手工 patch status/finalizer 或强删对象制造恢复成功。

人工恢复步骤与风险：先保持执行 Controller 停止，核对主集群 incarnation 和四条链的精确 UID；只读比较 retained backend/state 的 lineage/serial、实际 bucket 与 Job/terminal/plan 证据。需要另行实现并审查受支持的 fresh observation → 新不可变 Plan → 独立审批 → 正常 Destroy/finalizer 收尾流程。错误 state、错误目标或把缺失 terminal 推断为成功会造成漏删或重复 mutation；在该路径验证通过前保留原对象与证据，不宣称 CLEANUP PASS。

临时第二集群完整清理见[独立清理收据](evidence/final-acceptance-20261102/secondary-cleanup.public.json)。本轮其余支撑服务和原服务健康的交付状态见[最终资源核对](evidence/final-acceptance-20261102/final-cleanup.public.json)。未修改全局防火墙、DNS、共享路由；未重启或删除共享 Minikube/MiniStack；未访问真实 AWS。

## 6. Clean regression / 证据完整性

Go unit/race/vet/build、前端 npm ci / 7 tests / typecheck / build、Terraform 1.14.0 的全仓 fmt 与 basic/partial overlay init/validate、真实 MiniStack STS/immutable writer/backend lock、Markdown links、Gitleaks 8.30.1 与 git whitespace 全部实际执行。默认 E2E 与收费 DeepSeek opt-in 的 SKIP 日志单独保存，不计为 live PASS。[逐命令收据](evidence/final-acceptance-20261102/clean-regression.public.json)、[Go tests](evidence/final-acceptance-20261102/clean-logs/clean-go-unit.log)、[race](evidence/final-acceptance-20261102/clean-logs/clean-go-race.log)、[前端 tests](evidence/final-acceptance-20261102/clean-logs/clean-frontend-test.log)、[Terraform](evidence/final-acceptance-20261102/clean-logs/clean-terraform.log)。

[完整性核验](evidence/final-acceptance-20261102/evidence-integrity.public.json)：8 张原始 IAB JPEG 解码、文件长度和 SHA 匹配；live receipts/raw Controller/Pod log hash 匹配；实际 Pod Controller imageID 与 OCI index、child manifest、image 内 executable 的 hash 绑定。公开文本仅脱敏和规范换行，原始敏感证据不提交。真实 AWS/IAM、生产故障域、SLA 与生产可移植性不在验收范围。

## 7. 交付

Feature branch 提交并创建 PR，**不合并 main**。GitHub Actions 发布后核验，最终 PR URL、head SHA 与最新 CI run 在交付收据中明确记录。NO-GO 来自 F04 受支持恢复与正常资源清理的实证阻塞，不被绿色单测/CI 或保护性拒绝覆盖。

以下是前序批次的原文，其时间、身份、FAIL/BLOCKED/PARTIAL 不覆盖本轮结果。

<!-- FINAL_ACCEPTANCE_20261102_HISTORY -->
# Phase 2.5 Final Freeze Decision — 2026-11-02

## 1. Executive Summary

本轮在隔离分支增加 DeepSeek Draft provider、严格结构验证、本地化错误、provider contract tests 和受限真实推理矩阵。真实 DeepSeek 语义请求 7/7 通过；浏览器控制不可用，故本轮没有 UI 操作或截图。Kubernetes / MiniStack 当前资源可读且健康，但本轮新增 Gate A/B/E/F 故障矩阵缺少可用的 Pod MiniStack 与固定 Git source 传输，未创建测试资源。

**Final Freeze：NO-GO。** 六 Gate 强制 live 安全缺口尚未关闭，没有获批例外。DeepSeek Draft 局部验收与完整 Phase 2.5 Freeze 分开判定。

## 2. Git / Source

- Repository：`miku-wwl/kube-platform-control-plane`
- Base：本轮起始远端 main `4ec0ecc11086d8eb982b5b9e2169bec0849adb3f`。
- 工作分支：`qa/phase25-deepseek-final-closure`，位于独立 worktree。
- DeepSeek 产品实现提交：`fcc804874a65812563127a9a58c98ed710a5a1e9`；干净 checkout 回归对应此源码 SHA。
- PR head 会包含后续报告、证据与 CI 工作流提交；最终分支 head 与 PR URL 在交付摘要中列出；不合并 main。
- 独立工作树和根 main 工作树的预存改动均未覆盖。

## 3. DeepSeek Provider / Acceptance

| 层次 | 结果 | 限定 |
|---|---|---|
| DeepSeek API | PASS | 7 个真实串行 API 请求；实际 served model 为 `deepseek-flash`；总计 1,948 tokens |
| JSON / semantic Draft | PASS | DS01–DS07 通过；DS06 使用生产 Class 校验器与内存 Class，非 live Kubernetes E2E |
| Contract / error safety | PASS | Controlled Go HTTP fixtures；未泄露 API key 或上游正文；没有自动 fallback |
| Draft / Submit boundary | PASS（contract） | fake-client 验证零 KPCP/Job 创建；真实调用只进入生成器 |
| Browser UI | BLOCKED | 内置 Browser 的 `getTab` 和一次受控 reconnect/retry 均返回 `browser.user is unavailable` |
| Screenshot | BLOCKED | 本轮未生成真实 Browser capture；没有用历史或 Mock 图替代 |

详细逐 Case 结果见 [DeepSeek Live Validation](DEEPSEEK_LIVE_VALIDATION.md)。密钥只检查本机环境变量存在性，未读值、未输出、未写入仓库或 CI。

## 4. Gate A–F Matrix

| Gate | 本轮判定 | 依据 / 未关闭项 |
|---|---|---|
| A — Exact Plan Approval | PARTIAL；本轮扩展项 BLOCKED | 历史七项绑定、过期、幂等和独立 Destroy evidence 保留；本轮 source/target/backend/runner/input/profile 变更矩阵未运行 |
| B — Immutable Evidence | PARTIAL；本轮扩展项 BLOCKED | 历史 Plan/source/backend/report/terminal 损坏和 immutable write evidence 保留；伪造一致 hash 但错误 Run/Job/operation 身份、部分上传 live 场景未运行 |
| C — Controller Recovery | PASS（历史限定范围） | 保留 2026-11-02 双副本、多阶段 failover 的独立证据；本轮未重跑 |
| D — Multi-target Isolation | PASS（历史限定范围） | 保留 2026-11-02 两个实际独立集群身份与逐目标证据；本轮未重跑，不升级为多主机或生产云保证 |
| E — Fail-Closed | PARTIAL；本轮扩展项 BLOCKED | 历史 invalid source/endpoint、启动拒绝、scoped 503 保留；wrong account、Git outage、foreign Job、ambiguous/partial Apply live 场景未运行 |
| F — Destroy / Finalizer | PARTIAL；本轮扩展项 BLOCKED | 历史独立审批、Destroy Plan failure、正常 Destroy/finalizer 与 failover 保留；部分 Destroy Apply failure、缺失终态与 ambiguous restart live 场景未运行 |

Gate C/D 的 PASS 是此前已审阅的 run 与限定范围，不是本轮重跑或通用生产证明。四个尚未闭环 Gate 的细节见对应单 Gate 文档与本轮 evidence JSON。

## 5. Fixed Defects / Safety Controls

- 新增最小 `DraftGenerator` DeepSeek 实现，保留 Ollama 与 deterministic provider。
- 限制官方 HTTPS origin、模型白名单、无重定向、无代理、超时、请求体大小、单 choice、JSON mode、非 thinking、无 tool call。
- 仅接受指定的六个 typed intent 字段；重复键、缺失/额外/null/无效字段、尾随文本、截断、空内容及 explicit field drift 均 fail closed。
- Namespace/Class 来自服务端信任输入；用户显式字段优先；没有 Submit/approval/Apply/Destroy 能力。
- 错误码本地化，API 不回显 credential 或上游错误正文；后端日志保留安全分类和 request ID（若有）。
- 新增 controlled HTTP tests 与可 opt-in 的串行真实 DeepSeek tests；默认测试不调用收费 API。

## 6. Remaining Defects and Test Gaps

- Browser host API 不可控制既有 Tab：`browser.user is unavailable`；DeepSeek UI、EN/ZH UI、PNG 截图均 BLOCKED。
- 现有共享 API 健康响应报告 `aiProvider=ollama`，不是此分支 DeepSeek build；没有用旧 UI 冒充验收。
- 当前 Kubernetes 没有 KPCP Controller/Runner Pod、QA namespace 或业务对象；集群已有 CRD且 Ready。
- MiniStack 1.5.35 运行且 S3 bucket list 为空；节点内 Runner digest 可核对，但当前没有 Pod 可达的 MiniStack 传输端口或固定 Git source 服务。
- Gate A/B/E/F 指定的 live 故障矩阵仍未关闭。具体条目保持 PARTIAL/BLOCKED，不以单测代替 live 场景。
- 真实 AWS、IAM/AssumeRole、外部故障域与生产安全认证未执行。

## 7. Clean Build / Regression

以下命令在独立 detached clean checkout `fcc804874a65812563127a9a58c98ed710a5a1e9` 执行，开始/结束 `git status` 均 clean；API 二进制中 `vcs.revision` 匹配且 `vcs.modified=false`。默认 CI 的 E2E/DeepSeek/MiniStack opt-in 跳过不是 live 集成 PASS。Terraform init/validate 是固定 fixture 的静态验证，不代表 Plan/Apply。完整命令、退出码、版本、摘要及公开日志见 [clean-build 收据](evidence/deepseek-final-20261102/clean-build.public.json)。

| 检查 | 实际结果 |
|---|---|
| Go test / race / vet / build | PASS；Go 1.27.2；各命令 exit 0 |
| Frontend `npm ci`, tests, typecheck, build | PASS；Node 26.5.0 / npm 12.0.1；5/5 Node tests |
| Terraform fmt / init / validate | PASS；固定 Runner digest 使用 Terraform 1.14.0 / AWS provider 5.100.0；host Terraform 1.15.5 被 fixture 约束拒绝后未改约束 |
| Markdown local links / whitespace / secret scan | PASS；519 links、41 Markdown；Gitleaks 8.30.1；1 条历史 binary SHA 误报使用精确 fingerprint 忽略 |
| GitHub Actions PR checks | Run `41386849296`（head `d042c5a`）及报告提交后的 Run `41387011556`（head `5897ae6`）：Go tests/race/vet/build、前端、Terraform fixture、Markdown/whitespace、Secret scan 与 GitGuardian 全部 PASS；首轮 `41386332271` 曾因两份 `*.log` 证据未入 Git 造成链接失败，修复提交 `d042c5a` 已纳入日志；机器收据更新后会再触发 PR 检查 |
| Clean source `vcs.modified=false` | PASS；API build metadata 的 `vcs.modified=false`，revision `fcc8048…` |
| Go Kubernetes lifecycle E2E | **NOT RUN / SKIP**；缺少显式 `KPCP_E2E_CONFIG`，日志证明未执行 |

全仓 Gitleaks 初扫命中历史公开收据第 6 行的 `APISHA256` 字段；只读校验表明该值为 64 位十六进制二进制 SHA-256，不是 credential。增加单条精确 finding fingerprint 忽略项 `.gitleaksignore`，不改动历史收据；再执行全仓扫描与 PR CI 确认。

## 8. Screenshots and Evidence Validation

本轮没有真实 Browser capture，也没有新增 UI PNG。真实 API 语义日志、脱敏基础设施快照、clean build 日志及其 SHA-256 实际保存于 [deepseek-final-20261102 evidence](evidence/deepseek-final-20261102/)，总索引为 [clean-build 收据](evidence/deepseek-final-20261102/clean-build.public.json)。历史截图继续留在各自历史批次，不引用为本轮截图。

原始回归日志：[Go tests](evidence/deepseek-final-20261102/logs/go-test.log)、[Go race](evidence/deepseek-final-20261102/logs/go-race.log)、[API build metadata](evidence/deepseek-final-20261102/logs/build-info.public.log)、[E2E opt-in skip](evidence/deepseek-final-20261102/logs/e2e-opt-in-skip.log)、[Frontend tests](evidence/deepseek-final-20261102/logs/npm-test.log)、[Frontend build](evidence/deepseek-final-20261102/logs/npm-build.log)、[Terraform Runner](evidence/deepseek-final-20261102/logs/terraform-runner.log)、[Markdown links](evidence/deepseek-final-20261102/logs/markdown-links.log)、[Gitleaks](evidence/deepseek-final-20261102/logs/gitleaks-dir.log)。

## 9. Infrastructure / Portability

只读预检看到一个 Ready 的 `podman-minikube` Kubernetes 集群，kube-system UID `2c62d6a8-08b6-460d-bb1b-ac59557dc281`；MiniStack container ID `01e6fc9c0ccde322bed165fa4e74dcd0184f7c337883ec4fb90986e317f52473`，image revision `ae9285c9b50ef2232a9d2c2c92e9a5efa93cead2`，version 1.5.35。验证仅涉及本机 AWS-compatible emulator；没有接触真实 AWS，也没有重启 Podman、Minikube 或 MiniStack。

本轮没有迁移或替换基础设施。MiniStack 本地兼容性不等于生产 AWS portability。

## 10. Security Residual Risks

- DeepSeek 是云 API；用户需求文本发送至官方 API，调用会产生账户使用量。语义验收只覆盖七条短请求。
- Prompt injection 测试覆盖结构化 Draft intent，不是模型红队或通用 LLM 安全认证。
- UI 与真实 Kubernetes Submit/Plan/Apply/Destroy 未通过本轮浏览器或新 lifecycle run 验收。
- 不提供真实 AWS/IAM、跨云、身份隔离或生产 SLA 保证。

## 11. Cleanup

本轮七次模型调用没有 Kubernetes client 或资源创建路径；Fake-client contract tests 仅使用内存对象。复核时 KPCP namespace、业务 CR 和 Job 均为 0，MiniStack buckets 为空。没有新测试资源需要清理；既有 shared cluster/Podman/MiniStack 均未重启或删除。

## 12. Final Verdict

**NO-GO。** DeepSeek 真实 Draft intent 子集为 PASS；完整 UI acceptance 被 Browser 控制阻塞；A/B/E/F 的指定 live safety cases 尚未执行。没有可批准的范围例外，不能以 API 语义通过、历史截图、默认 CI 或历史 C/D 结果替代完整 Freeze 门槛。
