import { useCallback, useEffect, useMemo, useState } from 'react'
import { request, envPath, type Approval, type ClassInfo, type DraftResponse, type Environment, type EnvironmentDetail, type PlanView, type TerraformRun, type TimelineEvent } from './api'

type Screen = 'dashboard' | 'builder' | 'detail'
type DetailTab = 'overview' | 'architecture' | 'terraform' | 'timeline'

function App() {
  const [screen, setScreen] = useState<Screen>('dashboard')
  const [tab, setTab] = useState<DetailTab>('overview')
  const [environments, setEnvironments] = useState<Environment[]>([])
  const [classes, setClasses] = useState<ClassInfo[]>([])
  const [selected, setSelected] = useState<Pick<Environment, 'namespace' | 'name'> | null>(null)
  const [detail, setDetail] = useState<EnvironmentDetail | null>(null)
  const [plan, setPlan] = useState<PlanView | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const refresh = useCallback(async () => {
    try {
      const [items, classItems] = await Promise.all([request<Environment[]>('/api/environments'), request<ClassInfo[]>('/api/classes')])
      setEnvironments(items)
      setClasses(classItems)
      setError('')
    } catch (value) { setError((value as Error).message) }
    finally { setLoading(false) }
  }, [])

  const openEnvironment = useCallback(async (environment: Pick<Environment, 'namespace' | 'name'>) => {
    setSelected(environment); setScreen('detail'); setTab('overview'); setDetail(null); setPlan(null); setError('')
    try {
      const value = await request<EnvironmentDetail>(envPath(environment))
      setDetail(value)
      const planRun = value.infrastructureDetail?.latestPlanRun
      if (planRun) setPlan(await request<PlanView>(`/api/terraform-runs/${encodeURIComponent(environment.namespace)}/${encodeURIComponent(planRun)}/plan`))
    } catch (reason) { setError((reason as Error).message) }
  }, [])

  useEffect(() => { void refresh(); const timer = window.setInterval(() => void refresh(), 8000); return () => window.clearInterval(timer) }, [refresh])
  useEffect(() => {
    if (!selected || screen !== 'detail') return
    let active = true
    const load = async () => {
      try {
        const value = await request<EnvironmentDetail>(envPath(selected))
        if (!active) return
        setDetail(value)
        const latest = value.infrastructureDetail?.latestPlanRun
        setPlan(latest ? await request<PlanView>(`/api/terraform-runs/${encodeURIComponent(selected.namespace)}/${encodeURIComponent(latest)}/plan`) : null)
      } catch (reason) { if (active) setError((reason as Error).message) }
    }
    const timer = window.setInterval(() => void load(), 6000)
    return () => { active = false; window.clearInterval(timer) }
  }, [selected, screen])

  const readyCount = useMemo(() => environments.filter(item => item.ready).length, [environments])
  const navigate = (next: Screen) => { setScreen(next); setError(''); setNotice('') }

  const approve = async (run: TerraformRun) => {
    if (!selected) return
    setBusy(true); setError(''); setNotice('')
    try {
      const result = await request<Approval>(`/api/terraform-runs/${encodeURIComponent(run.namespace)}/${encodeURIComponent(run.name)}/approve`, {
        method: 'POST', body: JSON.stringify({ planRunUID: run.uid, planDigest: run.planDigest, executionContextDigest: run.executionContextDigest, effectivePlanInputDigest: run.effectivePlanInputDigest, planReportRef: run.planReportRef, planReportDigest: run.planReportDigest }),
      })
      setNotice(`Approval ${result.name} recorded against this exact plan. The existing controller will reconcile it.`)
      await openEnvironment(selected); await refresh()
    } catch (reason) { setError((reason as Error).message) }
    finally { setBusy(false) }
  }

  const created = async (environment: Pick<Environment, 'namespace' | 'name'>) => {
    await refresh(); setNotice('PlatformEnvironment submitted. Terraform Plan and all later steps remain under the existing control plane.'); await openEnvironment(environment)
  }

  const updateCapacity = async (nodeCount: number) => {
    if (!detail) return
    setBusy(true); setError(''); setNotice('')
    try {
      await request(envPath(detail), { method: 'PUT', body: JSON.stringify({ uid: detail.uid, generation: detail.generation, nodeCount }) })
    } catch (reason) {
      const message = (reason as Error).message
      if (message.includes('StaleEnvironment')) { setError('The environment changed while this page was open. Refresh the detail and try again.') }
      else { setError(message) }
    } finally { setBusy(false); await openEnvironment(detail) }
  }

  const removeEnvironment = async () => {
    if (!detail || !window.confirm(`Request deletion of ${detail.namespace}/${detail.name}? Existing finalizers will prune runtime resources and plan/apply Terraform destroy.`)) return
    const typed = window.prompt(`Type ${detail.name} to confirm environment deletion`)
    if (typed !== detail.name) return
    setBusy(true); setError(''); setNotice('')
    try { await request(envPath(detail), { method: 'DELETE', body: JSON.stringify({ uid: detail.uid }) }); setNotice('Deletion requested. Cleanup is running through existing finalizers; this console does not execute Terraform.'); await refresh() }
    catch (reason) { setError((reason as Error).message) }
    finally { setBusy(false) }
  }

  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><div className="brand-mark">P</div><div><strong>PLATFORM</strong><small>CONTROL PLANE</small></div></div>
      <div className="workspace-label">LOCAL WORKSPACE</div>
      <nav className="primary-nav">
        <button className={screen === 'dashboard' || screen === 'detail' ? 'active' : ''} onClick={() => navigate('dashboard')}><span className="nav-icon">◫</span> Environments <span className="nav-count">{environments.length}</span></button>
        <button className={screen === 'builder' ? 'active' : ''} onClick={() => navigate('builder')}><span className="nav-icon">＋</span> Platform builder</button>
      </nav>
      <div className="sidebar-bottom"><div className="local-indicator"><span className="pulse"/> LOCAL MODE</div><p>Kind · LocalStack</p><small>Stage 1 engine · frozen</small></div>
    </aside>

    <main className="main-area">
      <header className="topbar"><div><div className="eyebrow">PLATFORM ENGINEERING / LOCAL</div><h1>{screen === 'builder' ? 'Platform builder' : screen === 'detail' ? selected?.name || 'Environment' : 'Environments'}</h1></div><div className="top-actions"><span className="connected"><i/> API connected</span><button className="icon-button" title="Refresh" onClick={() => void refresh()}>↻</button><button className="button button-primary" onClick={() => navigate('builder')}>＋ New environment</button></div></header>
      {error && <div className="alert alert-error"><span>!</span><div><strong>Request could not be completed</strong><p>{error}</p></div><button onClick={() => setError('')}>×</button></div>}
      {notice && <div className="alert alert-success"><span>✓</span><div><strong>Workflow updated</strong><p>{notice}</p></div><button onClick={() => setNotice('')}>×</button></div>}

      {screen === 'dashboard' && <Dashboard environments={environments} readyCount={readyCount} loading={loading} onOpen={openEnvironment} onBuild={() => navigate('builder')} />}
      {screen === 'builder' && <Builder classes={classes} onCancel={() => navigate('dashboard')} onCreated={created} />}
      {screen === 'detail' && detail && <Detail detail={detail} plan={plan} tab={tab} busy={busy} onTab={setTab} onApprove={approve} onUpdate={updateCapacity} onDelete={removeEnvironment} onOpenRun={() => setTab('terraform')} />}
      {screen === 'detail' && !detail && !error && <div className="loading-state"><div className="spinner"/> Loading environment evidence…</div>}
    </main>
  </div>
}

