# KPCP Stage 2 测试设计

文档审查基线：`main` / `acd00679f05bc1c5de465ec775d2a02513253c06`　｜　整理日期：2026-10-08

**交接结论：主网页流程可按本指南执行；TC06-B 为 PREPARATION REQUIRED。** 本次仅完成文档静态核对和纸面演练，未启动会话、未执行网页或自动化测试。`STAGE2_FREEZE = BLOCKED`。

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

| 执行类型 | 用例 | 执行入口 |
|---|---|---|
| 手动网页测试（MANUAL UI TEST） | TC01–TC08，其中 TC06 使用 TC06-A；TC12–TC15 | 3.1 主流程；3.3 结束与清理。TC02/TC05/TC08/TC14 的隐藏行为另保存只读检查结果 |
| 自动化回归（AUTOMATED REGRESSION TEST） | TC09、TC10、TC11 | 3.4；脚本自行准备、重启和检查测试资源 |
| 专项夹具测试（SPECIALIZED TEST REQUIRING FIXTURE） | TC06-B | 3.2；开发人员先交付真实多资源夹具 |

判定：实际结果符合预期且必需证据完整才记 **PASS**；行为不符记 **FAIL**；外部依赖或工具阻止执行记 **BLOCKED**；未尝试记 **NOT RUN**。超时先保存现状；确认外部原因记 BLOCKED，流程未按预期完成记 FAIL。历史回归不能代替新网页 PASS。先复制[空白执行记录](STAGE2_MANUAL_TEST_RECORD.md)，在副本填结果和缺陷；不修改空白模板。

### 3.1 准备网页验收环境

#### 3.1.1 启动与动态会话参数

所有 PowerShell 终端先进入仓库根目录 `D:\workshop\oct\kube-platform-control-plane`。终端 A 运行会话，终端 B 运行前端，终端 C 读参数、保存检查结果及释放会话。同一仓库一次只运行一个验收脚本。

**启动前检查（终端 C）**：以下均为只读检查。Docker Desktop 必须已运行；已部署的 LocalStack Ultimate 必须发布本机 4566；Ollama 必须已安装 `phi4-mini:latest`。工具缺失、端口占用或接口不可达时先停止准备，记 BLOCKED 并联系环境负责人。

```powershell
Set-Location -LiteralPath 'D:\workshop\oct\kube-platform-control-plane'
Get-Command pwsh, git, python, docker, kind, kubectl, go, node, npm -ErrorAction Stop
docker info
kind get clusters
docker ps --format '{{.Names}} {{.Status}} {{.Ports}}'
Invoke-RestMethod -Uri 'http://127.0.0.1:4566/_localstack/health'
Invoke-RestMethod -Uri 'http://127.0.0.1:11434/api/version'
(Invoke-RestMethod -Uri 'http://127.0.0.1:11434/api/tags').models | Select-Object name
Get-NetTCPConnection -State Listen -LocalPort 8090, 5173 -ErrorAction SilentlyContinue
git branch --show-current
git rev-parse HEAD
```

记录测试前已有的 Kind 集群及外部 LocalStack 容器名/状态，清理后作对照。8090 应空闲；5173 若已有本仓库前端可复用，否则请环境负责人处理冲突，勿任意停止进程。

**启动会话（终端 A）**：

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass `
  -File e2e/Run-LocalE2E.ps1 `
  -Suite browser `
  -BrowserSession `
  -AIProvider ollama `
  -OllamaEndpoint http://127.0.0.1:11434 `
  -OllamaModel phi4-mini:latest `
  -LocalStackEndpoint http://127.0.0.1:4566 `
  -ApiPort 8090 `
  -BuildParallelism 1 `
  -RequireExistingLocalStack
```

等待 `BROWSER_SESSION_READY <本次完整输出目录> API=http://127.0.0.1:8090`，复制其中完整目录。**终端 A 保持运行**；READY 只证明测试类别与 API 已准备，不证明网页测试通过。未出现 READY 时保存终端错误及该次 `artifacts/e2e/<run-id>/summary.txt`、`logs/`，按 3.5 处理，勿沿用旧会话。只有确认 Terraform 域名解析失败时，由环境负责人决定是否加现有参数 `-TerraformDnsServer 1.1.1.1` 后重新启动。

**启动前端（终端 B）**：首次使用、缺少 `web/node_modules` 时执行 `npm --prefix web ci`；随后运行下列命令并保持终端打开。首次安装需网络，失败记环境阻塞。

```powershell
npm --prefix web run dev -- --host 127.0.0.1 --port 5173 --strictPort
```

**读取本次参数（终端 C）**：输入刚才终端 A 的目录，不按“最新文件夹”猜测，不使用历史 Run ID。

