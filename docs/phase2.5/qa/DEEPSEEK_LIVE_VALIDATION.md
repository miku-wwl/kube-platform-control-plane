# DeepSeek Live Validation — 2026-11-02

## 本轮范围与来源

- Run ID：`kpcp-deepseek-final-20261102-1644`；API 请求实际执行约 16:35 NZDT。
- Provider：DeepSeek Chat Completions；服务端实际返回模型 `deepseek-flash`。
- API Key：本机进程环境变量存在性已确认；没有读取或输出密钥值。
- 请求数：7 次，串行，无重试；未设置自动充值。
- Token：prompt 1,678、completion 270、总计 1,948。Provider 计时合计 6,673 ms；服务端 request ID 未返回。
- 测试源码基于 `4ec0ecc11086d8eb982b5b9e2169bec0849adb3f` 的未提交工作树。DS01 在仅调整 usage observer 记录时序前运行；DS02–DS07 在该调整后运行。该调整不改变请求、模型输出解码或策略判定。最终提交后的非计费回归另见 Freeze 报告。
- 每次请求调用真实生成器；测试没有执行 Submit、Terraform、Kubernetes API、Apply 或 Destroy。

## 语义矩阵

| Case | Real API | JSON Valid | Semantic Correct | Policy Safe | Result |
|---|---|---|---|---|---|
| DS01 英文小型开发环境 | PASS | PASS | PASS：1 节点、Valkey 关闭 | PASS：仅六字段 advisory intent | PASS |
| DS02 中文两节点环境 | PASS | PASS | PASS：2 节点、Valkey 关闭 | PASS | PASS |
| DS03 显式字段约束 | PASS | PASS | PASS：名称、区域和节点数保持原值 | PASS：修改显式值会拒绝，不提交 | PASS |
| DS04 Redis/Valkey 意图 | PASS | PASS | PASS：启用，1 shard / 1 replica | PASS：仅用户明确请求时启用 | PASS |
| DS05 含糊请求 | PASS | PASS | PASS：保守 1 节点、无 Valkey | PASS：未增加能力 | PASS |
| DS06 超容量 | PASS | PASS | PASS：模型返回 100 节点用于负例 | PASS：真实生产请求校验器以 Class 最大值拒绝 | PASS |
| DS07 Prompt injection | PASS | PASS | PASS：显式安全字段未改变 | PASS：未接受 target、凭据、HCL、审批或执行权限字段 | PASS |

所有上述 JSON 均通过严格六字段解析；任何多余、重复、缺失、null、尾随内容、工具调用、截断或非法类型均在本地拒绝。DS06 使用测试内存中的 Ready Class 调用生产请求校验器，不是 live Kubernetes Class 证据。

## 其他安全回归

| Case | 证据方式 | 结果 | 限定 |
|---|---|---|---|
| DS08 非法 JSON / schema | Go controlled HTTP response fixtures | PASS | 非真实模型响应 |
| DS09 缺字段、额外字段、重复键、null | Go controlled HTTP response fixtures | PASS | 非真实模型响应 |
| DS10 timeout、取消、限流、服务端错误、TLS/DNS/网络错误 | Go controlled transport fixtures | PASS | 不向真实 API 触发限流或故障 |
| DS11 Draft / Submit 分离 | fake-client API contract + direct generator path | PASS（代码合同） | 不是本轮 live cluster 前后资源审计 |

Go API fixture 断言 Draft 不会创建 PlatformEnvironment、InfraStack、TerraformRun、ChangeApproval 或 Job。七次真实推理直接调用 `DraftGenerator`，未持有 Kubernetes client。当前集群在请求后复核未发现 KPCP 业务对象或 Job；本轮没有做严格配对的 live 前后快照，因此不将它写成独立的 live no-side-effect E2E PASS。

## 浏览器验收