function Dashboard({ environments, readyCount, loading, onOpen, onBuild }: { environments: Environment[]; readyCount: number; loading: boolean; onOpen: (item: Environment) => void; onBuild: () => void }) {
  const pending = environments.filter(item => item.latestApproval === 'Required' || item.latestApproval === 'Pending').length
  return <div className="content">
    <section className="welcome-row"><div><div className="section-kicker">CONTROL PLANE OVERVIEW</div><h2>Understand every environment,<br/><em>from intent to runtime.</em></h2><p>Live read model from the Kubernetes-native control plane.</p></div><div className="welcome-art"><div className="orb orb-one"/><div className="orb orb-two"/><div className="orbit"/><div className="welcome-art-label">K8s <span>↗</span> IaC</div></div></section>
    <section className="stat-grid"><Stat label="TOTAL ENVIRONMENTS" value={environments.length} icon="◫" tone="blue"/><Stat label="READY" value={readyCount} icon="✓" tone="green"/><Stat label="AWAITING APPROVAL" value={pending} icon="◇" tone="amber"/><Stat label="CONTROL PLANE" value="ONLINE" icon="⌁" tone="green" small/></section>
    <section className="panel environment-panel"><div className="panel-heading"><div><div className="section-kicker">YOUR FLEET</div><h3>Platform environments <span className="count-chip">{environments.length}</span></h3></div><button className="button button-secondary" onClick={onBuild}>＋ Create environment</button></div>
      {loading ? <div className="empty-state"><div className="spinner"/> Loading Kubernetes resources…</div> : environments.length === 0 ? <div className="empty-state"><div className="empty-icon">⌘</div><strong>No environments yet</strong><p>Start with a typed EnvironmentClass and a reviewed draft.</p><button className="button button-primary" onClick={onBuild}>Open platform builder</button></div> : <div className="table-wrap"><table><thead><tr><th>ENVIRONMENT</th><th>CLASS</th><th>STATE</th><th>INFRA / RUNTIME</th><th>TARGET</th><th>LATEST RUN</th><th/></tr></thead><tbody>{environments.map(item => <tr key={`${item.namespace}/${item.name}`} onClick={() => onOpen(item)}><td><div className="env-name"><span className="env-avatar">{item.name.slice(0,1).toUpperCase()}</span><div><strong>{item.name}</strong><small>{item.namespace} · gen {item.generation}</small></div></div></td><td><span className="class-pill">{item.class || 'legacy'}</span></td><td><StatePill value={item.phase}/></td><td><div className="mini-states"><span><i className={item.infrastructure.toLowerCase() === 'ready' ? 'dot good' : 'dot'}/>{item.infrastructure}</span><span><i className={item.runtime.toLowerCase() === 'ready' ? 'dot good' : 'dot'}/>{item.runtime}</span></div></td><td><div className="target-cell"><strong>{item.target.clusterName || '—'}</strong><small>{[item.target.provider,item.target.region].filter(Boolean).join(' · ')}</small></div></td><td>{item.latestTerraformRun ? <div className="run-cell"><strong>{item.latestTerraformRun.operation}</strong><small>{item.latestTerraformRun.reason || item.latestTerraformRun.outcome || 'Reconciling'}</small></div> : <span className="muted">No run</span>}</td><td><span className="row-arrow">↗</span></td></tr>)}</tbody></table></div>}
      <div className="panel-foot"><span><i className="pulse"/> Live status</span><span>Refreshes every 8 seconds</span></div>
    </section>
    <div className="footer-note"><span>◈</span> Kubernetes is the source of truth. AI proposes · API validates · controllers reconcile · Terraform executes.</div>
  </div>
}

