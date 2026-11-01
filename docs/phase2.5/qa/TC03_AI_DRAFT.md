# TC03 — AI Draft

## 1. Test Case Summary

| Field | Value |
|---|---|
| Test ID | TC03 |
| Test Name | AI Draft |
| Priority | P0 |
| Test Type | Manual UI + Engineering Evidence |
| Preconditions | TC02 已通过；Class kpcp-qa-20261101-2253-v1 为 Ready；专用 Controller、固定来源和 Backend 探针成功；位于同一 Tab 3 构建器。 |
| Expected Result | 真实点击生成草稿后，原生 provider 返回通过校验的类型化预览；字段与输入一致，提交仍需独立操作。 |
| Actual Result | 生成成功，提供方显示确定性生成；名称、命名空间、Class、local 区域、1 节点、Valkey 未启用与请求一致；提交按钮独立显示。 |
| Status | PASS |
| Commit | `e40c422327aa648a89d6baabf7268cb3239310d8`（现有 5445a5f 二进制，vcs.modified=true；产品路径差异为空，非本次 clean rebuild） |
| Execution Date | 2026-11-01T23:06:48.357000+13:00 → 2026-11-01T23:06:49.178000+13:00 |
| QA Run ID | `kpcp-qa-20261101-2253` |
| Evidence Scope | Current Execution: real UI + project-native deterministic provider; live LLM quality NOT RUN |

**Historical Evidence：**原 TC03 为 BLOCKED，图 18、19 显示无 Ready 类别；本轮已真正准备 Class 与执行前置条件，新截图证明原生草稿 UI 工作流可执行。历史 BLOCKED 仍保留身份。

## 2. Test Steps

1. 从 Ready Class 点击使用此类别进入构建器。
2. 填写需求、环境名称、命名空间和节点数量。
3. 点击生成草稿。
4. 逐项审核 provider、字段、YAML 和独立提交按钮。

## 3. Detailed Manual Test Guide

### Step 1 — Enter the Draft Request

**操作：**

1. 在 Ready Class 详情点击使用此类别。
2. 自然语言需求填：创建一个 1 节点开发环境，仅基础设施和测试 ConfigMap。
3. 环境名称填 qa-environment，命名空间填 kpcp-qa-20261101-2253。
4. 核对 Class 已选 kpcp-qa-20261101-2253-v1，区域使用类别默认值 local，节点数量 1。

**预期结果：** Class 就绪、生成草稿启用，右侧尚无草稿。

**实际结果：** 字段正确，左侧 Class 卡片显示就绪；生成草稿按钮启用。

**Screenshot：**

![TC03 Enter the Draft Request](screenshots/TC03/01-draft-input-ready-class.png)

**图注：** 完整输入和 Ready Class 卡片可见；此前没有生成结果。

### Step 2 — Generate and Review the Typed Draft

**操作：**

1. 实际点击生成草稿。
2. 检查右侧校验通过。
3. 检查提供方明确为确定性生成；不将其写成 Ollama 或真实 LLM 推理。
4. 核对环境、Class、区域、容量与 Valkey 字段。
5. 核对拟议 PlatformEnvironment YAML 的 metadata.name/namespace、spec.classRef、capacity.nodeCount 和 region。
6. 保留页面，暂不点击提交 PlatformEnvironment，交给 TC04 做隔离核对。

**预期结果：** 原生 API 结果可审核；尚未触发显式提交。

**实际结果：** 预览通过校验；qa-environment、本轮 namespace/Class、local、1 节点、Valkey 未启用；YAML 和提交按钮可见。

**Screenshot：**

![TC03 Generate and Review the Typed Draft](screenshots/TC03/02-generated-draft-review.png)

**图注：** 全页保留左侧输入、右侧确定性生成身份、校验结果、YAML 和独立提交按钮；是当前产品真实页面。

### Historical Evidence — 原有截图归档引用

以下图片来自前一轮 2026-11-01 21:54–22:03、产品基线 `5445a5f`，不计入本轮截图。保留历史执行事实；含私网地址的三图引用原有遮盖副本。

![Historical TC03 图18](../screenshots/qa-20261101-18-builder-no-ready-class.png)

图注：Historical Evidence，原报告图 18；无 Ready Class 时草稿生成被禁用。原图与公开副本对应关系沿用原有清单。

![Historical TC03 图19](../screenshots/qa-20261101-19-class-selector-empty.png)

图注：Historical Evidence，原报告图 19；实际展开类别选择器后无可选项。原图与公开副本对应关系沿用原有清单。

## 4. Engineering Evidence

Current Execution：点击调用实际 QA API 的 /api/drafts，页面 provider=确定性生成，与 /api/health 的 deterministic 配置一致。未调用模型端点，没有页面数据注入。隔离边界的资源前后核对在 TC04 独立记录。Historical Evidence：既有 Go E2E PlatformAPI_DraftAndTemplate 也使用 deterministic；历史记录不替代上述新 UI 证据。

构建和前置环境的范围限制见[本轮审查收据](evidence/TC02/environment-preflight.public.json)，二进制原始构建身份及哈希已保留。

- [本用例状态与截图 SHA-256](evidence/TC03/result.json)

## 5. Actual Result

本用例 PASS 仅对应项目正式支持的原生 deterministic 草稿 UI 工作流、类型化校验和字段显示。现场 Ollama 服务/模型不可用，真实 AI 意图理解与模型质量为 NOT RUN，仍是 Phase 2.5 完整 Freeze 的缺口。默认 Valkey 需求已明确改为本轮固定模块支持的基础设施与 ConfigMap 场景，未冒充 Valkey/Deployment 验收。

## 6. Defects and Blockers

真实模型质量 NOT RUN：缺少 Ollama 服务和模型。中文 advisory warning 仍为英文，归入已打开的 QA-01，见 TC15。没有修改产品代码或禁用校验。

## 7. Final Verdict

**PASS** — 原生 deterministic 真实 UI 草稿路径 PASS；真实 LLM 推理不在此 PASS 范围。

## 8. Cleanup / Handoff

保留同一页的有效 Draft 给 TC04、TC05；不取消、刷新或删除 Class。没有通过 API/脚本代替 UI 生成。

以上保留要求是本用例结束时的连续生命周期交接。整个 Run 的最终清理现已完成，见[TC14](TC14_DESTROY_CLEANUP.md)及[最终清理收据](evidence/TC14/final-cleanup.public.json)；没有待清理测试基础设施。
