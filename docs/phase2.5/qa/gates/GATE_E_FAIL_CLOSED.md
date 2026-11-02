# Gate E — Fail-Closed

## PR #3 后独立批次 — 2026-11-02：五项 live 保护 PASS

Batch `kpcp-final-20261102-1815`；clean Controller `7cdad59ef8035391cafc1e4d8727ebe715fd9f32`，driver `f4e067d`（相关产品/E2E 源码一致）。E/F lane 实际 1006.39 秒、exit 0；F04 单独 SKIP/BLOCKED，不能把该 exit 0 当作完整恢复 PASS。

| 场景 | 真实操作与结果 | 状态 |
|---|---|---|
| E01 Wrong Account | dummy Secret 为 `111111111111`、配置绑定 `000000000000`；实际 Controller 输出 account/identity mismatch；零 TerraformRun、零 Apply/cloud mutation。独立 dummy STS 响应仅作 emulator 范围证据 | PASS，16.71 秒；不证明生产 IAM 隔离 |
| E02 Git Transport Outage | 先真实成功 Plan；只修改本轮 Git Service selector，确认 ready endpoints 为零；真实 clone Job 失败，无旧 source fallback/Apply；恢复同 UID Service 后生成不同 UID 的新 Plan，仍需新审批 | PASS，142.83 秒 |
| E03 Foreign Job | 本轮 namespace 中创建相同 Run label、不同 ConfigMap owner 的真实完成 Job；Controller 判 Indeterminate，没有接管、更换 owner 或替换 Job，精确 UID fixture 普通删除 | PASS，21.52 秒 |
| E04 Ambiguous Apply | 已批准 saved Plan 的真实 Apply 创建 QA bucket；仅 terminal PUT 被 403 拒绝，实际对象缺失，Controller Indeterminate；重启后原 Run/Job 身份保留，无重复 Apply，普通删除停在 RecoveryRequired | PASS（保护行为），109.81 秒 |
| E05 Partial Mutation | 两个固定、有依赖的 QA bucket；第二个 Create PUT 精确 403，第一个已真实创建；Terraform Apply 非零退出、mutationMayHaveOccurred=true；无盲重试，两个业务 finalizer 保留 | PASS（保护行为），93.80 秒 |

[49 项完整脱敏 E/F 收据](../evidence/final-acceptance-20261102/gate-ef.public.json)、[真实命令输出](../evidence/final-acceptance-20261102/gate-ef.selected.public.txt)。Run/Job/Plan/Approval UID、执行上下文与 artifact digest、实际 Pod 日志及 cloud before/after 均在独立子场景收据中；完整原始证据只保留本地。

**Gate E 本轮要求的 fail-closed 保护 PASS；正常恢复仍受 F04 阻塞。** E04/E05 不是普通 Plan failure，未将未完成的人工恢复算作 PASS。四条保留链见[身份及归档清单](../evidence/final-acceptance-20261102/retained-resources.public.json)；完整 Freeze 因 F04 / cleanup BLOCKED 为 NO-GO。下方历史 NOT RUN/PARTIAL 保持原批次身份。

## 1. Test Objective

缺少可信 source、target、execution identity、approval 或 terminal evidence 时，不执行未经授权的 Apply、不重复 mutation、不影响非归属资源。

## 2. Preconditions

独占 QA Run、固定 source/image、本地 MiniStack dummy 账户。503 只影响测试代理中精确匹配的本轮 artifact bucket；不对共享 MiniStack 注入全局故障。完整 kubeconfig/Secret 不公开。

## 3. Test Matrix

| 场景 | 本轮结果 | 证据边界 |
|---|---|---|
| invalid Git revision | PASS | 真实 Plan Job 失败，没有 Apply |
| unavailable emulator endpoint | PASS | 测试 Class 使用独立错误 endpoint，真实 Job 失败；共享 emulator 正常 |
| unknown context / endpoint profile mismatch / unknown registry field | PASS | 真实 Controller 启动 exit 1，manager 未启动，Job UID 集合不变 |
| target incarnation UID mismatch | PASS | 实际读取 live kube-system UID 后拒绝错误注册身份 |
| credential UID/RV mismatch | PASS | 真实 A/B 独占 Secret drift，Apply rejected |
| scoped S3 HTTP 503 | PASS | Controller host direct，Pod 专属代理，Destroy Plan Job 真正失败，恢复后新 Plan 需独立审批 |
| missing/corrupt evidence | PASS | Gate B live matrix |
| foreign active Job / ambiguous Apply / mutation fence | 单测 PASS | fake-client/reconstruction 测试；未声称真实故障注入 |
| wrong account / execution profile | 单测覆盖；live NOT RUN | 当前仅 dummy MiniStack 账户 |
| unavailable Git transport | NOT RUN | invalid revision 不等于 Git server 故障 |
| partial mutation 后重试 / live foreign Job | NOT RUN | 不能用正常重启或 Plan failure 替代 |