```powershell
$qaRepoRoot = (Get-Location).Path
$qaArtifactDir = (Resolve-Path -LiteralPath (Read-Host '粘贴本次 BROWSER_SESSION_READY 的完整输出目录')).Path
$qaArtifactRoot = [IO.Path]::GetFullPath((Join-Path $qaRepoRoot 'artifacts/e2e'))
$qaSession = Get-Content -LiteralPath (Join-Path $qaArtifactDir 'browser-session.json') -Raw | ConvertFrom-Json
$qaOwnership = Get-Content -LiteralPath (Join-Path $qaArtifactDir 'ownership.json') -Raw | ConvertFrom-Json
if ($qaSession.runId -notmatch '^\d{14}-[a-f0-9]{8}$' -or
    $qaArtifactDir -ne (Join-Path $qaArtifactRoot $qaSession.runId) -or
    $qaOwnership.owner -ne 'kpcp-local-e2e' -or $qaOwnership.runId -ne $qaSession.runId -or
    $qaOwnership.cleanupCompleted) { throw '不是本次有效的受管会话目录；停止并核对终端 A' }
if ([DateTimeOffset]::Parse($qaSession.expiresAt) -le [DateTimeOffset]::UtcNow) { throw '会话已过期；检查结束记录并启动新会话' }
if ($qaSession.apiBase -ne 'http://127.0.0.1:8090' -or $qaSession.aiProvider -ne 'ollama' -or
    $qaSession.ollamaModel -ne 'phi4-mini:latest') { throw '会话端点/提供器/模型不符合本次测试配置' }
$qaSession | Select-Object runId, environmentName, className, runtimeNamespace, apiBase, aiProvider, ollamaModel, expiresAt, holdFile
$qaHealth = Invoke-RestMethod -Uri "$($qaSession.apiBase)/api/health"
$qaClass = Invoke-RestMethod -Uri "$($qaSession.apiBase)/api/classes/$($qaSession.className)"
$qaHealth | Select-Object status, aiProvider
$qaClass | Select-Object name, ready, defaultRegion, allowedRegions, capacityBounds
$qaEvidenceDir = Join-Path $qaRepoRoot "artifacts/qa/$($qaSession.runId)"
New-Item -ItemType Directory -Path $qaEvidenceDir -Force | Out-Null
$qaRecordPath = Join-Path $qaEvidenceDir 'STAGE2_MANUAL_TEST_RECORD.md'
if (-not (Test-Path -LiteralPath $qaRecordPath)) {
  Copy-Item -LiteralPath 'docs/STAGE2_MANUAL_TEST_RECORD.md' -Destination $qaRecordPath -ErrorAction Stop
  $qaRecordText = [IO.File]::ReadAllText($qaRecordPath)
  $qaRecordText = $qaRecordText.Replace('](STAGE2_TEST_DESIGN.md)', '](../../../docs/STAGE2_TEST_DESIGN.md)')
  $qaRecordText = $qaRecordText.Replace('](STAGE2_VALIDATION_REPORT.md)', '](../../../docs/STAGE2_VALIDATION_REPORT.md)')
  $qaRecordText = $qaRecordText.Replace('](STAGE2_FREEZE_EVIDENCE.md)', '](../../../docs/STAGE2_FREEZE_EVIDENCE.md)')
  [IO.File]::WriteAllText($qaRecordPath, $qaRecordText, [Text.UTF8Encoding]::new($false))
}
$qaRecordLinks = @('STAGE2_TEST_DESIGN.md', 'STAGE2_VALIDATION_REPORT.md', 'STAGE2_FREEZE_EVIDENCE.md')
$qaCopiedRecordText = [IO.File]::ReadAllText($qaRecordPath)
foreach ($qaReferenceName in $qaRecordLinks) {
  if (-not $qaCopiedRecordText.Contains('](../../../docs/' + $qaReferenceName + ')')) { throw "Copied record link was not rewritten: $qaReferenceName" }
  $qaReferencePath = [IO.Path]::GetFullPath((Join-Path $qaEvidenceDir "../../../docs/$qaReferenceName"))
  if (-not (Test-Path -LiteralPath $qaReferencePath -PathType Leaf)) { throw "Copied record reference is missing: $qaReferencePath" }
  "Record link target: $qaReferencePath"
}
```

要求 health 为 `status=ok`、`aiProvider=ollama`，类别 `ready=True`，允许区域含 `local`，节点范围含 1；否则不要开始 UI 操作。前端代理固定连接 8090。打开 `http://127.0.0.1:5173`，右上角应显示 **API connected / API 已连接**。模型来源是本次元数据及 Ollama 模型清单；网页只显示提供方，不能从网页单独证明模型名。

以下表格是**每次重新读取的数据**；填写到执行记录 A，截图保存到本次 `$qaEvidenceDir`，不要覆盖历史 JPG。执行记录副本已存在时继续填写，勿再次复制覆盖。

| 网页参数 / 检查项 | 填写方式 |
|---|---|
| API | 使用元数据的 `apiBase`；网页代理要求它是 `http://127.0.0.1:8090` |
| 提供器 / 模型 | 元数据 `aiProvider=ollama`、`ollamaModel=phi4-mini:latest`；草稿页面 Provider 应显示 Ollama |
| 自然语言需求 | `创建一个小型开发环境，不要缓存，使用所选名称、区域和1个节点` |
| Environment name | 使用元数据的 `environmentName`，每次新会话名称不同 |
| Namespace | `platform-system`；这是管理端环境所在位置，目标运行资源所在的 `default` 不是此字段 |
| Environment class | 选择元数据的 `className`，须显示 Ready |
| Region | 本脚本类别允许 `local`；以本次 `$qaClass.allowedRegions` 为核对依据，明确选择 `local` |
| Node count | `1`，且位于类别允许的范围内 |
| 草稿检查 | 名称、命名空间、类别、区域、节点数与表单一致；缓存 Disabled |

类别由会话提供可执行的来源、后端、运行器和目标配置，测试人员无需猜测这些引用。模板仅保存配置；**选择模板不等于已经创建 Ready 类别**。