| 项目 | 结果 | 实际证据 |
|---|---|---|
| Built-in Browser 控制 | BLOCKED | 现有 IAB tab 可发现；`getTab` 返回 `browser.user is unavailable`，一次受支持的 reconnect/retry 返回相同错误 |
| KPCP Draft UI / YAML / Provider | BLOCKED | 本轮没有读取或操作当前页面 DOM；当前共享 API `/api/health` 仍显示 `aiProvider=ollama`，不能代表 DeepSeek build |
| EN/ZH 真实错误 UI | BLOCKED | DeepSeek 错误码的本地化实现有 Go/前端回归，但未做浏览器验收 |
| Browser PNG 截图 | BLOCKED | 没有可控 Browser capture，未生成或引用截图 |
| Computer Use | NOT USED | `COMPUTER_USE_INVOKED = FALSE` |

没有将历史截图、HTTP API 调用或前端 build 作为本轮浏览器证据。

## 证据

- [DS01 原始安全日志](evidence/deepseek-final-20261102/DS01.log)，SHA-256 `8cc895868f8188e7901e9772cc45ab0bd1011af70faa9a3cfe121c85dee432d4`
- [DS02–DS07 原始安全日志](evidence/deepseek-final-20261102/DS02-DS07.log)，SHA-256 `328587c117a34a706cbb0cde81f87983c8a72507eb5f3b8ec7c4bc223450f81e`
- [脱敏语义收据](evidence/deepseek-final-20261102/deepseek-live.public.json)
- [浏览器预检收据](evidence/deepseek-final-20261102/browser-preflight.public.json)
- [当前基础设施只读预检](evidence/deepseek-final-20261102/infrastructure-preflight.public.json)

## 结论

**DeepSeek 真实推理与语义矩阵：PASS（7/7，限定于 Draft intent）。** Provider contract 和错误分类通过 controlled fixture。**完整 DeepSeek UI acceptance：BLOCKED。** 结果不代表 Terraform 计划质量、真实 Class UI 校验、生产 IAM 或 AWS 可移植性。

## PR #3 后新增批次 — 2026-11-02

Run ID：`kpcp-final-20261102-1815`。以上七次请求、1,948 tokens 与 Browser BLOCKED 是原批次结果，保持不变。本轮新建内置 Browser 标签页预检成功；独立 QA `5174 → 8091` 的 runtime provider 真实返回 `deepseek`，clean backend 为 `f8c307fd6555ff118249f667dd2833d41f650e76`（`vcs.modified=false`），配置模型 `deepseek-flash`。成功 API 不暴露 served model 或 token usage，本轮未推测该值。

两次真实 UI 调用：首次发现否定句 Do not enable Valkey 被本地正向 phrase matcher 错误覆盖，保留 FAIL 截图；最小修复及针对性 red→green 回归后，第二次页面显示 DeepSeek、正确名称/namespace/Class/local/2 nodes、Valkey Disabled、有效 YAML。中文/英文切换、Advisory、独立 Submit 与未提交边界均通过。真实专属 API outage 的首个英文直出错误也保留；修复前端 transport 分类后，中英文错误和技术详情通过，未增加模型调用。

| 项目 | 新批次结果 | 边界 |
|---|---|---|
| Built-in Browser / 真截图 | PASS | 当前聊天新 tab；8 张真实 JPEG，有逐图 hash |
| DeepSeek Draft / Typed Intent / YAML | PASS（修复后） | 2 次真实请求，未重跑 DS01–DS07 |
| Class / 容量 / Advisory | PASS | 实际 Ready Class、1–3 节点边界内的 2 节点、EN/ZH Warning |
| Error localization | PASS（修复后） | 真实本地 API outage；没有伪称真实 DeepSeek 429/500 故障注入 |
| No Submit / No Mutation | PASS | 前后无 Environment/Stack/Run；仅另行创建的 preflight Job，UID 分开记录 |
| Freeze / AWS Production | 另行评估 / NOT VALIDATED | UI PASS 不替代安全 Gate 或真实 AWS |

逐 Step、预期/实际、截图解释见 [TC03 新批次](TC03_AI_DRAFT.md#11-pr-3-后独立验收批次--2026-11-02)。[本轮 UI 收据](evidence/final-acceptance-20261102/deepseek-ui.public.json)保留实际 URL、source、截图 hash、两次调用及资源计数；[首次失败收据](evidence/final-acceptance-20261102/browser-initial.public.json)不覆盖。没有输出、写入前端或提交 API key。
