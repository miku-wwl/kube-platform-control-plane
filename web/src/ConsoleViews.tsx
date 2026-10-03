import { useEffect, useState } from 'react'
import {
  Activity,
  ArrowRight,
  Boxes,
  CheckCircle2,
  ChevronRight,
  Clock,
  Cloud,
  Cpu,
  Database,
  FileCode2,
  GitBranch,
  HardDrive,
  Layers,
  ListChecks,
  LoaderCircle,
  Network,
  Plus,
  Search,
  Server,
  ShieldCheck,
  Sparkles,
  Terminal,
  Trash2,
} from 'lucide-react'
import {
  request,
  type ClassInfo,
  type DraftResponse,
  type Environment,
  type EnvironmentDetail,
  type PlanView,
  type TerraformRun,
  type TimelineEvent,
} from './api'
import { useI18n } from './i18n'
import { Alert, EmptyState, MetricCard, StatusBadge, LoadingState, formatTime, short } from './components/ui'
import { PlanTopology } from './components/PlanTopology'

type DetailTab = 'overview' | 'architecture' | 'terraform' | 'timeline'

export function Dashboard({
  environments,
  readyCount,
  loading,
  apiState,
  onOpen,
  onBuild,
}: {
  environments: Environment[]
  readyCount: number
  loading: boolean
  apiState: 'connecting' | 'connected' | 'unavailable'
  onOpen: (item: Environment) => void
  onBuild: () => void
}) {
  const { t } = useI18n()
  const [query, setQuery] = useState('')
  const pending = environments.filter(
    (item) => item.latestApproval === 'Required' || item.latestApproval === 'Pending'
  ).length
  const visible = environments.filter((item) =>
    [item.name, item.namespace, item.class, item.target.clusterName, item.phase].some((value) =>
      value?.toLowerCase().includes(query.toLowerCase())
    )
  )
  return (
    <div className="content">
      <div className="page-heading">
        <div>
          <div className="section-kicker">{t('CONTROL PLANE OVERVIEW')}</div>
          <h2>{t('Environments')}</h2>
          <p>{t('Live read model from the Kubernetes-native control plane.')}</p>
        </div>
        <button className="button button-primary" onClick={onBuild}>
          <Plus size={16} aria-hidden="true" />
          {t('New environment')}
        </button>
      </div>
      <section className="stat-grid" aria-label={t('Environment summary')}>
        <MetricCard label={t('Total environments')} value={loading ? '—' : environments.length} icon={Boxes} />
        <MetricCard label={t('Ready')} value={loading ? '—' : readyCount} icon={CheckCircle2} tone="good" />
        <MetricCard label={t('Awaiting approval')} value={loading ? '—' : pending} icon={ShieldCheck} tone="warn" />
        <MetricCard
          label={t('Control plane')}
          value={
            <span className="connection-value">
              <i className={`dot ${apiState === 'connected' ? 'good' : apiState === 'unavailable' ? 'bad' : ''}`} />
              {t(
                apiState === 'connected'
                  ? 'API connected'
                  : apiState === 'connecting'
                    ? 'Connecting…'
                    : 'API unavailable'
              )}
            </span>
          }
          icon={Activity}
          note={t('API reachability')}
        />
      </section>
      <section className="panel environment-panel">
        <div className="panel-heading">
          <h3>
            {t('Platform environments')} <span className="count-chip">{environments.length}</span>
          </h3>
          <label className="search-input">
            <Search size={16} aria-hidden="true" />
            <input
              aria-label={t('Search environments')}
              placeholder={t('Search environments…')}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
        </div>
        {loading ? (
          <LoadingState label={t('Loading Kubernetes resources…')} />
        ) : environments.length === 0 ? (
          <EmptyState
            icon={Boxes}
            title={t('No environments yet')}
            description={t('Start with a typed EnvironmentClass and a reviewed draft.')}
            action={
              <button className="button button-primary" onClick={onBuild}>
                <Plus size={16} aria-hidden="true" />
                {t('Open platform builder')}
              </button>
            }
          />
        ) : visible.length === 0 ? (
          <EmptyState
            icon={Search}
            title={t('No matching environments')}
            description={t('Search by name, namespace, class, target or status.')}
            action={
              <button className="button button-secondary" onClick={() => setQuery('')}>
                {t('Clear search')}
              </button>
            }
          />
        ) : (
          <div className="table-wrap" tabIndex={0} aria-label={t('Platform environments')}>
            <table>
              <thead>
                <tr>
                  <th>{t('Environment')}</th>
                  <th>{t('Environment class')}</th>
                  <th>{t('Status')}</th>
                  <th>{t('INFRA / RUNTIME')}</th>
                  <th>{t('Target')}</th>
                  <th>{t('Latest run')}</th>
                  <th>
                    <span className="sr-only">{t('Open environment')}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {visible.map((item) => (
                  <tr key={`${item.namespace}/${item.name}`}>
                    <td>
                      <button className="environment-link" onClick={() => onOpen(item)}>
                        <span className="env-avatar">
                          <Boxes size={17} aria-hidden="true" />
                        </span>
                        <span>
                          <strong>{item.name}</strong>
                          <small>
                            {item.namespace} <span className="metadata-separator">/</span> {t('generation')}{' '}
                            {item.generation}
                          </small>
                        </span>
                      </button>
                    </td>
                    <td>
                      <span className="class-pill">{item.class || t('legacy')}</span>
                    </td>
                    <td>
                      <StatusBadge value={item.phase} />
                    </td>
                    <td>
                      <div className="mini-states">
                        <span>
                          <Cloud size={12} aria-hidden="true" />
                          <i className={`dot ${item.infrastructure.toLowerCase() === 'ready' ? 'good' : ''}`} />
                          {t(item.infrastructure)}
                        </span>
                        <span>
                          <Server size={12} aria-hidden="true" />
                          <i className={`dot ${item.runtime.toLowerCase() === 'ready' ? 'good' : ''}`} />
                          {t(item.runtime)}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div className="target-cell">
                        <strong>{item.target.clusterName || '—'}</strong>
                        <small>{[item.target.provider, item.target.region].filter(Boolean).join(' · ')}</small>
                      </div>
                    </td>
                    <td>
                      {item.latestTerraformRun ? (
                        <div className="run-cell">
                          <strong>{t(item.latestTerraformRun.operation)}</strong>
                          <small>
                            {t(item.latestTerraformRun.reason || item.latestTerraformRun.outcome || 'Reconciling')}
                          </small>
                        </div>
                      ) : (
                        <span className="muted">{t('No run')}</span>
                      )}
                    </td>
                    <td>
                      <button
                        className="icon-button row-open"
                        aria-label={t('Open {{name}}', { name: item.name })}
                        onClick={() => onOpen(item)}
                      >
                        <ChevronRight size={17} aria-hidden="true" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="panel-foot">
          <span>
            <i className={`dot ${apiState === 'connected' ? 'good' : ''}`} />
            {t(apiState === 'connected' ? 'Live status' : 'Status refresh unavailable')}
          </span>
          <span>
            <Clock size={12} aria-hidden="true" />
            {t('Refreshes every 8 seconds')}
          </span>
        </div>
      </section>
      <div className="footer-note">
        <ShieldCheck size={15} aria-hidden="true" />
        {t(
          'Kubernetes is the source of truth. AI proposes · API validates · controllers reconcile · Terraform executes.'
        )}
      </div>
    </div>
  )
}

export function Builder({
  classes,
  initialClass,
  onManageClasses,
  onCancel,
  onCreated,
}: {
  classes: ClassInfo[]
  initialClass: string
  onManageClasses: () => void
  onCancel: () => void
  onCreated: (value: Pick<Environment, 'namespace' | 'name'>) => Promise<void>
}) {
  const { t, language } = useI18n()
  const [description, setDescription] = useState(() => t('Create a small development platform with Valkey cache'))
  const [name, setName] = useState('')
  const [namespace, setNamespace] = useState('default')
  const [classRef, setClassRef] = useState(initialClass)
  const [region, setRegion] = useState('')
  const [nodeCount, setNodeCount] = useState(1)
  const [draft, setDraft] = useState<DraftResponse | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const selectedClass = classes.find((item) => item.name === classRef)
  useEffect(() => {
    setDescription((current) =>
      current === 'Create a small development platform with Valkey cache' ||
      current === '创建一个带有 Valkey 缓存的小型开发平台'
        ? t('Create a small development platform with Valkey cache')
        : current
    )
  }, [language, t])
  useEffect(() => {
    if (!classes.some((item) => item.name === classRef && item.ready)) {
      setClassRef(classes.find((item) => item.ready)?.name || '')
      setRegion('')
    }
  }, [classes, classRef])

  const generate = async () => {
    setPending(true)
    setError('')
    setDraft(null)
    try {
      const value = await request<DraftResponse>('/api/drafts', {
        method: 'POST',
        body: JSON.stringify({ description, name, namespace, classRef, region, nodeCount }),
      })
      setDraft(value)
      if (!value.valid) setError(value.errors?.join('; ') || t('Draft validation failed'))
    } catch (value) {
      setError((value as Error).message)
    } finally {
      setPending(false)
    }
  }
  const submit = async () => {
    if (!draft?.valid) return
    setPending(true)
    setError('')
    try {
      await request('/api/environments', { method: 'POST', body: JSON.stringify(draft.intent) })
      await onCreated({ namespace: draft.intent.namespace, name: draft.intent.name })
    } catch (value) {
      setError((value as Error).message)
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="content builder-content">
      <div className="breadcrumb">
        <button onClick={onCancel}>{t('Environments')}</button>
        <ChevronRight size={13} aria-hidden="true" />
        <span>{t('New environment')}</span>
      </div>
      <div className="page-heading">
        <div>
          <div className="section-kicker">{t('SELF-SERVICE')}</div>
          <h2>{t('Platform builder')}</h2>
          <p>{t('Describe your environment. Review the proposed configuration before you submit it.')}</p>
        </div>
        <span className="tag">
          <ShieldCheck size={14} aria-hidden="true" />
          {t('Human confirmation required')}
        </span>
      </div>
      <div className="builder-grid">
        <section className="panel builder-form">
          <div className="panel-title">
            <span className="step-number">01</span>
            <div>
              <h3>{t('Describe desired state')}</h3>
              <p>{t('AI output stays a proposal until you explicitly submit it.')}</p>
            </div>
          </div>
          <form
            onSubmit={(event) => {
              event.preventDefault()
              void generate()
            }}
          >
            <label className="intent-field">
              <span className="field-label">{t('Natural-language request')}</span>
              <textarea
                className="text-area"
                rows={4}
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder={t('Describe a development or test environment…')}
              />
            </label>
            <div className="form-grid">
              <label>
                <span className="field-label">
                  {t('Environment name')} <small>{t('optional')}</small>
                </span>
                <input
                  className="text-input"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder={t('generated from description')}
                />
              </label>
              <label>
                <span className="field-label">{t('Namespace')}</span>
                <input
                  className="text-input"
                  value={namespace}
                  onChange={(event) => setNamespace(event.target.value)}
                />
              </label>
              <label>
                <span className="field-label">{t('Environment class')}</span>
                <select
                  className="text-input"
                  value={classRef}
                  onChange={(event) => {
                    setClassRef(event.target.value)
                    setRegion('')
                  }}
                >
                  <option value="">{t('Select a ready class')}</option>
                  {classes.map((item) => (
                    <option key={item.name} value={item.name} disabled={!item.ready}>
                      {item.name}
                      {item.ready ? '' : t(' · not ready')}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span className="field-label">
                  {t('Region')} <small>{t('class default')}</small>
                </span>
                <select className="text-input" value={region} onChange={(event) => setRegion(event.target.value)}>
                  <option value="">
                    {t('Use class default') + (selectedClass?.defaultRegion ? ` (${selectedClass.defaultRegion})` : '')}
                  </option>
                  {(selectedClass?.allowedRegions || []).map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span className="field-label">{t('Node count')}</span>
                <input
                  className="text-input"
                  type="number"
                  min={selectedClass?.capacityBounds.minNodeCount || 0}
                  max={selectedClass?.capacityBounds.maxNodeCount || 1000}
                  value={nodeCount}
                  onChange={(event) => setNodeCount(Number(event.target.value))}
                />
              </label>
            </div>
            {selectedClass && (
              <div className="class-summary">
                <Layers size={19} aria-hidden="true" />
                <div>
                  <strong>{selectedClass.name}</strong>
                  <p>
                    {selectedClass.provider} · {selectedClass.targetCluster} ·{' '}
                    {selectedClass.defaultRegion || t('region not fixed')}
                  </p>
                </div>
                <StatusBadge value={selectedClass.ready ? 'Ready' : 'Pending'} />
              </div>
            )}
            {!classes.some((item) => item.ready) && (
              <Alert tone="info">
                {t('Create a ready environment class before generating a draft.')}
                <button type="button" className="text-button" onClick={onManageClasses}>
                  {t('Manage environment classes')}
                  <ArrowRight size={14} aria-hidden="true" />
                </button>
              </Alert>
            )}
            {error && <Alert tone="error">{error}</Alert>}
            <div className="form-actions">
              <button type="button" className="button button-secondary" onClick={onCancel}>
                {t('Cancel')}
              </button>
              <button
                type="submit"
                className="button button-primary"
                disabled={pending || !classRef || !selectedClass?.ready}
              >
                {pending ? (
                  <LoaderCircle className="spinner" size={16} aria-hidden="true" />
                ) : (
                  <Sparkles size={16} aria-hidden="true" />
                )}
                {pending ? t('Generating…') : t('Generate draft')}
              </button>
            </div>
          </form>
        </section>
        <section className="panel draft-panel">
          <div className="draft-panel-head">
            <div>
              <div className="section-kicker">{t('REVIEW BEFORE SUBMIT')}</div>
              <h3>{t('AI draft')}</h3>
            </div>
            <span className="tag">
              <FileCode2 size={14} aria-hidden="true" />
              {t('Proposal')}
            </span>
          </div>
          {draft ? (
            <>
              <Alert
                tone={draft.valid ? 'success' : 'error'}
                title={draft.valid ? t('Validated') : t('Draft validation failed')}
              >
                {t('AI proposes a typed draft. Nothing is created until you submit it.')}
              </Alert>
              <dl className="draft-facts">
                <Fact
                  label={t('Provider')}
                  value={draft.provider === 'foundry-local' ? 'Foundry Local' : t(draft.provider)}
                />
                <Fact label={t('Environment')} value={`${draft.intent.namespace}/${draft.intent.name}`} />
                <Fact label={t('Environment class')} value={draft.intent.classRef} />
                <Fact
                  label={t('Region')}
                  value={
                    draft.intent.region ||
                    classes.find((item) => item.name === draft.intent.classRef)?.defaultRegion ||
                    t('class region')
                  }
                />
                <Fact
                  label={t('Capacity')}
                  value={t(draft.intent.nodeCount === 1 ? '{{count}} node' : '{{count}} nodes', {
                    count: draft.intent.nodeCount,
                  })}
                />
                <Fact label="Valkey" value={t(draft.intent.valkeyEnabled ? 'Enabled' : 'Disabled')} />
              </dl>
              {draft.warnings?.map((item, index) => (
                <Alert key={index} tone="warning" title={t('Warning')}>
                  {item}
                </Alert>
              ))}
              {draft.errors?.map((item, index) => (
                <Alert key={index} tone="error" title={t('Validation error')}>
                  {item}
                </Alert>
              ))}
              {draft.yaml && (
                <div className="code-preview">
                  <div className="code-heading">
                    <FileCode2 size={14} aria-hidden="true" />
                    {t('Proposed manifest')}
                    <span>YAML</span>
                  </div>
                  <pre className="yaml-preview">{draft.yaml}</pre>
                </div>
              )}
              <button
                className="button button-primary submit-draft"
                disabled={pending || !draft.valid}
                onClick={() => void submit()}
              >
                {pending ? t('Submitting…') : t('Submit PlatformEnvironment')}
                <ArrowRight size={16} aria-hidden="true" />
              </button>
              <p className="safe-note">
                {t(
                  'Submitting creates only the top-level Kubernetes resource. No Plan is approved and no infrastructure is applied by this button.'
                )}
              </p>
            </>
          ) : (
            <EmptyState
              icon={Sparkles}
              title={t('Your typed manifest preview appears here')}
              description={t('AI proposes a typed draft. Nothing is created until you submit it.')}
            />
          )}
        </section>
      </div>
      <div className="boundary-strip">
        <span>
          <Sparkles size={14} aria-hidden="true" />
          {t('AI proposes')}
        </span>
        <ChevronRight size={13} aria-hidden="true" />
        <span>
          <ListChecks size={14} aria-hidden="true" />
          {t('API validates')}
        </span>
        <ChevronRight size={13} aria-hidden="true" />
        <span>
          <ShieldCheck size={14} aria-hidden="true" />
          {t('Human confirms')}
        </span>
        <ChevronRight size={13} aria-hidden="true" />
        <span>
          <Activity size={14} aria-hidden="true" />
          {t('Controllers reconcile')}
        </span>
        <ChevronRight size={13} aria-hidden="true" />
        <span>
          <Terminal size={14} aria-hidden="true" />
          {t('Terraform executes')}
        </span>
      </div>
    </div>
  )
}

export function Detail({
  detail,
  plan,
  tab,
  busy,
  onBack,
  onTab,
  onApprove,
  onUpdate,
  onDelete,
  onOpenRun,
}: {
  detail: EnvironmentDetail
  plan: PlanView | null
  tab: DetailTab
  busy: boolean
  onBack: () => void
  onTab: (tab: DetailTab) => void
  onApprove: (run: TerraformRun) => Promise<void>
  onUpdate: (nodeCount: number) => Promise<void>
  onDelete: () => Promise<void>
  onOpenRun: (run: TerraformRun) => void
}) {
  const { t } = useI18n()
  const [nodeCount, setNodeCount] = useState<number | null>(null)
  const [deleteConfirming, setDeleteConfirming] = useState(false)
  const [deleteName, setDeleteName] = useState('')
  const [selectedRun, setSelectedRun] = useState('')
  const latestPlan = detail.terraformRuns.filter((run) => run.operation === 'Plan').slice(-1)[0]
  const approvalReady =
    !!latestPlan &&
    latestPlan.state === 'Ready' &&
    latestPlan.outcome === 'ChangesPresent' &&
    latestPlan.hasChanges &&
    latestPlan.artifactsReady &&
    latestPlan.evidenceCaptured &&
    !latestPlan.approval
  const tabs = [
    { id: 'overview', label: 'Overview', icon: Boxes },
    { id: 'architecture', label: 'Architecture preview', icon: Network },
    { id: 'terraform', label: 'Terraform runs', icon: Terminal },
    { id: 'timeline', label: 'Lifecycle timeline', icon: Clock },
  ] as const
  return (
    <div className="content detail-content">
      <div className="breadcrumb">
        <button onClick={onBack}>{t('Environments')}</button>
        <ChevronRight size={13} aria-hidden="true" />
        <span>{detail.namespace}</span>
        <ChevronRight size={13} aria-hidden="true" />
        <span>{detail.name}</span>
      </div>
      <section className="entity-header">
        <div className="entity-heading">
          <span className="env-avatar large">
            <Boxes size={23} aria-hidden="true" />
          </span>
          <div>
            <div className="entity-title">
              <h2>{detail.name}</h2>
              <StatusBadge value={detail.phase} />
            </div>
            <div className="entity-metadata">
              <span>{detail.namespace}</span>
              <span>{detail.class || t('legacy environment')}</span>
              <span>
                {t('generation')} {detail.generation}
              </span>
            </div>
            <div className="detail-tags">
              <span>
                <Server size={14} aria-hidden="true" />
                {detail.target.clusterName || t('Discovery pending')}
              </span>
              <span>
                <Cloud size={14} aria-hidden="true" />
                {[detail.target.provider || 'Kubernetes', detail.target.region].filter(Boolean).join(' · ')}
              </span>
            </div>
          </div>
        </div>
        <button
          className="button button-danger-outline"
          disabled={busy || detail.deleting}
          onClick={() => {
            setDeleteName('')
            setDeleteConfirming(true)
          }}
        >
          <Trash2 size={15} aria-hidden="true" />
          {detail.deleting ? t('Deleting…') : t('Delete environment')}
        </button>
      </section>
      {deleteConfirming && !detail.deleting && (
        <section className="panel delete-confirm-panel">
          <div className="danger-heading">
            <Trash2 size={19} aria-hidden="true" />
            <h3>{t('Confirm deletion of {{environment}}', { environment: `${detail.namespace}/${detail.name}` })}</h3>
          </div>
          <ol className="delete-explanation">
            <li>{t('This action requests deletion of the environment.')}</li>
            <li>{t('Existing finalizers clean up runtime resources.')}</li>
            <li>{t('Terraform Destroy requires a separate exact-plan approval.')}</li>
          </ol>
          <label>
            <span className="field-label">{t('Type {{name}} to request deletion', { name: detail.name })}</span>
            <input
              className="text-input"
              aria-label={t('Confirm environment name')}
              value={deleteName}
              onChange={(event) => setDeleteName(event.target.value)}
              autoFocus
              autoComplete="off"
            />
          </label>
          <div className="delete-confirm-actions">
            <button className="button button-secondary" disabled={busy} onClick={() => setDeleteConfirming(false)}>
              {t('Cancel')}
            </button>
            <button
              className="button button-danger"
              disabled={busy || deleteName !== detail.name}
              onClick={() => {
                setDeleteConfirming(false)
                void onDelete()
              }}
            >
              <Trash2 size={15} aria-hidden="true" />
              {t('Request deletion')}
            </button>
          </div>
        </section>
      )}
      <div className="detail-metrics">
        <MetricCard label={t('Infrastructure')} value={<StatusBadge value={detail.infrastructure} />} icon={Cloud} />
        <MetricCard label={t('Runtime')} value={<StatusBadge value={detail.runtime} />} icon={Server} />
        <MetricCard label={t('Resource inventory')} value={detail.runtimeDetail?.inventoryItems ?? 0} icon={Database} />
        <MetricCard
          label={t('Latest approval')}
          value={detail.latestApproval ? <StatusBadge value={detail.latestApproval} /> : t('Not submitted')}
          icon={ShieldCheck}
        />
      </div>
      <div className="tab-bar" role="tablist" aria-label={t('Environment details')}>
        {tabs.map(({ id, label, icon: Icon }, index) => (
          <button
            key={id}
            id={`tab-${id}`}
            role="tab"
            aria-selected={tab === id}
            aria-controls={`panel-${id}`}
            tabIndex={tab === id ? 0 : -1}
            className={tab === id ? 'selected' : ''}
            onClick={() => onTab(id)}
            onKeyDown={(event) => {
              if (
                event.key === 'ArrowRight' ||
                event.key === 'ArrowLeft' ||
                event.key === 'Home' ||
                event.key === 'End'
              ) {
                event.preventDefault()
                const next =
                  event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                      ? tabs.length - 1
                      : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
                onTab(tabs[next].id)
                document.getElementById(`tab-${tabs[next].id}`)?.focus()
              }
            }}
          >
            <Icon size={16} aria-hidden="true" />
            {t(label)}
            {id === 'terraform' && <span className="count-chip">{detail.terraformRuns.length}</span>}
          </button>
        ))}
      </div>
      <div id={`panel-${tab}`} role="tabpanel" aria-labelledby={`tab-${tab}`}>
        {tab === 'overview' && (
          <div className="overview-grid">
            <section className="panel conditions-panel">
              <div className="panel-heading">
                <h3>
                  <Activity size={17} aria-hidden="true" />
                  {t('Current conditions')}
                </h3>
                <span className="tag">
                  {t('generation')} {detail.generation}
                </span>
              </div>
              {detail.conditions.length === 0 ? (
                <EmptyState compact icon={Activity} title={t('No conditions have been recorded yet.')} />
              ) : (
                detail.conditions.map((condition) => (
                  <div className="condition-row" key={condition.type}>
                    <StatusBadge value={condition.status} />
                    <div>
                      <div className="condition-heading">
                        <strong>{condition.type}</strong>
                        <span>{t(condition.reason || condition.status)}</span>
                      </div>
                      <p>{condition.message || t('No condition message provided.')}</p>
                      <time dateTime={condition.lastTransitionTime}>
                        <Clock size={12} aria-hidden="true" />
                        {formatTime(condition.lastTransitionTime)}
                      </time>
                    </div>
                  </div>
                ))
              )}
            </section>
            <div className="side-stack">
              <section className="panel target-panel">
                <h3>
                  <ShieldCheck size={17} aria-hidden="true" />
                  {t('Trusted target')}
                </h3>
                <strong className="target-name">{detail.target.clusterName || t('Discovery pending')}</strong>
                <dl className="detail-facts">
                  <Fact label={t('Provider')} value={detail.target.provider || '—'} />
                  <Fact label={t('Account')} value={detail.target.accountID || '—'} />
                  <Fact label={t('Region')} value={detail.target.region || '—'} />
                  <Fact label={t('Incarnation')} value={detail.target.incarnationID || '—'} code />
                </dl>
                <p className="safe-note">
                  {t('Connection credentials and certificate data are intentionally not exposed.')}
                </p>
              </section>
              {detail.runtimeDetail && (
                <section className="panel runtime-panel">
                  <h3>
                    <Server size={17} aria-hidden="true" />
                    {t('Runtime reconciliation')}
                  </h3>
                  <strong className="target-name">{detail.runtimeDetail.name}</strong>
                  <div className="runtime-info">
                    <StatusBadge value={detail.runtimeDetail.state} />
                    <span>{t('{{count}} inventoried resources', { count: detail.runtimeDetail.inventoryItems })}</span>
                  </div>
                  {detail.runtimeDetail.mutationBlocked && (
                    <Alert tone="warning">{t('Runtime mutation is currently fenced.')}</Alert>
                  )}
                  {detail.valkey && (
                    <div className="valkey-row">
                      <Database size={19} aria-hidden="true" />
                      <div>
                        <strong>Valkey</strong>
                        <small>
                          {t('{{ready}}/{{total}} replicas ready', {
                            ready: detail.valkey.readyReplicas || 0,
                            total: detail.valkey.replicas || 0,
                          })}
                        </small>
                      </div>
                      <StatusBadge value={detail.valkey.state} />
                    </div>
                  )}
                </section>
              )}
            </div>
            <section className="panel update-panel">
              <div>
                <h3>
                  <Cpu size={17} aria-hidden="true" />
                  {t('Capacity')}{' '}
                  <span className="tag">
                    {t(detail.nodeCount === 1 ? '{{count}} node' : '{{count}} nodes', { count: detail.nodeCount })}
                  </span>
                </h3>
                <p>
                  {t('Change node count only; the existing control plane plans and reconciles the new generation.')}
                </p>
              </div>
              <div className="update-control">
                <label>
                  <span className="sr-only">{t('Node count')}</span>
                  <input
                    className="text-input"
                    type="number"
                    min="0"
                    value={nodeCount ?? detail.nodeCount}
                    onChange={(event) => setNodeCount(Number(event.target.value))}
                  />
                </label>
                <button
                  className="button button-secondary"
                  disabled={busy || detail.class === '' || nodeCount === null}
                  onClick={() => void onUpdate(nodeCount ?? detail.nodeCount)}
                >
                  {t('Update capacity')}
                </button>
              </div>
            </section>
            <section className="panel evidence-panel">
              <div className="panel-heading">
                <h3>
                  <FileCode2 size={17} aria-hidden="true" />
                  {t('Latest Terraform evidence')}
                </h3>
                <button className="text-button" onClick={() => onTab('terraform')}>
                  {t('View runs')}
                  <ArrowRight size={14} aria-hidden="true" />
                </button>
              </div>
              <Evidence detail={detail} />
            </section>
          </div>
        )}
        {tab === 'architecture' && (
          <Architecture plan={plan} run={latestPlan} approvalReady={approvalReady} busy={busy} onApprove={onApprove} />
        )}
        {tab === 'terraform' && (
          <section className="panel runs-panel">
            <div className="panel-heading">
              <h3>
                <Terminal size={17} aria-hidden="true" />
                {t('Terraform runs')}
                <span className="count-chip">{detail.terraformRuns.length}</span>
              </h3>
              <span className="muted">{t('Immutable attempts')}</span>
            </div>
            {detail.terraformRuns.length === 0 ? (
              <EmptyState
                icon={Terminal}
                title={t('No Terraform runs yet')}
                description={t('The controller will create one after the environment is reconciled.')}
              />
            ) : (
              <div className="run-list">
                {[...detail.terraformRuns].reverse().map((run) => (
                  <RunRow
                    key={run.uid}
                    run={run}
                    expanded={selectedRun === run.uid}
                    onClick={() => {
                      setSelectedRun(selectedRun === run.uid ? '' : run.uid)
                      onOpenRun(run)
                    }}
                  />
                ))}
              </div>
            )}
          </section>
        )}
        {tab === 'timeline' && <Timeline events={detail.timeline} note={detail.timelineNote} />}
      </div>
    </div>
  )
}

function Fact({ label, value, code = false }: { label: string; value: string; code?: boolean }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{code ? <code>{value}</code> : value}</dd>
    </div>
  )
}

function Evidence({ detail }: { detail: EnvironmentDetail }) {
  const { t } = useI18n()
  const entries = [
    ['Plan digest', detail.evidence.planDigest],
    ['Plan report', detail.evidence.planReportRef],
    ['Report digest', detail.evidence.planReportDigest],
    ['Terminal result', detail.evidence.terminalResultRef],
    ['Source bundle digest', detail.evidence.sourceBundleDigest],
    ['Target discovery digest', detail.evidence.targetDiscoveryDigest],
  ]
  return (
    <dl className="evidence-grid">
      {entries.map(([label, value]) => (
        <Fact key={label} label={t(String(label))} value={value || t('Not recorded')} code />
      ))}
    </dl>
  )
}

function RunRow({ run, expanded, onClick }: { run: TerraformRun; expanded: boolean; onClick: () => void }) {
  const { t } = useI18n()
  const Icon = run.operation === 'Plan' ? FileCode2 : run.operation === 'Apply' ? GitBranch : Trash2
  const changeState =
    run.hasChanges || run.outcome === 'ChangesPresent'
      ? t('Changes present')
      : run.outcome === 'NoChange'
        ? t('No changes')
        : null
  return (
    <article className={`run-item ${expanded ? 'expanded' : ''}`}>
      <button className="run-row" onClick={onClick} aria-expanded={expanded}>
        <span className={`operation-icon ${run.operation.toLowerCase()}`}>
          <Icon size={19} aria-hidden="true" />
        </span>
        <span className="run-primary">
          <strong>
            {t(run.operation)}
            {run.planMode === 'Destroy' && <span className="tag">{t('Destroy')}</span>}
          </strong>
          <code>{run.name}</code>
          <span>
            {t(run.reason || run.outcome || 'Reconciliation pending')}
            {changeState && <> · {changeState}</>}
          </span>
        </span>
        <StatusBadge value={run.state} />
        <span className="run-digest">
          <span>{t('Plan identity')}</span>
          <code title={run.planDigest || run.uid}>{short(run.planDigest || run.uid)}</code>
        </span>
        <span className="run-evidence">
          <span>
            <HardDrive size={13} aria-hidden="true" />
            {t(run.artifactsReady ? 'Evidence ready' : 'Evidence pending')}
          </span>
          <time dateTime={run.createdAt}>{formatTime(run.createdAt)}</time>
        </span>
        <ChevronRight size={16} className="run-chevron" aria-hidden="true" />
      </button>
      {expanded && (
        <div className="run-expanded">
          {run.message && <p>{run.message}</p>}
          <dl className="evidence-grid">
            <Fact label={t('Plan digest')} value={run.planDigest || t('Not recorded')} code />
            <Fact label={t('Report digest')} value={run.planReportDigest || t('Not recorded')} code />
            <Fact label={t('Execution context digest')} value={run.executionContextDigest || t('Not recorded')} code />
            <Fact label={t('Effective input digest')} value={run.effectivePlanInputDigest || t('Not recorded')} code />
            <Fact label={t('Evidence captured')} value={t(run.evidenceCaptured ? 'Yes' : 'No')} />
            <Fact label={t('Finished')} value={formatTime(run.terminalFinishedAt)} />
          </dl>
        </div>
      )}
    </article>
  )
}

function Architecture({
  plan,
  run,
  approvalReady,
  busy,
  onApprove,
}: {
  plan: PlanView | null
  run?: TerraformRun
  approvalReady: boolean
  busy: boolean
  onApprove: (run: TerraformRun) => Promise<void>
}) {
  const { t } = useI18n()
  const graph = plan?.graph
  const recorded = run?.approval?.state === 'Recorded' || run?.approval?.state === 'Approved'
  const noChange = run?.outcome === 'NoChange'
  return (
    <div className={`architecture-layout ${run ? 'has-approval' : ''}`}>
      <section className="panel architecture-panel">
        <div className="panel-heading">
          <h3>
            <Network size={17} aria-hidden="true" />
            {t('Architecture change preview')}
          </h3>
          <span className="tag">{t('Saved Plan')}</span>
        </div>
        {graph && (
          <div className="change-summary" aria-label={t('Change summary')}>
            {(['create', 'update', 'delete', 'replace'] as const).map((action) => (
              <div className={action} key={action}>
                <span>{t(action)}</span>
                <strong>
                  {action === 'create' ? '+' : action === 'update' ? '~' : action === 'delete' ? '−' : '±'}{' '}
                  {graph.summary[action]}
                </strong>
              </div>
            ))}
          </div>
        )}
        <div className="architecture-note">
          <ShieldCheck size={15} aria-hidden="true" />
          <p>
            {t(
              'Architecture nodes are derived from the immutable Terraform plan; values are allowlisted and sensitive fields are omitted.'
            )}
          </p>
        </div>
        {noChange && (
          <Alert tone="success" title={t('No resource changes')}>
            {t('This plan has no create, update, delete or replace actions.')}
          </Alert>
        )}
        {!plan?.available || !graph ? (
          <EmptyState
            icon={Network}
            title={t('Plan preview unavailable')}
            description={plan?.message || t('Plan evidence has not been loaded.')}
          />
        ) : graph.nodes.length === 0 ? (
          <EmptyState
            icon={CheckCircle2}
            title={t('No resource changes')}
            description={t('This plan has no create, update, delete or replace actions.')}
          />
        ) : (
          <PlanTopology graph={graph} />
        )}
      </section>
      {run && (
        <section className={`panel approval-panel ${recorded ? 'recorded' : noChange ? 'no-changes' : ''}`}>
          <div className="approval-heading">
            <span className="approval-icon">
              <ShieldCheck size={23} aria-hidden="true" />
            </span>
            <div>
              <div className="section-kicker">
                {t(noChange ? 'NO CHANGES' : recorded ? 'APPROVAL RECORDED' : 'HUMAN REVIEW REQUIRED')}
              </div>
              <h3>
                {t(
                  noChange
                    ? 'No changes to approve'
                    : recorded
                      ? 'Approval recorded'
                      : run.planMode === 'Destroy'
                        ? 'Review the Destroy plan'
                        : approvalReady
                          ? 'Review this exact plan'
                          : 'No approval action available'
                )}
              </h3>
            </div>
            {run.approval && <StatusBadge value={run.approval.state} />}
          </div>
          <dl className="approval-evidence">
            <Fact label={t('Plan digest')} value={run.planDigest || t('Not recorded')} code />
            <Fact label={t('Expiration')} value={formatTime(run.planExpiresAt)} />
            <Fact
              label={t('Evidence readiness')}
              value={t(run.artifactsReady && run.evidenceCaptured ? 'Evidence ready' : 'Evidence pending')}
            />
            <Fact
              label={t('Exact-plan binding')}
              value={run.approval?.name || t(noChange ? 'Not required' : 'Awaiting human approval')}
            />
          </dl>
          <p>
            {t(
              noChange
                ? 'This Plan reports NoChange. No approval can be submitted.'
                : 'Approval is bound to this immutable saved Plan. The controller reconciles the matching approval; there is no direct Apply action.'
            )}
          </p>
          {!noChange && !run.approval && !approvalReady && (
            <Alert tone="info">
              {t(
                'Waiting for a Ready Plan with changes and complete saved evidence. No approval can be submitted yet.'
              )}
            </Alert>
          )}
          <div className="approval-footer">
            <span>
              <ShieldCheck size={15} aria-hidden="true" />
              {t(
                noChange
                  ? 'Not required'
                  : run.planMode === 'Destroy'
                    ? 'Separate Destroy approval'
                    : 'Exact-plan human approval'
              )}
            </span>
            {run.approval ? (
              <StatusBadge value={run.approval.state} />
            ) : (
              <button
                className={`button ${approvalReady ? 'button-primary' : 'button-secondary'}`}
                disabled={busy || !approvalReady}
                onClick={() => void onApprove(run)}
              >
                <ShieldCheck size={16} aria-hidden="true" />
                {t(
                  noChange
                    ? 'No changes to approve'
                    : approvalReady
                      ? 'Approve exact plan'
                      : 'Waiting for Plan evidence'
                )}
              </button>
            )}
          </div>
        </section>
      )}
    </div>
  )
}

function Timeline({ events, note }: { events: TimelineEvent[]; note: string }) {
  const { t } = useI18n()
  return (
    <section className="panel timeline-panel">
      <div className="panel-heading">
        <h3>
          <Clock size={17} aria-hidden="true" />
          {t('Lifecycle timeline')}
        </h3>
        <span className="count-chip">{t('{{count}} entries', { count: events.length })}</span>
      </div>
      {note && <div className="timeline-note">{t(note)}</div>}
      {events.length === 0 ? (
        <EmptyState icon={Clock} title={t('No lifecycle evidence yet')} />
      ) : (
        <ol className="timeline-list">
          {[...events].reverse().map((event, index) => (
            <li className="timeline-event" key={`${event.kind}:${event.resource}:${event.at}:${index}`}>
              <span className="timeline-marker">
                {event.kind === 'ChangeApproval' ? (
                  <ShieldCheck size={15} aria-hidden="true" />
                ) : event.kind === 'TerraformRun' ? (
                  <Terminal size={15} aria-hidden="true" />
                ) : (
                  <Activity size={15} aria-hidden="true" />
                )}
              </span>
              <div className="timeline-event-body">
                <div className="timeline-event-head">
                  <div>
                    <span className="event-kind">{event.kind}</span>
                    <strong>{t(event.title)}</strong>
                  </div>
                  <time dateTime={event.at}>{formatTime(event.at)}</time>
                </div>
                <p>{event.message || t(event.reason || event.state || event.resource)}</p>
                <code>{event.resource}</code>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