元数据**没有管理命名空间字段**：表单 Namespace 固定填脚本使用的 `platform-system`，必须覆盖网页默认 `default`。`runtimeNamespace=default` 只用于目标运行资源查询。`environmentName`、`className`、`runId`、`holdFile`、`expiresAt`、kubeconfig、桶名均从本次元数据读取。

#### 3.1.2 主网页生命周期与截图检查点

进入左侧 **Platform builder / 平台构建器**（或右上 **New environment / 新建环境**）。Natural-language request / 自然语言需求、Environment name / 环境名称、Namespace / 命名空间、Environment class / 环境类别、Region / 区域、Node count / 节点数量按 3.1.1 填写。

**先运行本节下方 TC02 的两条基线查询，再开始第 1 步。** 本节按钮采用实际中英文标签；Plan / 计划、Apply / 执行、Destroy / 销毁、Ready / 就绪、Running / 运行中、ChangesPresent / 存在变更、Succeeded / 成功按语言显示。审批按钮位于 **Architecture preview / 架构预览** 右侧审核面板；Terraform runs 用于查看执行记录。

页面自动刷新；每阶段先等待状态推进，Plan / Apply / Destroy 的观察上限各为 15 分钟，并记录开始/结束时间。这是 QA 等待标准，不是网页倒计时。整个会话默认有效 1 小时，以 `expiresAt` 为准；若准备更长测试，启动前可使用支持的 `-BrowserSessionTimeoutSeconds 7200`，到期后必须重新准备。操作失败先截图和记缺陷，后续依赖用例记 BLOCKED，不反复提交或审批同一请求。

| 顺序 / TC | 网页操作 | PASS 检查点（不符记 FAIL） | 截图及其证明内容 |
|---|---|---|---|
| 1 / TC01 | 填表后点击 **Generate draft / 生成草稿**；等待 Generating… / 正在生成… 结束 | **Validated / 校验通过**；Provider / 提供方为 Ollama；名称/命名空间/类别/区域/节点与输入一致；Valkey 为 Disabled / 未启用 | `TC01-ai-draft-ollama.png`：表单和完整校验草稿；模型名另由元数据证明 |
| 2 / TC02 | 保持草稿，**不点击提交**；查看左侧 Environments / 环境计数，并运行下方“提交前检查” | 本次环境不存在；管理端执行记录未增加；计数与生成前一致（普通新会话为 0） | `TC02-before-submit.png`：草稿与列表计数；隐藏执行由只读 JSON 证明 |
| 3 / TC03 | 点击 **Submit PlatformEnvironment / 提交 PlatformEnvironment** | 自动进入本次环境详情；名称为本次 environmentName，命名空间 platform-system，类别为本次 className；状态开始推进，可见 PlanRunning | `TC03-environment-created.png`：本次名称、命名空间、类别、已创建状态；错过短暂状态可拍后续 WaitingApproval 并在记录说明 |
| 4 / TC03 | 点击左侧 **Environments / 环境**，找到本次名称；点击**环境名称**返回详情 | 列表确有本次环境；详情名称一致 | 已有图无法同时证明列表时追加 `TC03-environment-listed.png` |
| 5 / TC04、TC05 | 打开 **Terraform runs / Terraform 运行记录**；等待 Plan 完成，点击该 Plan 行可展开证据，先不批准 | 环境 **WaitingApproval**；Reconcile Plan 为 Ready、ChangesPresent，Evidence ready / 证据已就绪；没有 Apply。等待状态中文页也可能显示原始 WaitingApproval | `TC04-awaiting-approval.png`：环境状态与 Plan 运行记录；TC05 另存只读检查结果 |
| 6 / TC06-A | 打开 **Architecture preview / 架构预览**，查看 Architecture change preview / 架构变更预览 | 真实计划资源与动作可见，无阻塞错误。普通夹具为 1 个 CREATE、0 条边，不要求 3 节点；详见 3.2 | `TC06-plan-topology.png`：本次环境名称、动作汇总和全部可见节点；有关系时包含连线 |
| 7 / TC07 | 保持 **Architecture preview / 架构预览**，在右侧核对当前 Plan 名称及 Expiration / 过期时间未过期；点击 **Approve exact plan / 批准此确切 Plan**；再切到 Terraform runs | 出现 **Approval recorded / 审批已记录**；Latest approval / 最近审批为 Recorded / 已记录；产生 Apply，Running 后达到 Ready / Succeeded | `TC07-approved-applying.png`：审批接受及 Apply 记录；状态很快时拍成功后续状态并记录未捕获 Running；同屏不足时追加审批面板图 |
| 8 / TC08 | 打开 **Overview / 概览**，等待就绪 | 本次环境、Infrastructure / 基础设施、Runtime / 运行时均 Ready / 就绪；Resource inventory / 资源清单计数为 1；下方只读桶及 Service 检查通过 | `TC08-ready.png`：名称、三项 Ready 和资源计数；桶/Service 的实际存在另存查询结果 |
| 9 / TC12 | 点击 **Delete environment / 删除环境**；在 **Confirm environment name / 确认环境名称** 输入本次 environmentName；点击 **Request deletion / 请求删除** | 请求接受，环境仍可见且 **Deleting / 删除中**，开始准备 Destroy Plan | 与下一步同图足够时共享；若需单独证明请求接受，追加 `TC12-delete-requested.png` |
| 10 / TC13 | 在 Terraform runs 等待新的 Ready / ChangesPresent Plan；切到 **Architecture preview / 架构预览**，**先不批准**；记录新 Plan 名称及创建审批 | 新 Plan 的 planMode=Destroy；显示 **Review the Destroy plan / 审核销毁 Plan** 和 **Separate Destroy approval / 独立销毁审批**；旧创建审批不能解除当前审批等待，未产生 Destroy 执行 | `TC13-destroy-approval.png`：Deleting、新 Destroy Plan 和未审批状态；旧审批不在同屏时补只读 JSON |
| 11 / TC13 | 在架构预览右侧核对 Destroy Plan 未过期，点击 **Approve exact plan / 批准此确切 Plan**；切到 Terraform runs 观察 | 新审批记录接受；Destroy 执行开始。系统在批准后执行 | 追加 `TC13-destroy-approved.png` 证明新审批及执行记录；必要时分别捕获审批面板和执行记录 |
| 12 / TC14 | 等待完成，页面自动回到 Environments；查找本次名称 | 出现 **Environment removed after finalizer cleanup. / finalizer 清理完成，环境已移除。**；本次名称消失，计数回到基线；只读检查桶/Service 不存在 | `TC14-environment-removed.png`：移除提示与列表；资源不存在不能只靠截图判定 |
| 13 / TC15 | 保存本次记录和证据后，按 3.3 释放；等待脚本结束 | 正常释放与本次资源清理通过；既有集群/外部服务保留 | 保存 `summary.txt`、`ownership.json`、前后只读对照，不要求伪造清理截图 |