## 4. Execution Steps

1. 只读核对集群、资源所有权和 emulator 健康；保留原资源身份。
2. 为 invalid source 和错误 emulator endpoint 分别创建独立 Class/环境，等待真实 Job 失败；断言零 Apply。
3. 启动真实 Controller binary，依次给错误 incarnation/context/endpoint/未知字段；捕获原始错误和非零退出，比较 Job UID 集合。
4. 按 Gate B/F 在精确 QA object/bucket 上注入损坏或 503；恢复故障开关；检查审批/Job/side effect。
5. 运行拒绝 ambiguous/foreign Job 的 Go 单测，单独标为 controlled/fake-client 证据。

## 5. Expected Results

未知目标不得准入；执行状态不明确时保留证据和 finalizer；恢复不盲目复制 Terraform mutation。

## 6. Actual Results

本轮 `E_FailClosed` 真实执行 PASS（18 秒）；四项独立真实启动拒绝 PASS（1.31 秒）。后者是注册入口拒绝，不能替代“已准入业务运行中目标被替换”的端到端故障。Go 单测/race/vet 通过。真实 Ollama unavailable 的无 Draft/无 Submit 证据另见 TC03，模型质量仍 NOT RUN。

## 7. Evidence

- [source/endpoint 结果](../evidence/final-gates-20261102/e2e-host-current.selected.log)
- [启动拒绝原始错误与 exit code](../evidence/final-gates-20261102/e2e-startup-rejections.receipts.public.json)
- [启动拒绝命令](../evidence/final-gates-20261102/e2e-startup-rejections.command.public.json)
- [503 后失败及恢复](../evidence/final-gates-20261102/e2e-fault-corrected.selected.log)
- [完整测试命令与显式 SKIP](../evidence/final-gates-20261102/clean-build.public.json)

## 8. Failure / Recovery Behavior

首轮把 host 和 Pod 请求一起 fault，Controller 在创建 Job 前 fail closed，测试等待 Job 超时，因此全套 FAIL。修正夹具后 host 直连、仅 Pod 请求进专属代理，真实失败 Job 和恢复 PASS。没有放宽产品校验。错误尝试和传输修正收据保留。

## 9. Remaining Coverage

live wrong-account/profile、Git 传输不可用、外来 active Job、ambiguous/partial Apply 后重试矩阵未关闭。单位测试不能替代这些真实故障。生产 AWS/IAM、网络分区和跨版本对象存储也未验收。

## 10. Final Verdict

**PARTIAL**。已执行边界均 fail closed；完整 live 负例矩阵仍有明确缺口。

## 11. 2026-11-02 Final Closure Attempt

| 本轮要求场景 | 本轮结果 | 证据 / 原因 |
|---|---|---|
| Wrong MiniStack account | BLOCKED | 目前 emulator 只有当前 dummy context；本轮未伪造可隔离的 account boundary |
| Git transport outage | BLOCKED | Pod 可达 Git source server 未运行，不能把现有无 source 当作故障注入后的可信基线 |
| Foreign active Job | BLOCKED | 未创建 QA-owned Controller/Job，避免触碰 shared Job |
| Ambiguous Apply / partial mutation | BLOCKED | 无完整隔离 Plan/Runner/backend transport；不启动未经验证的 mutation |
| Partial mutation 后恢复 | BLOCKED | 无 QA-owned cloud object 或并发隔离 transport |

本轮没有执行任何 Apply，也没有向 MiniStack 写对象。集群、Runner image cache、当前 MiniStack identity 与缺少的 Pod transport 见[只读预检收据](../evidence/deepseek-final-20261102/infrastructure-preflight.public.json)。

**本轮新增场景 BLOCKED；Gate E 仍 PARTIAL。** 原有 startup/source/endpoint/scoped 503 live 结果和 unit-only 行为保持各自范围。
