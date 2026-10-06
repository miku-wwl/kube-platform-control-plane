# Ollama + Phi-4-mini 接入验证

验证日期：2026-10-06（Pacific/Auckland）。本轮迁移 AI 接入；此前待完成的 Local E2E 工作保留，等待 review 后继续。

## 1. 测试验证项目

| 项目 | 结果 |
| --- | --- |
| 本机 Ollama 与已安装模型 | PASS：Ollama `0.35.1`，`phi4-mini:latest`，3.8B / Q4_K_M |
| 真实推理 | PASS：英文小型环境、英文缓存 + 2 节点、中文启用缓存、中文不要缓存、明确表单参数、模糊描述保守处理 |
| 明确参数优先 | PASS：描述“小型环境”时，表单明确指定的名称、区域及 2 节点仍保持原值；使用输出 schema 的 `const` 约束，并继续校验结果 |
| 模型不存在 | PASS：真实 Ollama 返回 HTTP 404，生成器报错，无自动切换提供器 |
| 异常输出 | PASS：单元测试拒绝非 JSON、未知/缺少/null 字段、错误类型/范围、未完成/截断响应、tool calls、超大响应；服务不可达时报错 |
| 网页代理 → API → Ollama → Kind 校验 | PASS：3 次真实请求均为 `provider=ollama`、`valid=true`，返回类型化 PlatformEnvironment YAML；临时类别由真实控制器变为 Ready |
| 草稿不自动创建资源 | PASS：真实 Kind 中环境数量前后均为 0；临时调试类别已删除 |
| Go 测试与构建 | PASS：`go test -p 1 ./... -count=1`、`go build -p 1 ./...` |
| 前端 build / typecheck / lint | PASS：三个命令退出 0；本仓库 lint 脚本为 `tsc --noEmit` |
| 浏览器页面点击与截图 | NOT VERIFIED：浏览器插件无已连接浏览器；Chrome 和内置浏览器均不可用。本轮通过网页代理调用真实 API，未将其算作浏览器点击验证 |
| Submit、Plan、Apply、Destroy、PlanTopology | 本轮未执行；按要求等 review 后再继续此前的指令 |

临时类别仅验证草稿和类别准入，其 backend / runner 引用未作为可执行基础设施验证。本轮控制器保持 `PCP_ENABLE_TERRAFORM_EXECUTION=false`。

## 2. 启动参数

Ollama 已运行且模型已安装时，API 默认即使用以下值：

```powershell
$env:AI_PROVIDER = "ollama"
$env:OLLAMA_ENDPOINT = "http://127.0.0.1:11434"
$env:OLLAMA_MODEL = "phi4-mini:latest"
Invoke-RestMethod "$env:OLLAMA_ENDPOINT/api/tags"
# 设置与控制器相同的 KUBECONFIG 后启动：
go run ./cmd/platform-api
```

`OLLAMA_ENDPOINT` 填服务根地址，不加 `/api` 或 `/v1`；模型填 `/api/tags` 中实际安装的标签。已安装无需重新下载。旧 `AI_PROVIDER=foundry-local` 应改为 `ollama`，旧 `FOUNDRY_LOCAL_*` 配置不再使用。

```powershell
npm --prefix web run dev
Invoke-RestMethod http://127.0.0.1:8090/api/health
```

检查 `aiProvider` 为 `ollama`，网页地址为 `http://127.0.0.1:5173`。

## 3. 网页手动复核步骤

1. 打开网页，进入平台构建器，选择一个 **Ready** 环境类别。类别为空时先通过环境类别管理页面创建；节点上限应至少为 2，允许区域应包含所填区域。
2. 输入下表内容；环境名使用未占用的新名称，命名空间填已存在的 `default`。区域以选定类别的允许值为准，本轮 Kind 临时类别使用 `local`。
3. 点击 **生成草稿 / Generate draft**。
4. 检查提供器显示 **Ollama**，草稿通过校验，名称、命名空间、类别、区域、节点数量与表单一致；检查缓存设置与描述一致，YAML 的 `kind` 为 `PlatformEnvironment`。
5. 刷新环境列表，生成草稿本身应不增加环境。本轮复核止于草稿，Submit 及后续生命周期待下一轮执行。

| 自然语言需求 | 环境名示例 | 节点数量 | 缓存预期 | 本轮真实 API 结果 / 耗时 |
| --- | --- | --- | --- | --- |
| 创建一个小型开发环境，不要缓存 | `ollama-review-cn` | 2 | 禁用 | PASS / 6.47 秒 |
| Create a development environment with cache enabled and 2 nodes | `ollama-review-cache` | 2 | 启用，1 shard / 1 replica | PASS / 6.71 秒 |
| 创建一个启用缓存的小型开发环境 | `ollama-review-cn-cache` | 1 | 启用，1 shard / 1 replica | PASS / 6.11 秒 |

## 4. 重跑真实模型检查

```powershell
$env:AI_PROVIDER = "ollama"
$env:OLLAMA_ENDPOINT = "http://127.0.0.1:11434"
$env:OLLAMA_MODEL = "phi4-mini:latest"
$env:PCP_RUN_OLLAMA_INTEGRATION = "1"
go test ./internal/console -run 'TestOllama|TestMalformedOllama' -count=1 -v
```

此命令使用真实模型和测试 Kubernetes client。第 1 节的网页代理检查另行使用了真实 Kind，原始草稿与日志保存在本机忽略目录 `tmp/ollama-migration/`。

接入代码：[ai.go](../internal/console/ai.go)。接口依据：[Ollama Chat API](https://docs.ollama.com/api/chat)、[Structured outputs](https://docs.ollama.com/capabilities/structured-outputs)。2026-10-04 的 Foundry 验收报告保留历史结果，并已注明当前接入迁移。