**只读检查补充（终端 C）**：继续使用 3.1.1 的本次变量。以下命令按对应时机逐段执行，不能一次全部运行。查询报错时先核对连接及退出码；错误输出不能当作资源不存在。截图只证明可见界面。

**TC02 基线：生成草稿前执行。** 启动本终端记录，后续命令输出保存为 `manual-checks.txt`。

```powershell
$qaEnvironmentPath = "$($qaSession.apiBase)/api/environments/platform-system/$($qaSession.environmentName)"
Start-Transcript -Path (Join-Path $qaEvidenceDir 'manual-checks.txt') -Append
$qaDraftEnvironmentsBefore = Invoke-RestMethod -Uri "$($qaSession.apiBase)/api/environments"
ConvertTo-Json -InputObject $qaDraftEnvironmentsBefore -Depth 30 |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-environments-before.json')
kubectl --kubeconfig $qaSession.managementKubeconfig get terraformruns -n platform-system -o json |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-runs-before.json')
"kubectl exitCode=$LASTEXITCODE"
```

**TC02 提交前：草稿生成后、点击 Submit 前执行。** 比较 before/after 文件，本次环境不存在，执行数量未增加。

```powershell
$qaDraftEnvironmentsAfter = Invoke-RestMethod -Uri "$($qaSession.apiBase)/api/environments"
ConvertTo-Json -InputObject $qaDraftEnvironmentsAfter -Depth 30 |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-environments-after.json')
kubectl --kubeconfig $qaSession.managementKubeconfig get terraformruns -n platform-system -o json |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-runs-after.json')
"kubectl exitCode=$LASTEXITCODE"
$qaDraftRunsBefore = Get-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-runs-before.json') -Raw | ConvertFrom-Json
$qaDraftRunsAfter = Get-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC02-runs-after.json') -Raw | ConvertFrom-Json
"environments before=$($qaDraftEnvironmentsBefore.Count); after=$($qaDraftEnvironmentsAfter.Count)"
"TerraformRuns before=$(@($qaDraftRunsBefore.items).Count); after=$(@($qaDraftRunsAfter.items).Count)"
@($qaDraftEnvironmentsAfter | Where-Object { $_.namespace -eq 'platform-system' -and $_.name -eq $qaSession.environmentName }).Count
```

**TC04/TC05：提交并等待 Plan 完成后、创建审批前执行。** Apply 计数应为 0，桶查询应为 404/Not Found。

```powershell
$qaBeforeApproval = Invoke-RestMethod -Uri $qaEnvironmentPath
$qaBeforeApproval | ConvertTo-Json -Depth 30 |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC05-before-approval.json')
@($qaBeforeApproval.terraformRuns | Where-Object operation -eq 'Apply').Count
docker exec $qaOwnership.localStackContainer awslocal s3api head-bucket --bucket $qaSession.managedBucket
"head-bucket exitCode=$LASTEXITCODE"
```

**TC08：环境 Ready 后执行。** 桶查询退出 0，Service JSON 返回本次 serviceName，kubectl 退出 0。

```powershell
docker exec $qaOwnership.localStackContainer awslocal s3api head-bucket --bucket $qaSession.managedBucket
"head-bucket exitCode=$LASTEXITCODE"
kubectl --kubeconfig $qaSession.targetKubeconfig get service $qaSession.serviceName -n $qaSession.runtimeNamespace --ignore-not-found -o json
"kubectl exitCode=$LASTEXITCODE"
```

**TC13：删除请求接受、新 Destroy Plan 完成后、销毁审批前执行。** 新 Plan 的 planMode 为 Destroy；Destroy 操作计数应为 0，创建审批与新 Plan 的审批分别核对。

```powershell
$qaBeforeDestroy = Invoke-RestMethod -Uri $qaEnvironmentPath
$qaBeforeDestroy | ConvertTo-Json -Depth 30 |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC13-before-destroy-approval.json')
$qaBeforeDestroy.terraformRuns | Select-Object name, operation, planMode, state, outcome, approval
@($qaBeforeDestroy.terraformRuns | Where-Object operation -eq 'Destroy').Count
```