function Stat({ label, value, icon, tone, small = false }: { label: string; value: string | number; icon: string; tone: string; small?: boolean }) { return <div className="stat-card"><div className={`stat-icon ${tone}`}>{icon}</div><div><span>{label}</span><strong className={small ? 'small-value' : ''}>{value}</strong></div><div className="stat-decoration">{icon}</div></div> }

function Builder({ classes, onCancel, onCreated }: { classes: ClassInfo[]; onCancel: () => void; onCreated: (value: Pick<Environment,'namespace'|'name'>) => Promise<void> }) {
  const [description,setDescription]=useState('Create a small development platform with Valkey cache')
  const [name,setName]=useState('')
  const [namespace,setNamespace]=useState('default')
  const [classRef,setClassRef]=useState('')
  const [region,setRegion]=useState('')
  const [nodeCount,setNodeCount]=useState(1)
  const [draft,setDraft]=useState<DraftResponse|null>(null)
  const [pending,setPending]=useState(false)
  const [error,setError]=useState('')
  const selectedClass=classes.find(item=>item.name===classRef)
  useEffect(()=>{if(!classRef&&classes.length>0){const ready=classes.find(item=>item.ready);if(ready)setClassRef(ready.name)}},[classes,classRef])

  const generate=async()=>{setPending(true);setError('');setDraft(null);try{const value=await request<DraftResponse>('/api/drafts',{method:'POST',body:JSON.stringify({description,name,namespace,classRef,region,nodeCount})});setDraft(value);if(!value.valid)setError(value.errors?.join('; ')||'Draft validation failed')}catch(value){setError((value as Error).message)}finally{setPending(false)}}
  const submit=async()=>{if(!draft?.valid)return;setPending(true);setError('');try{await request('/api/environments',{method:'POST',body:JSON.stringify(draft.intent)});await onCreated({namespace:draft.intent.namespace,name:draft.intent.name})}catch(value){setError((value as Error).message)}finally{setPending(false)}}
  return <div className="content builder-content"><div className="breadcrumb"><button onClick={onCancel}>Environments</button><span>/</span><strong>New environment</strong></div><div className="builder-intro"><div className="section-kicker">INTENT → REVIEW → SUBMIT</div><h2>Describe the platform<br/><em>you need.</em></h2><p>The assistant drafts a typed PlatformEnvironment. Existing controllers remain responsible for planning, approval, execution and runtime reconciliation.</p></div>
    <div className="builder-grid"><section className="panel builder-form"><div className="panel-title"><span className="step-number">01</span><div><h3>Describe desired state</h3><p>AI output stays a proposal until you explicitly submit it.</p></div></div><label className="field-label">Natural-language request</label><textarea className="text-area" rows={4} value={description} onChange={event=>setDescription(event.target.value)} placeholder="Describe a development or test environment…"/><div className="form-grid"><label><span className="field-label">Environment name <small>optional</small></span><input className="text-input" value={name} onChange={event=>setName(event.target.value)} placeholder="generated from description"/></label><label><span className="field-label">Namespace</span><input className="text-input" value={namespace} onChange={event=>setNamespace(event.target.value)}/></label><label><span className="field-label">Environment class</span><select className="text-input" value={classRef} onChange={event=>{setClassRef(event.target.value);setRegion('')}}><option value="">Select a ready class</option>{classes.map(item=><option key={item.name} value={item.name} disabled={!item.ready}>{item.name}{item.ready?'':' · not ready'}</option>)}</select></label><label><span className="field-label">Region <small>class default</small></span><select className="text-input" value={region} onChange={event=>setRegion(event.target.value)}><option value="">{selectedClass?.defaultRegion||'Use class default'}</option>{(selectedClass?.allowedRegions||[]).map(value=><option key={value} value={value}>{value}</option>)}</select></label><label><span className="field-label">Node count</span><input className="text-input" type="number" min={selectedClass?.capacityBounds.minNodeCount||0} max={selectedClass?.capacityBounds.maxNodeCount||1000} value={nodeCount} onChange={event=>setNodeCount(Number(event.target.value))}/></label></div>
      {selectedClass&&<div className="class-summary"><span className="class-summary-icon">⌘</span><div><strong>{selectedClass.name}</strong><p>{selectedClass.provider} · target {selectedClass.targetCluster} · {selectedClass.defaultRegion||'region not fixed'}</p></div><span className="ready-label"><i/> {selectedClass.ready?'READY':'NOT READY'}</span></div>}
      <div className="form-actions"><button className="button button-secondary" onClick={onCancel}>Cancel</button><button className="button button-primary" onClick={()=>void generate()} disabled={pending||!classRef||!selectedClass?.ready}><span>✦</span> {pending?'Generating…':'Generate draft'}</button></div>
      {error&&<p className="inline-error">{error}</p>}
    </section>
    <section className="draft-panel"><div className="draft-panel-head"><div><div className="section-kicker">02 · REVIEW BEFORE SUBMIT</div><h3>Draft preview</h3></div><span className="provider-chip">{draft?.provider||'LOCAL DRAFT'}</span></div>{draft?.valid?<><div className="draft-summary"><div className="draft-check">✓</div><div><strong>{draft.intent.namespace}/{draft.intent.name}</strong><p>{draft.intent.classRef} · {draft.intent.nodeCount} nodes · {draft.intent.region||'class region'}</p></div></div>{draft.warnings?.map(item=><div className="warning-note" key={item}>ⓘ {item}</div>)}<pre className="yaml-preview">{draft.yaml}</pre><button className="button button-primary submit-draft" disabled={pending} onClick={()=>void submit()}>{pending?'Submitting…':'Submit PlatformEnvironment'} <span>→</span></button><p className="approval-note">Submitting creates only the top-level Kubernetes resource. No Plan is approved and no infrastructure is applied by this button.</p></>:<div className="draft-placeholder"><div className="placeholder-grid"/><div className="sparkle">✦</div><strong>Your typed manifest preview appears here</strong><p>Generate a draft, review its validation result and YAML, then submit explicitly.</p></div>}</section></div>
    <div className="boundary-strip"><span>✦ AI PROPOSES</span><i>→</i><span>⌕ API VALIDATES</span><i>→</i><span>◉ HUMAN SUBMITS</span><i>→</i><span>⟳ CONTROL PLANE RECONCILES</span></div>
  </div>
}

