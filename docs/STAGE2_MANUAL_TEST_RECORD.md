# KPCP Stage 2 手动测试执行记录（空白模板）

文档审查基线：`main` / `acd00679f05bc1c5de465ec775d2a02513253c06`，2026-10-08。**此基线不是本次执行版本；执行版本在 A 填写。**

使用方式：按[测试设计](STAGE2_TEST_DESIGN.md)复制到本次 `artifacts/qa/<run-id>/STAGE2_MANUAL_TEST_RECORD.md` 后填写；按指南把副本的 3 个文档链接改为指向仓库 `docs/`，否则副本中的链接不成立。历史结果在[验证报告](STAGE2_VALIDATION_REPORT.md)，不能继承到本表。冻结状态见[Freeze 证据](STAGE2_FREEZE_EVIDENCE.md)，当前 `STAGE2_FREEZE = BLOCKED`。

## A — 执行信息

| 字段 | 值 |
|---|---|
| 测试人员（Tester） | |
| 日期/时间及时区（Date/time） | |
| Branch | |
| HEAD SHA | |
| BrowserSession Run ID | |
| Ollama 版本 | |
| 模型（Model） | |
| UI URL | |
| API URL | |
| 测试环境（Kind / 外部 LocalStack） | |
| 会话元数据路径 / 到期时间 | |
| 环境名称 / 管理命名空间 | |
| Environment class / Region / Node count | |
| 目标运行命名空间 / Service | |
| 终端 A 本次输出目录 / holdFile | |
| 启动前已有集群与外部服务基线 | |
| 浏览器版本 / 语言 / 窗口宽度 | |
| 本次证据目录 | |
| TC06-B 夹具交付人 / 准备版本 / 交付路径 | |
| TC09 / TC10 / TC11 各自 Run ID | |

## B — 测试结果

状态仅可填：**PASS、FAIL、BLOCKED、NOT RUN、NOT APPLICABLE**。

- PASS：实际行为满足全部预期且证据完整；不得用历史图或后端 PASS 代替本次 UI 结果。
- FAIL：实际行为与预期不符，填写缺陷及复现证据。
- BLOCKED：已尝试执行或准备，但外部依赖、工具或缺失夹具阻止执行；写明阻塞原因、责任人及已执行到哪一步。
- NOT RUN：尚未尝试；与已遇到阻塞不同。
- NOT APPLICABLE：经审核确认不属于本次范围；填写原因，不能用来掩盖缺失证据或未准备的 TC06-B。

| TC ID | 测试点 | 实际结果 | 状态 | 证据路径 | 缺陷 ID |
|---|---|---|---|---|---|
| TC01 | AI 生成环境草稿 | | NOT RUN | | |
| TC02 | 草稿不自动创建资源 | | NOT RUN | | |
| TC03 | 提交环境 | | NOT RUN | | |
| TC04 | Plan 生成并等待审批 | | NOT RUN | | |
| TC05 | 未审批不得执行 | | NOT RUN | | |
| TC06 | PlanTopology 展示（父项汇总） | | NOT RUN | | |
| TC06-A | PlanTopology 基础展示 | | NOT RUN | | |
| TC06-B | 多资源三动作展示 | | NOT RUN | | |
| TC07 | 审批后执行 | | NOT RUN | | |
| TC08 | 环境 Ready | | NOT RUN | | |
| TC09 | 服务重启恢复 | | NOT RUN | | |
| TC10 | 多环境/目标隔离 | | NOT RUN | | |
| TC11 | 异常场景安全保护 | | NOT RUN | | |
| TC12 | 删除环境 | | NOT RUN | | |
| TC13 | Destroy 独立审批 | | NOT RUN | | |
| TC14 | Destroy 完成 | | NOT RUN | | |
| TC15 | Cleanup 无残留 | | NOT RUN | | |

TC06-A 与 TC06-B 分别判定。父项仅列子项结论，不单独计数；16 个独立执行项中关键 UI 为 9 项，使用 TC06-A，TC06-B 另行专项验收。TC09–TC11 填自动化命令、Run ID、退出码、实际 PASS/FAIL 信号、清理信号及日志路径。每例填写实际起止时间；短暂状态未捕获时写明后续证据，不编造截图。

## C — 缺陷

| 缺陷 ID | TC ID | 描述 | 严重级别 | 复现步骤 | 证据 |
|---|---|---|---|---|---|
| | | | | | |

描述包含预期/实际差异、发生阶段与本次环境名；复现步骤写输入和按钮。按项目缺陷规则填写 P0/P1/P2 等严重级别。环境/工具问题另写明 BLOCKED 或 NON-PRODUCT BLOCKER，保留原始错误；不要据此宣称产品缺陷或更改原日志。

## D — 证据清单

- [ ] `TC01-ai-draft-ollama.png`：校验通过、Ollama、正确草稿字段。
- [ ] `TC02-before-submit.png`：草稿和生成前后的环境计数；附 before/after 环境及执行 JSON。
- [ ] `TC03-environment-created.png`：本次环境名称、命名空间、类别、创建后状态；必要时补列表图。
- [ ] `TC04-awaiting-approval.png`：WaitingApproval 和完成的 Plan；附 TC05 的零 Apply / 桶不存在检查。
- [ ] `TC06-plan-topology.png`：TC06-A 当前真实资源和动作；附语言/窄屏检查。
- [ ] TC06-B 已执行时附 `TC06-B-plan-topology.png`、真实计划及夹具交付证据；未执行写明原因。
- [ ] `TC07-approved-applying.png`：审批接受与 Apply 执行证据。
- [ ] `TC08-ready.png`：环境/基础设施/运行时 Ready；附本次桶及 Service 存在检查。
- [ ] `TC13-destroy-approval.png`：新 Destroy Plan 独立等待审批，支持 TC12/TC13；附批准后图及必要 JSON。
- [ ] `TC14-environment-removed.png`：移除提示与列表；附本次桶/Service 不存在检查。
- [ ] TC09–TC11 已执行时，各附 summary、logs、ownership、命令与退出码；未执行分别写明状态。
- [ ] 本次 `browser-session.json`、会话 summary、ownership；不附 kubeconfig/证书/凭据。
- [ ] 正常释放/清理信号、`cleanupCompleted`、本次资源不存在及既有资源保留对照。
- [ ] 所有填写的证据文件存在，可打开，属于本次 Run ID；共享截图的可见内容足以支持对应 TC。

## E — 测试结论

| 项目 | 数量 / 内容 |
|---|---|
| 通过用例（PASS） | |
| 失败用例（FAIL） | |
| 阻塞用例（BLOCKED） | |
| 未执行用例（NOT RUN） | |
| 不适用用例及批准原因 | |
| 未关闭缺陷 / 阻塞 | |
| 当前关键 UI 有效截图用例数 / 9、覆盖率 | |
| 同次正常释放及清理结论 | |
| 测试人员结论 / 签名 / 日期 | |
| 审核人员结论 / 签名 / 日期 | |

结论列明未执行范围和遗留问题；16 个执行项不重复计 TC06 父项。关键 UI 80% 门槛至少需 8/9 个当前有效截图用例，但覆盖率本身不能代替用例通过或完整 Ollama 生命周期与正常清理证据。模板不填写执行通过结论；没有实际验收证据，Freeze 保持 BLOCKED。