**TC14：网页提示环境移除后、释放会话前执行。** 桶为 404/Not Found，Service 无输出且 kubectl 退出 0，环境列表不含本次 namespace/name。

```powershell
$qaEnvironmentsAfterDelete = Invoke-RestMethod -Uri "$($qaSession.apiBase)/api/environments"
ConvertTo-Json -InputObject $qaEnvironmentsAfterDelete -Depth 30 |
  Set-Content -LiteralPath (Join-Path $qaEvidenceDir 'TC14-environments.json')
@($qaEnvironmentsAfterDelete | Where-Object { $_.namespace -eq 'platform-system' -and $_.name -eq $qaSession.environmentName }).Count
docker exec $qaOwnership.localStackContainer awslocal s3api head-bucket --bucket $qaSession.managedBucket
"head-bucket exitCode=$LASTEXITCODE"
kubectl --kubeconfig $qaSession.targetKubeconfig get service $qaSession.serviceName -n $qaSession.runtimeNamespace --ignore-not-found -o json
"kubectl exitCode=$LASTEXITCODE"
```

按 3.3 保存清理对照后执行 `Stop-Transcript`；若中途失败，也先保存现状及记录，再释放并清理，最后停止终端记录。

### 3.2 TC06 两个独立场景

#### TC06-A — PlanTopology 基础展示

**手动网页测试；READY。** 使用 3.1 普通 BrowserSession，等待 Reconcile Plan 完成、尚未审批时执行。当前基础夹具为 `test/fixtures/terraform/localstack-basic/main.tf`，包含 `aws_s3_bucket.lifecycle` 一个资源。打开 Architecture preview / 架构预览：资源地址及 CREATE 信息应可见，动作汇总与 Terraform runs 中 Plan 对应；无依赖时 0 条边正确，有依赖时逐条核对端点。无阻塞错误、遮挡或明显凭据；切换 EN / 中文并缩窄浏览器窗口，分别保存补充截图。普通流程 **1 节点/0 边是 PASS**，不能据此判 TC06-B PASS。

#### TC06-B — 多资源三动作展示

**专项夹具测试；PREPARATION REQUIRED。** 仓库受版本控制的 E2E/Makefile 未提供当前 BrowserSession 可复现的三动作准备入口。本机历史脚本 `tmp/frontend-supplement-20261004/Prepare-RealPlanFixture.ps1` 的参数为 `-SessionPath`，但该文件被 Git 忽略，依赖旧 `source-stage2/work`、`source-stage2.git`；当前脚本生成 `git/source-browser-work`、`git/source-browser.git`。因此它不是可交接的新会话命令，QA 不应直接执行它。

**准备人：开发/测试环境负责人。** 在独立本地测试来源及状态中准备真实初始资源和后续源码，通过产品生成真实计划；QA 不手工运行 Terraform 或创建控制器内部资源。交付清单：可复现准备入口、Ready class、环境名/命名空间/区域/节点数、当前有效会话元数据、三个资源地址及变更前后值、真实 Plan 证据、资源归属和安全清理步骤。缺任一项，TC06-B 记 BLOCKED（已申请但准备缺失）或 NOT RUN（尚未申请/执行），不得填 PASS。

| 测试资源 | 初始状态 → 目标状态 | 预期动作 |
|---|---|---|
| 主 S3 桶 | 标签 `Stage=before` → `Stage=after` | UPDATE |
| 替换 S3 桶 | 名称后缀 `-original` → `-next` | REPLACE |
| 桶内对象 | 不存在 → 新对象，依赖上述两个桶 | CREATE |

**QA 操作**：收到完整交付后，按 3.1 使用交付参数生成草稿并提交，等待审批前真实 Plan；打开架构预览逐项核对 CREATE / UPDATE / REPLACE 各 1、3 节点/2 连线、资源地址和前后值。切换中英文及窄屏，保存 `TC06-B-plan-topology.png` 及必要语言/宽度补图；有阻塞错误、动作或端点错误记 FAIL。结束按交付清理步骤处理该专项归属资源。不得注入计划 JSON 或借用普通流程截图。原 TC06 历史图保留在验证报告，标记为 TC06-B 历史证据。

### 3.3 释放当前会话与清理（TC15）

保存 UI 和只读结果后，终端 C 使用上文 `$qaArtifactDir` / `$qaSession`。若换了终端，先重新执行 3.1.1“读取本次参数”，已有记录副本不要再次复制。确认终端 A 仍在等待且期限未到；到期后只检查结束日志，不能释放另一个会话。

```powershell
$qaReleaseSession = Get-Content -LiteralPath (Join-Path $qaArtifactDir 'browser-session.json') -Raw | ConvertFrom-Json
$qaReleaseOwnership = Get-Content -LiteralPath (Join-Path $qaArtifactDir 'ownership.json') -Raw | ConvertFrom-Json
$qaExpectedHold = [IO.Path]::GetFullPath((Join-Path $qaArtifactDir 'browser-hold.flag'))
if ($qaReleaseSession.runId -ne $qaSession.runId -or
    $qaReleaseOwnership.runId -ne $qaSession.runId -or $qaReleaseOwnership.owner -ne 'kpcp-local-e2e' -or
    $qaReleaseOwnership.cleanupCompleted -or
    [IO.Path]::GetFullPath($qaReleaseSession.holdFile) -ne $qaExpectedHold -or
    [DateTimeOffset]::Parse($qaReleaseSession.expiresAt) -le [DateTimeOffset]::UtcNow) { throw '会话归属/路径/期限不匹配；禁止释放' }
$qaHoldItem = Get-Item -LiteralPath $qaExpectedHold -ErrorAction Stop
if ($qaHoldItem.PSIsContainer -or ($qaHoldItem.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'hold 文件类型不安全；停止' }
$qaReleaseSession | Select-Object runId, environmentName, holdFile
# 人工确认输出正是本次验收 runId，然后只删除这一个非递归 hold 文件。
Remove-Item -LiteralPath $qaExpectedHold -ErrorAction Stop
```