function Detail({ detail, plan, tab, busy, onTab, onApprove, onUpdate, onDelete, onOpenRun }: { detail: EnvironmentDetail; plan: PlanView|null; tab: DetailTab; busy: boolean; onTab: (tab:DetailTab)=>void; onApprove: (run:TerraformRun)=>Promise<void>; onUpdate: (nodeCount:number)=>Promise<void>; onDelete:()=>Promise<void>; onOpenRun:(run:TerraformRun)=>void }) {
  const [nodeCount,setNodeCount]=useState<number|null>(null)
  const latestPlan=detail.terraformRuns.filter(run=>run.operation==='Plan').slice(-1)[0]
  const approvalReady=!!latestPlan&&latestPlan.state==='Ready'&&latestPlan.outcome==='ChangesPresent'&&latestPlan.hasChanges&&latestPlan.artifactsReady&&latestPlan.evidenceCaptured&&!latestPlan.approval
  return <div className="content detail-content"><div className="breadcrumb"><button onClick={()=>onTab('overview')}>Environments</button><span>/</span><strong>{detail.namespace}/{detail.name}</strong></div><section className="detail-hero"><div className="detail-title"><span className="env-avatar large">{detail.name.slice(0,1).toUpperCase()}</span><div><div className="detail-subtitle">{detail.namespace} · {detail.class||'legacy environment'} · generation {detail.generation}</div><h2>{detail.name}</h2><div className="detail-tags"><StatePill value={detail.phase}/><span className="tag">target {detail.target.clusterName||'pending'}</span><span className="tag">{detail.target.provider||'Kubernetes'}{detail.target.region?` · ${detail.target.region}`:''}</span></div></div></div><button className="button button-danger-outline" disabled={busy||detail.deleting} onClick={()=>void onDelete()}>{detail.deleting?'Deleting…':'Delete environment'}</button></section>
    <div className="detail-metrics"><Metric label="INFRASTRUCTURE" value={detail.infrastructure} icon="⌂"/><Metric label="RUNTIME" value={detail.runtime} icon="◈"/><Metric label="RESOURCE INVENTORY" value={detail.runtimeDetail?.inventoryItems??0} icon="▤"/><Metric label="LATEST APPROVAL" value={detail.latestApproval||'Not submitted'} icon="◇"/></div>
    <div className="tab-bar">{(['overview','architecture','terraform','timeline'] as DetailTab[]).map(value=><button key={value} className={tab===value?'selected':''} onClick={()=>onTab(value)}>{value==='overview'?'Overview':value==='architecture'?'Architecture preview':value==='terraform'?'Terraform runs':'Lifecycle timeline'}{value==='terraform'&&<span>{detail.terraformRuns.length}</span>}</button>)}</div>
    {tab==='overview'&&<div className="overview-grid"><div className="panel conditions-panel"><div className="panel-heading"><div><div className="section-kicker">RECONCILIATION</div><h3>Current conditions</h3></div><span className="generation-chip">observed gen {detail.generation}</span></div>{detail.conditions.length===0?<p className="muted pad">No conditions have been recorded yet.</p>:detail.conditions.map(condition=><div className="condition-row" key={condition.type}><StateDot status={condition.status}/><div><strong>{condition.type}<small>{condition.reason||condition.status}</small></strong><p>{condition.message||'No condition message provided.'}</p></div><time>{formatTime(condition.lastTransitionTime)}</time></div>)}</div><div className="side-stack"><div className="panel target-panel"><div className="section-kicker">TRUSTED TARGET</div><h3>{detail.target.clusterName||'Discovery pending'}</h3><dl><dt>Provider</dt><dd>{detail.target.provider||'—'}</dd><dt>Account</dt><dd>{detail.target.accountID||'—'}</dd><dt>Region</dt><dd>{detail.target.region||'—'}</dd><dt>Incarnation</dt><dd>{detail.target.incarnationID||'—'}</dd></dl><p className="safe-note">Connection credentials and certificate data are intentionally not exposed.</p></div>{detail.runtimeDetail&&<div className="panel runtime-panel"><div className="section-kicker">RUNTIME RECONCILIATION</div><h3>{detail.runtimeDetail.name}</h3><div className="runtime-info"><StatePill value={detail.runtimeDetail.state}/><span>{detail.runtimeDetail.inventoryItems} inventoried resources</span></div>{detail.runtimeDetail.mutationBlocked&&<p className="inline-error">Runtime mutation is currently fenced.</p>}{detail.valkey&&<div className="valkey-row"><span className="valkey-icon">V</span><div><strong>Valkey</strong><small>{detail.valkey.state} · {detail.valkey.readyReplicas||0}/{detail.valkey.replicas||0} replicas ready</small></div><StatePill value={detail.valkey.state}/></div>}</div>}</div><div className="panel update-panel"><div><div className="section-kicker">SAFE DESIRED-STATE UPDATE</div><h3>Capacity</h3><p>Change node count only; the existing control plane plans and reconciles the new generation.</p></div><div className="update-control"><input type="number" min="0" value={nodeCount??detail.nodeCount} onChange={event=>setNodeCount(Number(event.target.value))}/><button className="button button-secondary" disabled={busy||detail.class===''||nodeCount===null} onClick={()=>void onUpdate(nodeCount??detail.nodeCount)}>Update capacity</button></div></div><div className="panel evidence-panel"><div className="panel-heading"><div><div className="section-kicker">DURABLE EVIDENCE</div><h3>Latest Terraform evidence</h3></div><button className="text-button" onClick={()=>onTab('terraform')}>View runs →</button></div><Evidence detail={detail}/></div></div>}
    {tab==='architecture'&&<Architecture plan={plan} run={latestPlan} approvalReady={approvalReady} busy={busy} onApprove={onApprove}/>}
    {tab==='terraform'&&<div className="panel runs-panel"><div className="panel-heading"><div><div className="section-kicker">IMMUTABLE ATTEMPTS</div><h3>Terraform runs</h3></div><span className="count-chip">{detail.terraformRuns.length}</span></div>{detail.terraformRuns.length===0?<div className="empty-state"><strong>No Terraform runs yet</strong><p>The controller will create one after the environment is reconciled.</p></div>:<div className="run-list">{[...detail.terraformRuns].reverse().map(run=><RunRow key={run.uid} run={run} onClick={()=>onOpenRun(run)}/>)}</div>}</div>}
    {tab==='timeline'&&<Timeline events={detail.timeline} note={detail.timelineNote}/>}
  </div>
}

