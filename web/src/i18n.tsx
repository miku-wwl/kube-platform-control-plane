import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

export type Language = 'en' | 'zh'

const chinese: Record<string, string> = {
  Language: '语言',
  'Switch language to English': '切换到英文',
  'Switch language to Chinese': '切换到中文',
  Close: '关闭',
  'LOCAL WORKSPACE': '本地工作区',
  Environments: '环境',
  'Platform builder': '平台构建器',
  'LOCAL MODE': '本地模式',
  'Stage 1 engine · frozen': '阶段一引擎 · 已冻结',
  'PLATFORM ENGINEERING / LOCAL': '平台工程 / 本地',
  Environment: '环境',
  'API connected': 'API 已连接',
  Refresh: '刷新',
  'New environment': '新建环境',
  'Request could not be completed': '请求未能完成',
  'Workflow updated': '工作流已更新',
  'Loading environment evidence…': '正在加载环境证据…',
  'CONTROL PLANE OVERVIEW': '控制平面概览',
  'Understand every environment,': '掌握每个环境的状态，',
  'from intent to runtime.': '从意图到运行时。',
  'Live read model from the Kubernetes-native control plane.': '来自 Kubernetes 原生控制平面的实时视图。',
  'TOTAL ENVIRONMENTS': '环境总数',
  READY: '就绪',
  'NOT READY': '未就绪',
  'AWAITING APPROVAL': '等待审批',
  'CONTROL PLANE': '控制平面',
  ONLINE: '在线',
  'YOUR FLEET': '环境总览',
  'Platform environments': '平台环境',
  'Create environment': '创建环境',
  'Loading Kubernetes resources…': '正在加载 Kubernetes 资源…',
  'No environments yet': '还没有环境',
  'Start with a typed EnvironmentClass and a reviewed draft.': '先选择类型化的 EnvironmentClass，并检查生成的草稿。',
  'Open platform builder': '打开平台构建器',
  ENVIRONMENT: '环境',
  CLASS: '类别',
  STATE: '状态',
  'INFRA / RUNTIME': '基础设施 / 运行时',
  TARGET: '目标',
  'LATEST RUN': '最近运行',
  legacy: '旧版',
  Reconciling: '协调中',
  'No run': '暂无运行记录',
  'Live status': '实时状态',
  'Refreshes every 8 seconds': '每 8 秒刷新',
  'Kubernetes is the source of truth. AI proposes · API validates · controllers reconcile · Terraform executes.': 'Kubernetes 是事实来源。AI 提议 · API 校验 · 控制器协调 · Terraform 执行。',
  'INTENT → REVIEW → SUBMIT': '意图 → 审核 → 提交',
  'Describe the platform': '描述你需要的',
  'you need.': '平台。',
  'The assistant drafts a typed PlatformEnvironment. Existing controllers remain responsible for planning, approval, execution and runtime reconciliation.': '助手会生成类型化的 PlatformEnvironment 草稿。计划、审批、执行和运行时协调仍由现有控制器负责。',
  'Describe desired state': '描述期望状态',
  'AI output stays a proposal until you explicitly submit it.': 'AI 输出仅作为提议，只有你明确提交后才会创建资源。',
  'Natural-language request': '自然语言需求',
  'Create a small development platform with Valkey cache': '创建一个带有 Valkey 缓存的小型开发平台',
  'Describe a development or test environment…': '描述开发或测试环境…',
  'Environment name': '环境名称',
  optional: '可选',
  'generated from description': '根据描述生成',
  Namespace: '命名空间',
  'Environment class': '环境类别',
  'Select a ready class': '选择已就绪的类别',
  ' · not ready': ' · 未就绪',
  Region: '区域',
  'class default': '类别默认值',
  'Use class default': '使用类别默认值',
  'Node count': '节点数量',
  'region not fixed': '区域未固定',
  Cancel: '取消',
  'Generating…': '正在生成…',
  'Generate draft': '生成草稿',
  'Draft validation failed': '草稿校验失败',
  '02 · REVIEW BEFORE SUBMIT': '02 · 提交前审核',
  'Draft preview': '草稿预览',
  'LOCAL DRAFT': '本地草稿',
  'class region': '类别区域',
  'Submitting…': '正在提交…',
  'Submit PlatformEnvironment': '提交 PlatformEnvironment',
  'Submitting creates only the top-level Kubernetes resource. No Plan is approved and no infrastructure is applied by this button.': '提交操作只会创建顶层 Kubernetes 资源。此按钮不会批准 Plan，也不会应用任何基础设施。',
  'Your typed manifest preview appears here': '类型化清单预览会显示在这里',
  'Generate a draft, review its validation result and YAML, then submit explicitly.': '生成草稿，检查校验结果和 YAML，然后明确提交。',
  '✦ AI PROPOSES': '✦ AI 提议',
  '⌕ API VALIDATES': '⌕ API 校验',
  '◉ HUMAN SUBMITS': '◉ 人工提交',
  '⟳ CONTROL PLANE RECONCILES': '⟳ 控制平面协调',
  'Delete environment': '删除环境',
  'Deleting…': '正在删除…',
  'legacy environment': '旧版环境',
  'target ': '目标：',
  pending: '等待中',
  'DELETE ENVIRONMENT': '删除环境',
  'Confirm deletion of {{environment}}': '确认删除 {{environment}}',
  'Existing finalizers will prune runtime resources and require a separate approval for the Terraform Destroy Plan.': '现有 finalizer 将清理运行时资源；Terraform Destroy Plan 仍需单独审批。',
  'Type {{name}} to request deletion': '输入 {{name}} 以请求删除',
  'Confirm environment name': '确认环境名称',
  'Request deletion': '请求删除',
  INFRASTRUCTURE: '基础设施',
  RUNTIME: '运行时',
  'RESOURCE INVENTORY': '资源清单',
  'LATEST APPROVAL': '最近审批',
  'Not submitted': '未提交',
  Overview: '概览',
  'Architecture preview': '架构预览',
  'Terraform runs': 'Terraform 运行记录',
  'Lifecycle timeline': '生命周期时间线',
  RECONCILIATION: '协调状态',
  'Current conditions': '当前条件',
  'observed gen': '已观测版本',
  'No conditions have been recorded yet.': '尚无条件记录。',
  'No condition message provided.': '没有条件说明。',
  'TRUSTED TARGET': '可信目标',
  'Discovery pending': '等待发现目标',
  Provider: '提供方',
  Account: '账户',
  Incarnation: '实例标识',
  'Connection credentials and certificate data are intentionally not exposed.': '连接凭据和证书数据不会在此显示。',
  'RUNTIME RECONCILIATION': '运行时协调',
  '{{count}} inventoried resources': '已纳入清单的资源：{{count}} 项',
  'Runtime mutation is currently fenced.': '运行时变更当前已被安全栅栏阻止。',
  '{{ready}}/{{total}} replicas ready': '{{ready}}/{{total}} 个副本就绪',
  'SAFE DESIRED-STATE UPDATE': '安全更新期望状态',
  Capacity: '容量',
  'Change node count only; the existing control plane plans and reconciles the new generation.': '这里只修改节点数量；新版本的计划和协调由现有控制平面负责。',
  'Update capacity': '更新容量',
  'DURABLE EVIDENCE': '持久化证据',
  'Latest Terraform evidence': '最近的 Terraform 证据',
  'View runs →': '查看运行记录 →',
  'No Terraform runs yet': '尚无 Terraform 运行记录',
  'The controller will create one after the environment is reconciled.': '环境完成协调后，控制器会创建运行记录。',
  'Plan digest': 'Plan 摘要',
  'Plan report': 'Plan 报告',
  'Report digest': '报告摘要',
  'Terminal result': '终端结果',
  'Source bundle digest': '源代码包摘要',
  'Target discovery digest': '目标发现摘要',
  nodes: '个节点',
  generation: '代次',
  'IMMUTABLE ATTEMPTS': '不可变执行记录',
  'Architecture nodes are derived from the immutable Terraform plan; values are allowlisted and sensitive fields are omitted.': '架构节点根据不可变 Terraform Plan 生成；属性经过允许列表筛选，敏感字段不会显示。',
  'Not recorded': '未记录',
  'PLAN IDENTITY': 'PLAN 标识',
  'Reconciliation pending': '等待协调',
  ' · changes present': ' · 存在变更',
  'Evidence ready': '证据已就绪',
  'Evidence pending': '等待证据',
  'DETERMINISTIC PLAN PROJECTION': '确定性 Plan 投影',
  'Architecture change preview': '架构变更预览',
  'Plan evidence has not been loaded.': '尚未加载 Plan 证据。',
  'Plan preview unavailable': 'Plan 预览不可用',
  'Run a Terraform Plan to see proposed resource changes here.': '运行 Terraform Plan 后，这里会显示拟议的资源变更。',
  'No resource changes': '没有资源变更',
  'This plan has no create, update, delete or replace actions.': '此 Plan 不包含创建、更新、删除或替换操作。',
  CURRENT: '当前',
  PLANNED: '计划',
  CREATE: '创建',
  UPDATE: '更新',
  DELETE: '删除',
  'DELETE / REPLACE': '删除 / 替换',
  'HUMAN REVIEW GATE': '人工审核关口',
  'Approval recorded': '审批已记录',
  'Review this exact plan': '审核此 Plan',
  'No approval action available': '当前没有可用的审批操作',
  'Plan {{digest}} · expires {{time}}': 'Plan {{digest}} · 过期时间 {{time}}',
  'The platform API creates only the existing immutable ChangeApproval binding. The existing controller decides when the matching saved plan can proceed.': '平台 API 只创建现有的不可变 ChangeApproval 绑定。是否执行匹配的已保存 Plan 由现有控制器决定。',
  'Approve exact plan': '批准此确切 Plan',
  'Waiting for Plan evidence': '等待 Plan 证据',
  'No Apply endpoint exists in this console.': '此控制台没有 Apply 接口。',
  'RESOURCE-BACKED EVENTS': '资源事件记录',
  '{{count}} entries': '{{count}} 条记录',
  'No lifecycle evidence yet': '尚无生命周期证据',
  Unknown: '未知',
  Ready: '就绪',
  Pending: '等待中',
  Required: '待审批',
  Recorded: '已记录',
  Approved: '已批准',
  Succeeded: '成功',
  Failed: '失败',
  Running: '运行中',
  ChangesPresent: '存在变更',
  NoChange: '无变更',
  Progressing: '进行中',
  Deleting: '删除中',
  Creating: '创建中',
  Blocked: '已阻止',
  True: '是',
  False: '否',
  create: '创建',
  update: '更新',
  delete: '删除',
  replace: '替换',
  'no-op': '无操作',
  'Environment removed after finalizer cleanup.': 'finalizer 清理完成，环境已移除。',
  'Approval {{name}} recorded against this exact plan. The existing controller will reconcile it.': '审批 {{name}} 已绑定到此确切 Plan。现有控制器将继续协调。',
  'PlatformEnvironment submitted. Terraform Plan and all later steps remain under the existing control plane.': 'PlatformEnvironment 已提交。Terraform Plan 及后续步骤仍由现有控制平面处理。',
  'The environment changed while this page was open. Refresh the detail and try again.': '页面打开期间环境已发生变化。请刷新详情后重试。',
  'Deletion requested. Cleanup is running through existing finalizers; this console does not execute Terraform.': '已请求删除。清理由现有 finalizer 处理；此控制台不会执行 Terraform。',
}