回到终端 A，等待 `BROWSER_SESSION=PASS`、`SUITE_BROWSER=PASS`、`CLEANUP=PASS`、`LOCAL_E2E=PASS`；命令结束后立即查看 `$LASTEXITCODE`，要求 0。信号在 `summary.txt` 中以 `名称=PASS` 记录，终端为 `[PASS] 名称`。保存该次 `summary.txt`、`summary.json`、`ownership.json` 和 `browser-session.json`；元数据含本机路径，交付前检查，**不要附 kubeconfig、证书或凭据**。

```powershell
Get-Content -LiteralPath (Join-Path $qaArtifactDir 'summary.txt')
$qaFinalOwnership = Get-Content -LiteralPath (Join-Path $qaArtifactDir 'ownership.json') -Raw | ConvertFrom-Json
$qaFinalOwnership | Select-Object runId, cleanupCompleted, clusters, buckets, processes, stateRoot, ownsLocalStack
Test-Path -LiteralPath $qaFinalOwnership.stateRoot
kind get clusters
docker ps --format '{{.Names}} {{.Status}} {{.Ports}}'
docker exec $qaFinalOwnership.localStackContainer awslocal s3api list-buckets --query 'Buckets[].Name' --output json
```

要求 `cleanupCompleted=True`、stateRoot 不存在、ownership 中集群/桶不存在；脚本的 CLEANUP 检查包含进程身份及残留验证，不能仅凭 PID 未出现猜测成功。与启动前基线核对：既有集群及外部 LocalStack 仍在、`ownsLocalStack=False`。前端由本次 QA 新开时，仅在终端 B 按 Ctrl+C；复用的前端保持。

若删除 hold 被工具策略阻止，记 NON-PRODUCT BLOCKER，保留阻止信息并交环境负责人处理这个准确路径，勿绕过策略。清理 FAIL 时保存 ownership、summary、logs，升级处理；不要执行全局删集群/删桶/杀进程。`-Suite clean` 会扫描多次历史归属记录，不能作为“只清当前会话”的快捷命令。会话脚本 PASS 不自动使失败的网页用例 PASS。

### 3.4 TC09–TC11 自动化回归执行

**前置条件**：3.1.1 的工具、Docker、Kind、外部 LocalStack 可用，仓库根目录；浏览器会话已释放并完成清理。三套测试不需要 Ollama/UI，不手工重启进程、编辑集群或执行 Terraform。一次运行一套，不运行 `-Suite all`。

以下为 QA **后续执行命令**，本次文档任务未运行。先在同一终端保存已有输出目录列表，再仅执行需要的一条命令：

```powershell
$qaRunsBefore = @(Get-ChildItem -LiteralPath 'artifacts/e2e' -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty FullName)
# TC09：验证后台服务重启后，正在执行的任务能够恢复，且不会重复执行。
pwsh -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite recovery -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack
```

```powershell
# TC10：验证 A/B 操作落到正确目标，另一目标不被修改。
$qaRunsBefore = @(Get-ChildItem -LiteralPath 'artifacts/e2e' -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty FullName)
pwsh -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite multitarget -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack
```

```powershell
# TC11：验证未注册目标、不完整可信发现、持久变更围栏安全停止。
$qaRunsBefore = @(Get-ChildItem -LiteralPath 'artifacts/e2e' -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty FullName)
pwsh -NoProfile -ExecutionPolicy Bypass -File e2e/Run-LocalE2E.ps1 -Suite failclosed -LocalStackEndpoint http://127.0.0.1:4566 -BuildParallelism 1 -RequireExistingLocalStack
```

每条运行结束**立即**执行以下收集步骤。若多个新目录出现，停止并根据终端输出人工确认，不能随便选择“最新”目录。

```powershell
$qaRegressionExitCode = $LASTEXITCODE
$qaNewRuns = @(Get-ChildItem -LiteralPath 'artifacts/e2e' -Directory | Where-Object FullName -notin $qaRunsBefore)
if ($qaNewRuns.Count -ne 1) { throw '本次回归目录无法唯一识别；核对命令输出' }
$qaRegressionDir = $qaNewRuns[0].FullName
Get-Content -LiteralPath (Join-Path $qaRegressionDir 'summary.txt')
Get-ChildItem -LiteralPath (Join-Path $qaRegressionDir 'logs') -File | Select-Object Name
"exitCode=$qaRegressionExitCode; evidence=$qaRegressionDir"
```