function Metric({label,value,icon}:{label:string;value:string|number;icon:string}){return <div className="metric-card"><span className="metric-icon">{icon}</span><span>{label}</span><strong>{value}</strong></div>}
function Evidence({detail}:{detail:EnvironmentDetail}){const entries=[['Plan digest',detail.evidence.planDigest],['Plan report',detail.evidence.planReportRef],['Report digest',detail.evidence.planReportDigest],['Terminal result',detail.evidence.terminalResultRef],['Source bundle digest',detail.evidence.sourceBundleDigest],['Target discovery digest',detail.evidence.targetDiscoveryDigest]];return <div className="evidence-grid">{entries.map(([label,value])=><div key={label}><span>{label}</span><code>{value?short(String(value)): 'Not recorded'}</code></div>)}</div>}
function RunRow({run,onClick}:{run:TerraformRun;onClick:()=>void}){return <div className="run-row" onClick={onClick}><div className={`operation-icon ${run.operation.toLowerCase()}`}>{run.operation==='Plan'?'⌕':run.operation==='Apply'?'↗':'⌫'}</div><div className="run-primary"><strong>{run.operation} <small>{run.name}</small></strong><p>{run.reason||run.outcome||'Reconciliation pending'}{run.hasChanges?' · changes present':''}</p></div><StatePill value={run.state}/><div className="run-digest"><span>PLAN IDENTITY</span><code>{short(run.planDigest||run.uid)}</code></div><div className="run-evidence"><span>{run.artifactsReady?'Evidence ready':'Evidence pending'}</span><small>{formatTime(run.createdAt)}</small></div><span className="row-arrow">↗</span></div>}

