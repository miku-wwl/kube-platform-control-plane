# KPCP Stage 2 测试设计

基线：`main` / `408163fae9f8b80192ff3a94cc584692d010f426`　｜　整理日期：2026-10-08

KPCP 让用户通过网页描述环境、审核草稿、提交环境并批准变更。创建和删除均需人工审批；后台执行完成后更新环境状态。

## 1. 测试目标

- 验证 Ollama + phi4-mini 可生成符合用户输入的环境草稿。
- 验证提交、计划、审批、执行、Ready 全流程。
- 验证生成草稿和未审批方案不会执行基础设施变更。
- 验证后台重启后任务可恢复，多个目标之间相互隔离。
- 验证删除需要独立审批，结束后测试资源无残留。

## 2. 测试范围

| 模块 | 测试内容 | 测试类型 |
|---|---|---|
| AI Builder | 自然语言草稿、明确参数、缓存选择 | 功能 / AI 输出 / UI |
| Environment | 提交、状态变化、Ready | 功能 / UI |
| Approval | 审批前后行为、错误及旧审批拒绝 | 流程 / 负向 |
| PlanTopology | 资源、依赖、CREATE / UPDATE / REPLACE | UI |
| Apply | 执行成功及资源可用 | 功能 |
| Destroy | 删除确认、单独审批、环境消失 | 功能 / 负向 / UI |
| Recovery | 处理中及完成后的后台重启 | 恢复 |
| Multi-target | 环境 A / B 分离 | 隔离 |
| Cleanup | 本次测试资源清理、既有环境保留 | 清理 |

## 3. 测试策略

功能和流程通过网页检查；负向场景检查拒绝提示及零执行；恢复、隔离和清理由现有本地 E2E 验证。AI 检查中英文、缓存开关和明确参数。UI 关键状态必须有真实截图。

### 3.1 准备网页验收环境

确认 Docker、Kind、Ollama、已安装的 `phi4-mini:latest` 和外部 LocalStack Ultimate 可用；API 端口 8090 未被占用。仓库根目录运行：

```powershell
pwsh -NoProfile -File e2e/Run-LocalE2E.ps1 -Suite browser -BrowserSession -AIProvider ollama -OllamaModel phi4-mini:latest -ApiPort 8090 -LocalStackEndpoint http://127.0.0.1:4566 -RequireExistingLocalStack
```

等待输出 `BROWSER_SESSION_READY`。读取该次输出目录的 `browser-session.json`；保持该终端运行。前端未启动时，在另一终端运行 `npm --prefix web run dev`，打开 `http://127.0.0.1:5173`。

| 网页参数 / 检查项 | 填写方式 |
|---|---|
| API | 使用元数据的 `apiBase`；网页代理要求它是 `http://127.0.0.1:8090` |
| 提供器 / 模型 | 元数据 `aiProvider=ollama`、`ollamaModel=phi4-mini:latest`；草稿页面 Provider 应显示 Ollama |
| 自然语言需求 | `创建一个小型开发环境，不要缓存，使用所选名称、区域和1个节点` |
| Environment name | 使用元数据的 `environmentName`，每次新会话名称不同 |
| Namespace | `platform-system`；这是管理端环境所在位置，目标运行资源所在的 `default` 不是此字段 |
| Environment class | 选择元数据的 `className`，须显示 Ready |
| Region | 选择类别允许的 `local` |
| Node count | `1`，且位于类别允许的范围内 |
| 草稿检查 | 名称、命名空间、类别、区域、节点数与表单一致；缓存 Disabled |

类别由会话提供可执行的来源、后端、运行器和目标配置，测试人员无需猜测这些引用。模板仅保存配置；**选择模板不等于已经创建 Ready 类别**。

### 3.2 数据与结束条件

普通 BrowserSession 足以验证创建/审批/删除，但通常只产生 CREATE。TC06 的三动作专项须由开发人员另外提供已准备的多资源类别和初始资源：

| 测试资源 | 初始状态 → 目标状态 | 预期动作 |
|---|---|---|
| 主 S3 桶 | 标签 `Stage=before` → `Stage=after` | UPDATE |
| 替换 S3 桶 | 名称后缀 `-original` → `-next` | REPLACE |
| 桶内对象 | 不存在 → 新对象，依赖上述两个桶 | CREATE |

预期拓扑为 3 节点、2 条关系线；检查中英文及常用窄屏宽度。准备工作须使用真实资源与测试状态，不能注入伪造页面或计划数据。

TC09–TC11 复测可分别使用现有 `-Suite recovery`、`-Suite multitarget`、`-Suite failclosed`；`-Suite all` 覆盖全部后端测试。均使用本地 LocalStack，并加 `-RequireExistingLocalStack`。本机依赖域名解析有问题时，由开发人员确认后使用现有 `-TerraformDnsServer` 参数。