| TC | 预期信号（summary.txt） | 证据 / FAIL 判据 |
|---|---|---|
| TC09 | `RESTART_RECOVERY=PASS`、`RECOVERY_AFTER_PLAN=PASS`、`SUITE_RECOVERY=PASS` | 活跃任务、已完成 Plan、Ready 后重启恢复；计划/Apply 各 1，无重复执行。保存 `recovery-completed-plan.json`、summary、logs |
| TC10 | `MULTI_TARGET=PASS`、`SUITE_MULTITARGET=PASS` | 两个 Kind 目标资源无交叉，任一交叉或断言不满足为 FAIL；保存 summary、logs |
| TC11 | `FAIL_CLOSED=PASS`、`SUITE_FAILCLOSED=PASS` | 无意外运行资源变更；保存 `failclosed-runtime.json`、summary、logs。错误/旧审批是其他既有回归覆盖，不应声称本套直接覆盖 |

每套同时要求退出 0、`CLEANUP=PASS`、`LOCAL_E2E=PASS`。只有全部要求满足才记新 PASS；脚本断言失败记 FAIL；依赖/策略使测试无法完成记 BLOCKED，并记录原始脚本结果（包括 BLOCKED_LOCAL_ENVIRONMENT 或 FAIL）及原因，不改写日志。执行记录填本套 Run ID、命令、退出码、实际信号、证据绝对/相对路径；保留 `summary.json`、`ownership.json`，失败时附已有 `failure*.json` 和失败日志。默认已持久保存到 `artifacts/e2e/<run-id>/`，无需修改脚本收集日志。已有报告中的复用 PASS 不复制到新记录。

仅执行回归时，复制空白模板到 `artifacts/qa/<本套 run-id>/STAGE2_MANUAL_TEST_RECORD.md`，填写对应 TC 行及 A 中的回归 Run ID；其他未尝试用例保留 NOT RUN，BrowserSession 信息注明本次未使用。使用同一网页验收记录时，分别填三个回归 Run ID，不能把它们当作网页会话 Run ID。

### 3.5 QA 故障排查

| 现象 | 检查 | 下一步 |
|---|---|---|
| 网页打不开 / API 未连接 | 终端 B 的 Local URL；5173 监听；本次 `/api/health` 与终端 A 是否仍运行 | 核对 3.1.1；保存启动错误，端口/进程问题交环境负责人，勿任意终止服务 |
| AI 草稿失败 | 11434 `/api/version`、`/api/tags`；模型名；网页 Draft validation failed / 草稿校验失败详情 | 截图并保存 API 日志；外部模型不可用记 BLOCKED，实际输出/校验不符记 FAIL |
| 环境一直 Pending / PlanRunning | 本次名称/命名空间；Terraform runs 状态；观察起止时间 | 到 15 分钟保存环境 JSON、页面及 `logs/`；记录失败或已确认的环境阻塞 |
| Plan 失败 | Plan outcome、reason、条件及 Evidence pending / 等待证据 | 保存 Terraform runs 错误及当前会话日志，不审批、不反复提交 |
| 审批按钮不可用 / 审批被拒绝 | 架构预览右侧当前 Plan Ready / ChangesPresent、证据就绪、到期时间、是否已批准、是否 Destroy | Waiting for Plan evidence / 等待 Plan 证据时继续等；No changes to approve / 没有需要审批的变更不属于本流程；过期记录阻塞并准备新有效会话，不反复批准 |
| Ready 未出现 | Apply state/outcome；概览 Infrastructure/Runtime，最新条件 | 到观察上限保存现状并记缺陷，后续删除测试注明依赖状态 |
| 删除停滞 | 新 Destroy Plan 是否单独审批；Destroy 执行及 Deleting 状态 | 未审批先走 TC13；已审批超时保存错误，勿直接删内部资源或 finalizer |
| 会话过期 / 清理失败 | `expiresAt`、summary、ownership；启动终端是否结束 | 新测试重新准备；失败清理升级给环境负责人，只处理归属明确的资源 |

### 3.6 QA 证据保存与交付

填写记录 A–E：本次版本和环境、每例实际结果/状态/证据、缺陷复现步骤、截图和回归日志、清理结果、测试/审核结论。过程文件放在已忽略的 `artifacts/qa/<run-id>/`。完成审核后，要保留并交付的截图、记录和日志复制到未忽略的 `artifacts/qa-deliveries/<run-id>/`；两个目录深度相同，执行记录中的 `../../../docs/...` 链接仍指向仓库文档。

在仓库根目录运行；只复制本次最终文件，交付前检查日志和结果文件，移除凭据、证书及本机敏感信息：

```powershell
$qaDeliveryDir = Join-Path $qaRepoRoot "artifacts/qa-deliveries/$($qaSession.runId)"
New-Item -ItemType Directory -Path $qaDeliveryDir -Force | Out-Null
Copy-Item -LiteralPath $qaRecordPath -Destination $qaDeliveryDir
$qaFinalScreenshots = @(Get-ChildItem -LiteralPath $qaEvidenceDir -Filter 'TC*.png' -File)
if ($qaFinalScreenshots.Count -eq 0) { throw 'No final QA screenshots found' }
Copy-Item -LiteralPath $qaFinalScreenshots.FullName -Destination $qaDeliveryDir
if (Test-Path -LiteralPath (Join-Path $qaEvidenceDir 'manual-checks.txt')) {
  Copy-Item -LiteralPath (Join-Path $qaEvidenceDir 'manual-checks.txt') -Destination $qaDeliveryDir
}
Get-ChildItem -LiteralPath $qaDeliveryDir -File | Select-Object Name, Length
git check-ignore -v (Join-Path $qaDeliveryDir 'STAGE2_MANUAL_TEST_RECORD.md')
```