function Architecture({plan,run,approvalReady,busy,onApprove}:{plan:PlanView|null;run?:TerraformRun;approvalReady:boolean;busy:boolean;onApprove:(run:TerraformRun)=>Promise<void>}){const graph=plan?.graph;const nodes=graph?.nodes||[];const positions=nodes.map((node,index)=>({node,x:2+(index%2)*49,y:30+Math.floor(index/2)*152}));const coords=new Map(positions.map(item=>[item.node.address,item]));const height=Math.max(250,Math.ceil(nodes.length/2)*152+28);return <div className="architecture-layout"><div className="panel architecture-panel"><div className="panel-heading"><div><div className="section-kicker">DETERMINISTIC PLAN PROJECTION</div><h3>Architecture change preview</h3></div>{graph&&<div className="change-summary"><span className="create">+ {graph.summary.create}</span><span className="update">~ {graph.summary.update}</span><span className="delete">− {graph.summary.delete}</span><span className="replace">± {graph.summary.replace}</span></div>}</div><div className="architecture-note">{plan?.message||'Plan evidence has not been loaded.'} Architecture nodes are derived from the immutable Terraform plan; values are allowlisted and sensitive fields are omitted.</div>{!plan?.available||!graph?<div className="empty-state"><div className="empty-icon">⌁</div><strong>Plan preview unavailable</strong><p>{plan?.message||'Run a Terraform Plan to see proposed resource changes here.'}</p></div>:nodes.length===0?<div className="empty-state"><div className="empty-icon">✓</div><strong>No resource changes</strong><p>This plan has no create, update, delete or replace actions.</p></div>:<><div className="graph-legend"><span><i className="legend current"/>CURRENT</span><span><i className="legend planned"/>PLANNED</span><span><i className="legend create"/>CREATE</span><span><i className="legend update"/>UPDATE</span><span><i className="legend delete"/>DELETE / REPLACE</span></div><div className="graph-canvas" style={{height}}><svg className="graph-edges" viewBox={`0 0 1000 ${height}`} preserveAspectRatio="none" aria-hidden="true">{graph.edges.map(edge=>{const from=coords.get(edge.from),to=coords.get(edge.to);if(!from||!to)return null;const x1=(from.x+24)*10,y1=from.y+52,x2=(to.x+24)*10,y2=to.y+52;return <path key={`${edge.from}:${edge.to}`} d={`M ${x1} ${y1} C ${x1+90} ${y1}, ${x2-90} ${y2}, ${x2} ${y2}`} />})}</svg>{positions.map(({node,x,y})=><div key={node.address} className={`graph-node action-${node.action}`} style={{left:`${x}%`,top:y}}><div className="graph-node-head"><span className={`action-sign ${node.action}`}>{node.action==='create'?'+':node.action==='update'?'~':node.action==='delete'?'−':node.action==='replace'?'±':'='}</span><span className="resource-type">{node.type}</span><span className={`action-label ${node.action}`}>{node.action}</span></div><strong>{node.address}</strong><div className="node-columns"><div><small>CURRENT</small><Metadata values={node.before}/></div><div><small>PLANNED</small><Metadata values={node.after}/></div></div></div>)}</div></>}</div>{run&&<div className="panel approval-panel"><div className="approval-panel-copy"><div className="section-kicker">HUMAN REVIEW GATE</div><h3>{run.approval?.state==='Approved'?'Approval recorded':approvalReady?'Review this exact plan':'No approval action available'}</h3><p>Plan <code>{short(run.planDigest||'not ready')}</code> · expires {formatTime(run.planExpiresAt)}</p><p>The platform API creates only the existing immutable ChangeApproval binding. The existing controller decides when the matching saved plan can proceed.</p></div><div className="approval-panel-action">{run.approval?<StatePill value={run.approval.state}/>:approvalReady?<button className="button button-primary" disabled={busy} onClick={()=>void onApprove(run)}>✓ Approve exact plan</button>:<button className="button button-secondary" disabled>Waiting for Plan evidence</button>}<span>No Apply endpoint exists in this console.</span></div></div>}</div>}