TC14 完成后，在另一 PowerShell 终端执行以下命令。把 `<本次输出目录>` 替换为 `BROWSER_SESSION_READY` 后打印的完整目录；不要使用历史会话目录。

```powershell
$qaSession = Get-Content -LiteralPath '<本次输出目录>\browser-session.json' -Raw | ConvertFrom-Json
$qaSession | Select-Object runId, environmentName, holdFile
# 确认 runId 是刚才验收的会话，再释放它。
Remove-Item -LiteralPath $qaSession.holdFile
```

回到启动终端，等待 `BROWSER_SESSION=PASS`、`CLEANUP=PASS` 和 `LOCAL_E2E=PASS`，保存该次 `summary.txt`。默认会话期限为 1 小时；不要等到超时或直接关闭启动终端。只释放该次会话；禁止误删其他环境。若工具策略阻止释放，记录工具阻塞，不把它认定为产品缺陷。

## 4. 测试用例设计

| 用例 ID | 优先级 | 测试点 | 前置条件 | 操作步骤 | 预期结果 |
|---|---|---|---|---|---|
| TC01 | P0 | AI 生成环境草稿 | API 连接；Ollama 模型可用；Ready 类别 | 按 3.1 填表 → 生成草稿 → 核对结果 | 草稿 Validated；Provider 为 Ollama；会话模型为 phi4-mini；明确参数保持正确 |
| TC02 | P0 | 草稿不自动创建资源 | 记录本次环境/执行数量 | 生成草稿 → 不提交 → 查看列表与执行记录 | 环境数量不变；没有基础设施执行 |
| TC03 | P0 | 提交环境 | 有效草稿；唯一环境名 | 审核草稿 → 点击 Submit → 打开环境 | 提交成功；环境出现；状态开始推进 |
| TC04 | P0 | Plan 生成并等待审批 | 环境已提交；人工审批模式 | 等待 Plan → 不审批 → 查看状态 | Plan 完成；显示等待审批；Apply 未开始 |
| TC05 | P0 | 未审批不得执行 | 变更正在等待审批 | 保持未审批 → 检查执行及目标资源 | 无 Apply；无提前创建或修改基础设施 |
| TC06 | P1 | 多资源 PlanTopology | 已准备 3.2 的真实多资源计划 | 打开架构预览 → 核对动作、属性、连线 → 切换语言/宽度 | 3 节点/2 连线；三种动作正确；无阻塞错误、重叠或明显敏感信息 |
| TC07 | P0 | 审批后执行 | 当前方案可审批且未过期 | 点击批准当前方案 → 查看执行状态 | 有效审批接受；开始 Apply；状态推进 |
| TC08 | P0 | 环境 Ready | Apply 已开始 | 等待完成 → 检查概览与目标资源 | 环境、基础设施、运行时 Ready；预期资源存在 |
| TC09 | P0 | 服务重启恢复 | 隔离测试环境；现有 recovery 验证可运行 | 处理中重启后台 → 等待恢复 → 核对执行次数 | 任务恢复；不重复执行；最终状态正确 |
| TC10 | P0 | 多环境/目标隔离 | 环境 A、B 使用不同 Kind 目标 | 操作 A → 检查 A、B → 再操作 B | 资源仅出现在正确目标；对 A 的操作不改变 B |
| TC11 | P0 | 异常场景安全保护 | 准备错误审批或不匹配目标 | 提交错误请求 → 检查拒绝及执行记录 | 请求拒绝/安全停止；无继续执行或错误目标变更 |
| TC12 | P0 | 删除环境 | 环境 Ready | 点击删除 → 输入完整名称 → 请求删除 | 请求接受；显示删除中；清理前环境仍可查看 |
| TC13 | P0 | Destroy 独立审批 | 已请求删除；创建审批已存在 | 不做新审批 → 查看 Destroy → 单独批准 | 创建审批不能沿用；新 Destroy 审批接受后才销毁 |
| TC14 | P0 | Destroy 完成 | Destroy 已单独审批 | 等待完成 → 查环境列表和目标资源 | 基础设施和运行资源消失；环境移除 |
| TC15 | P0 | Cleanup 无残留 | 本次测试已结束；记录既有 `pcp-dev` 与外部服务 | 释放会话 → 查看清理结果 → 核对本次资源 | 本次桶、集群、进程、临时目录不存在；既有开发环境与外部服务保留 |

共 15 项：功能 5、流程 2、负向 4、UI 专项 1、恢复 1、隔离 1、清理 1。UI 关键测试为交叉标签，共 9 项：TC01、TC03、TC04、TC06、TC07、TC08、TC12、TC13、TC14。

执行结果与图片见 [Stage 2 验证报告](STAGE2_VALIDATION_REPORT.md)。