`git check-ignore` 对交付目录预期无匹配并返回 1；忽略中的过程目录不要提交。检查副本及选定日志后，明确暂存本次交付目录里的文件，再审核列表和差异：

```powershell
$qaDeliveryFiles = @(Get-ChildItem -LiteralPath $qaDeliveryDir -File | Select-Object -ExpandProperty FullName)
git add -- $qaDeliveryFiles
git diff --cached --name-only
git diff --cached --check
```

审查暂存列表，只保留本次已验收的记录、截图与必要日志，再按项目交付流程提交。不要递归暂存 `artifacts/`，也不要复制临时状态、kubeconfig、证书或未经检查的原始日志。未交付的过程文件留在被忽略的 `artifacts/qa/`。纸面审查不填写运行 PASS。

## 4. 测试用例设计

| 用例 ID | 优先级 | 测试点 | 前置条件 | 操作步骤 | 预期结果 |
|---|---|---|---|---|---|
| TC01 | P0 | AI 生成环境草稿 | API 连接；Ollama 模型可用；Ready 类别 | 按 3.1 填表 → 生成草稿 → 核对结果 | 草稿 Validated；Provider 为 Ollama；会话模型为 phi4-mini；明确参数保持正确 |
| TC02 | P0 | 草稿不自动创建资源 | 记录本次环境/执行数量 | 3.1.2：生成草稿 → 不提交 → 查看列表与只读执行记录 | 环境数量不变；没有基础设施执行 |
| TC03 | P0 | 提交环境 | 有效草稿；唯一环境名 | 审核草稿 → 点击 Submit → 打开环境 | 提交成功；环境出现；状态开始推进 |
| TC04 | P0 | Plan 生成并等待审批 | 环境已提交；人工审批模式 | 等待 Plan → 不审批 → 查看状态 | Plan 完成；显示等待审批；Apply 未开始 |
| TC05 | P0 | 未审批不得执行 | 变更正在等待审批 | 手动 UI：保持未审批 → 只读核对 Apply 数和本次管理桶；错误/不匹配审批看既有自动回归证据 | 当前网页不得出现 Apply、不得提前创建基础设施；错误/不匹配审批不要求手工构造请求 |
| TC06 | P1 | PlanTopology 展示 | 场景拆分为 TC06-A / TC06-B | 见两个子项，不单独重复执行 | 分别记录基础与高级结果，保留原 TC06 追溯 |
| TC06-A | P1 | PlanTopology 基础展示 | 普通 BrowserSession 已完成 Plan | 3.2：开架构预览 → 核对当前资源/动作/已有关系 → 切换语言/宽度 | 当前真实 Plan 正确显示，无阻塞错误；基础夹具 1 CREATE/0 边可通过 |
| TC06-B | P1 | 多资源三动作展示 | PREPARATION REQUIRED；开发交付真实多资源夹具 | 3.2：核对 CREATE / UPDATE / REPLACE、属性和连线 | 三种动作各 1，3 节点/2 连线；单资源证据不能代替 |
| TC07 | P0 | 审批后执行 | 当前方案可审批且未过期 | 点击批准当前方案 → 查看执行状态 | 有效审批接受；开始 Apply；状态推进 |
| TC08 | P0 | 环境 Ready | Apply 已开始 | 等待完成 → 检查概览与目标资源 | 环境、基础设施、运行时 Ready；预期资源存在 |
| TC09 | P0 | 服务重启恢复 | 3.4 自动化前置条件 | 运行 recovery → 保存退出码、信号、日志 | 恢复且不重复执行；套件、总结果与清理 PASS |
| TC10 | P0 | 多环境/目标隔离 | 3.4 自动化前置条件 | 运行 multitarget → 保存退出码、信号、日志 | 资源仅在正确目标；套件、总结果与清理 PASS |
| TC11 | P0 | 异常场景安全保护 | 3.4 自动化前置条件 | 运行 failclosed → 保存退出码、信号、日志 | 不匹配配置安全停止，无运行资源变更；套件、总结果与清理 PASS |
| TC12 | P0 | 删除环境 | 环境 Ready | 点击删除 → 输入完整名称 → 请求删除 | 请求接受；显示删除中；清理前环境仍可查看 |
| TC13 | P0 | Destroy 独立审批 | 已请求删除；创建审批已存在 | 不做新审批 → 查看 Destroy → 单独批准 | 创建审批不能沿用；新 Destroy 审批接受后才销毁 |
| TC14 | P0 | Destroy 完成 | Destroy 已单独审批 | 等待完成 → 查环境列表和目标资源 | 基础设施和运行资源消失；环境移除 |
| TC15 | P0 | Cleanup 无残留 | 本次测试已结束；记录既有 `pcp-dev` 与外部服务 | 释放会话 → 查看清理结果 → 核对本次资源 | 本次桶、集群、进程、临时目录不存在；既有开发环境与外部服务保留 |

保留 15 个原用例 ID；TC06 拆成两个独立场景，共 16 个执行项，TC06 父项只汇总、不重复计数。UI 关键测试为交叉标签，共 9 项：TC01、TC03、TC04、TC06-A、TC07、TC08、TC12、TC13、TC14；TC06-B 专项另计。

当前截图单独计数；关键 UI 用例 9 项，当前覆盖须达到 80%，历史图不能计入该门槛。

执行结果与图片见 [Stage 2 验证报告](STAGE2_VALIDATION_REPORT.md)。