function Metadata({values}:{values?:Record<string,unknown>}){if(!values||Object.keys(values).length===0)return <span className="metadata-empty">—</span>;return <ul className="metadata-list">{Object.entries(values).map(([key,value])=><li key={key}><span>{key}</span><b>{typeof value==='object'?JSON.stringify(value):String(value)}</b></li>)}</ul>}

function Timeline({events,note}:{events:TimelineEvent[];note:string}){return <div className="panel timeline-panel"><div className="panel-heading"><div><div className="section-kicker">RESOURCE-BACKED EVENTS</div><h3>Lifecycle timeline</h3></div><span className="count-chip">{events.length} entries</span></div><div className="timeline-note">ⓘ {note}</div>{events.length===0?<div className="empty-state"><strong>No lifecycle evidence yet</strong></div>:<div className="timeline-list">{[...events].reverse().map((event,index)=><div className="timeline-event" key={`${event.kind}:${event.resource}:${event.at}:${index}`}><div className="timeline-marker"><span/></div><div className="timeline-event-body"><div className="timeline-event-head"><div><span className="event-kind">{event.kind}</span><strong>{event.title}</strong></div><time>{formatTime(event.at)}</time></div><p>{event.message||event.reason||event.state||event.resource}</p><small>{event.resource}</small></div></div>)}</div>}</div>}

function StatePill({value}:{value:string}){const lower=value.toLowerCase();const tone=lower==='ready'||lower==='approved'||lower==='succeeded'||lower==='online'?'good':lower.includes('fail')||lower.includes('reject')||lower==='unknown'?'bad':lower.includes('waiting')||lower==='pending'||lower==='reconciling'||lower==='running'?'warn':'neutral';return <span className={`state-pill ${tone}`}><i/>{value||'Unknown'}</span>}
function StateDot({status}:{status:string}){return <span className={`condition-dot ${status.toLowerCase()==='true'?'good':status.toLowerCase()==='false'?'warn':'neutral'}`}/>}
function formatTime(value?:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?'—':date.toLocaleString(undefined,{month:'short',day:'2-digit',hour:'2-digit',minute:'2-digit'})}
function short(value:string){return value.length>34?`${value.slice(0,13)}…${value.slice(-12)}`:value}

export default App