type Translate = (key: string, values?: Record<string, string | number>) => string
type LanguageContextValue = { language: Language; setLanguage: (language: Language) => void; t: Translate }

const LanguageContext = createContext<LanguageContextValue | null>(null)
const storageKey = 'platform-console-language'

function getInitialLanguage(): Language {
  try {
    const saved = window.localStorage.getItem(storageKey)
    if (saved === 'en' || saved === 'zh') return saved
  } catch {
    // Storage may be unavailable in restricted browser contexts.
  }
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageValue] = useState<Language>(getInitialLanguage)
  const setLanguage = useCallback((next: Language) => {
    document.documentElement.lang = next === 'zh' ? 'zh-CN' : 'en'
    setLanguageValue(next)
  }, [])
  const t = useCallback<Translate>((key, values) => {
    let message = language === 'zh' ? chinese[key] ?? key : key
    for (const [name, value] of Object.entries(values ?? {})) {
      message = message.replaceAll(`{{${name}}}`, String(value))
    }
    return message
  }, [language])

  useEffect(() => {
    try { window.localStorage.setItem(storageKey, language) } catch {
      // The selection still works for the current page when storage is unavailable.
    }
    document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en'
    document.title = language === 'zh' ? '平台控制平面' : 'Platform Control Plane'
  }, [language])

  return <LanguageContext.Provider value={{ language, setLanguage, t }}>{children}</LanguageContext.Provider>
}

export function useI18n() {
  const context = useContext(LanguageContext)
  if (!context) throw new Error('useI18n must be used inside LanguageProvider')
  return context
}
