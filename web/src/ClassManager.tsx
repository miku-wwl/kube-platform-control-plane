import { useEffect, useId, useState } from 'react'
import { request, type ClassDetail, type ClassInfo, type ClassSpec, type ClassValidation, type RuntimeObject } from './api'
import { useI18n } from './i18n'

const classPath = (name: string) => `/api/classes/${encodeURIComponent(name)}`

function defaultSpec(): ClassSpec {
  return {
    version: 'v1', source: { url: '', revision: '', path: '.' },
    backend: { type: 's3', configRef: { name: '' }, authRef: { serviceAccountName: 'terraform-runner' }, lockTimeout: '2m' },
    executor: { terraformVersion: '1.14.0', image: '', workDir: '/workspace/terraform', executionTimeout: '12m' },
    runnerProfile: { serviceAccountName: 'terraform-runner', image: '', imageDigest: '', terraformVersion: '1.14.0' },
    runtimeProfile: {}, target: { provider: 'kind', account: 'local', region: 'local', clusterName: '' },
    allowedRegions: ['local'], capacityBounds: { minNodeCount: 1, maxNodeCount: 10, maxEnvironments: 10, maxConcurrentPlans: 2, maxConcurrentApplies: 1 },
    approvalPolicy: 'Manual',
  }
}

export function ClassManager({ classes, loading, onChanged, onUse }: { classes: ClassInfo[]; loading: boolean; onChanged: () => Promise<void>; onUse: (name: string) => void }) {
  const { t } = useI18n()
  const [mode, setMode] = useState<'list' | 'detail' | 'form'>('list')
  const [selected, setSelected] = useState('')
  const [detail, setDetail] = useState<ClassDetail | null>(null)
  const [copy, setCopy] = useState<ClassDetail | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [confirmName, setConfirmName] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    if (mode !== 'detail' || !selected) return
    let active = true
    request<ClassDetail>(classPath(selected)).then(value => { if (active) setDetail(value) }).catch(reason => {
      if (!active) return
      if ((reason as Error).message === 'EnvironmentClass was not found') {
        setMode('list'); setDetail(null); setNotice(t('Environment class removed.'))
      } else setError((reason as Error).message)
    })
    return () => { active = false }
  }, [selected, mode, classes, t])

  const open = (name: string) => { setSelected(name); setDetail(null); setMode('detail'); setError(''); setNotice(''); setConfirmName(null) }
  const newClass = (source: ClassDetail | null = null) => { setCopy(source); setMode('form'); setError(''); setNotice(''); setConfirmName(null) }
  const created = async (value: ClassDetail) => {
    setSelected(value.name); setDetail(value); setMode('detail')
    setNotice(t('Class saved. Waiting for the controller to report Ready.'))
    await onChanged()
  }
  const remove = async () => {
    if (!detail || confirmName !== detail.name) return
    setPending(true); setError('')
    try {
      await request(classPath(detail.name), { method: 'DELETE', body: JSON.stringify({ uid: detail.uid, confirmName }) })
      setConfirmName(null); setNotice(t('Class deletion requested. The controller protects classes still in use.'))
      await onChanged()
    } catch (reason) { setError((reason as Error).message) }
    finally { setPending(false) }
  }

  return <div className="content class-content">
    <div className="class-page-heading"><div><div className="section-kicker">{t('PLATFORM CONFIGURATION')}</div><h2>{t('Environment classes')}</h2><p>{t('Define reusable environment templates, then select a Ready class in the platform builder.')}</p></div>
      {mode === 'list' ? <button className="button button-primary" onClick={() => newClass()}>＋ {t('Create class')}</button> : <button className="button button-secondary" onClick={() => { setMode('list'); setError(''); setNotice(''); setConfirmName(null) }}>{t('Back to classes')}</button>}
    </div>
    {error && <div className="alert alert-error" role="alert"><div><strong>{t('Request could not be completed')}</strong><p>{error}</p></div></div>}
    {notice && <div className="alert alert-success" role="status"><div><p>{notice}</p></div></div>}
    {mode === 'list' && <section className="panel">
      <div className="panel-heading"><div><div className="section-kicker">{t('CLASS CATALOG')}</div><h3>{t('Available templates')} <span className="count-badge">{classes.length}</span></h3></div><span className="class-refresh-note">{t('Refreshes every 8 seconds')}</span></div>
      {loading ? <div className="empty-state">{t('Loading Kubernetes resources…')}</div> : classes.length === 0 ? <div className="empty-state"><div className="empty-icon">▤</div><strong>{t('No environment classes yet')}</strong><p>{t('Create your first class using the form. It is stored in Kubernetes and becomes selectable when Ready.')}</p><button className="button button-primary" onClick={() => newClass()}>{t('Create class')}</button></div> : <div className="class-catalog">
        {classes.map(item => <button className="class-catalog-card" key={item.name} onClick={() => open(item.name)}><div className="class-card-head"><strong>{item.name}</strong><ClassState value={item}/></div><p>{item.provider} · {item.targetCluster}</p><div className="class-card-meta"><span>{t('Version')} {item.version}</span><span>{item.defaultRegion || '—'}</span><span>{t('{{count}} environments', { count: item.usageCount })}</span></div><small>{item.readyReason || t('Waiting for controller')}</small><span className="class-card-arrow">↗</span></button>)}
      </div>}
    </section>}
    {mode === 'detail' && !detail && <div className="loading-state">{t('Loading class configuration…')}</div>}
    {mode === 'detail' && detail && <>
      <section className="panel class-detail-panel"><div className="class-detail-head"><div><h3>{detail.name}</h3><p>{t('Version')} {detail.version} · {t('{{count}} environments', { count: detail.usageCount })}</p><ClassState value={detail}/></div><div className="class-detail-actions"><button className="button button-primary" disabled={!detail.ready} onClick={() => onUse(detail.name)}>{t('Use this class')}</button><button className="button button-secondary" disabled={detail.redacted || detail.deleting} onClick={() => newClass(detail)}>{t('Copy as new version')}</button><button className="button button-danger" disabled={detail.usageCount > 0 || detail.deleting || pending} onClick={() => setConfirmName('')}>{t('Delete class')}</button></div></div>
        <p className="class-help">{t('Class configuration is immutable. Create a new named version to change it. Existing environments keep their original class.')}</p>
        {detail.redacted && <p className="class-help">{t('Inline credentials were hidden. Copying this legacy class is disabled; create a class using credential references.')}</p>}
        <dl className="class-facts">
          <Fact label={t('Provider')} value={detail.provider}/><Fact label={t('Target cluster / context')} value={detail.targetCluster}/><Fact label={t('Default region')} value={detail.defaultRegion || '—'}/><Fact label={t('Allowed regions')} value={detail.allowedRegions?.join(', ') || t('Unrestricted')}/>
          <Fact label={t('Source repository URL')} value={detail.spec.source.url}/><Fact label={t('Pinned source commit')} value={detail.spec.source.revision}/><Fact label={t('Backend configuration name')} value={detail.spec.backend.configRef.name}/><Fact label={t('Runner image')} value={detail.spec.runnerProfile.image}/>
          <Fact label={t('Node range')} value={`${detail.capacityBounds.minNodeCount || 0} – ${detail.capacityBounds.maxNodeCount || t('Unlimited')}`}/><Fact label={t('Approval policy')} value={t(detail.spec.approvalPolicy)}/><Fact label={t('Spec digest')} value={detail.specDigest || t('Waiting for controller')}/>
        </dl>
      </section>
      {confirmName !== null && <section className="panel delete-confirm-panel"><h3>{t('Delete class')} {detail.name}</h3><p>{t('Only an unused class can be deleted. Referenced backend configuration and infrastructure are retained.')}</p><Field label={t('Confirm class name')} value={confirmName} onChange={setConfirmName}/><div className="delete-confirm-actions"><button className="button button-secondary" disabled={pending} onClick={() => setConfirmName(null)}>{t('Cancel')}</button><button className="button button-danger" disabled={pending || confirmName !== detail.name} onClick={() => void remove()}>{pending ? t('Deleting…') : t('Delete class')}</button></div></section>}
      <section className="panel class-condition-panel"><h3>{t('Controller status')}</h3>{detail.conditions?.length ? detail.conditions.map(condition => <div key={condition.type} className="class-condition"><strong>{condition.type}: {t(condition.status)} · {condition.reason}</strong><p>{condition.message}</p></div>) : <p>{t('Waiting for controller')}</p>}<p className="class-help">{t('Ready confirms the class configuration was admitted and reconciled. Infrastructure and runtime readiness are checked when an environment is created.')}</p></section>
      <details className="panel class-manifest"><summary>{t('View configuration manifest')}</summary><pre className="yaml-preview">{detail.yaml}</pre></details>
    </>}
    {mode === 'form' && <ClassForm key={copy?.uid || 'new'} source={copy} onCreated={created} onCancel={() => setMode('list')}/>}</div>
}

function ClassState({ value }: { value: ClassInfo }) {
  const { t } = useI18n()
  return <span className={`state-pill ${value.ready ? 'good' : 'warn'}`}>{value.deleting ? t('Deleting') : value.ready ? t('Ready') : t('Pending')}</span>
}

function Fact({ label, value }: { label: string; value: string }) { return <div><dt>{label}</dt><dd>{value}</dd></div> }

function Field({ label, value, onChange, required = false, hint, placeholder, type = 'text' }: { label: string; value: string | number; onChange: (value: string) => void; required?: boolean; hint?: string; placeholder?: string; type?: string }) {
  const id = useId()
  return <label htmlFor={id}><span className="field-label">{label}{required && <span className="class-required"> *</span>}</span><input id={id} className="text-input" type={type} min={type === 'number' ? 0 : undefined} step={type === 'number' ? 1 : undefined} value={value} required={required} placeholder={placeholder} onChange={event => onChange(event.target.value)}/>{hint && <small className="class-field-hint">{hint}</small>}</label>
}

function ClassForm({ source, onCreated, onCancel }: { source: ClassDetail | null; onCreated: (value: ClassDetail) => Promise<void>; onCancel: () => void }) {
  const { t } = useI18n()
  const [name, setName] = useState(source ? `${source.name}-next` : '')
  const [spec, setSpec] = useState<ClassSpec>(() => source ? { ...structuredClone(source.spec), version: `${source.version}-next` } : defaultSpec())
  const [runtimeJSON, setRuntimeJSON] = useState(() => JSON.stringify(source?.spec.runtimeProfile.runtimeObjects || [], null, 2))
  const [regionsText, setRegionsText] = useState(() => (source?.spec.allowedRegions || ['local']).join(', '))
  const [validation, setValidation] = useState<ClassValidation | null>(null)
  const [validatedBody, setValidatedBody] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { setValidation(null); setValidatedBody(''); setError('') }, [name, spec, runtimeJSON, regionsText])

  const body = () => {
    const objects: unknown = JSON.parse(runtimeJSON)
    if (!Array.isArray(objects) || objects.some(item => !item || typeof item !== 'object' || Array.isArray(item))) throw new Error(t('Runtime resources must be a JSON array of resource definitions.'))
    return JSON.stringify({ name, spec: { ...spec, allowedRegions: regionsText.split(',').map(value => value.trim()).filter(Boolean), runtimeProfile: { ...spec.runtimeProfile, runtimeObjects: objects as RuntimeObject[] } } })
  }
  const check = async () => {
    setPending(true); setError(''); setValidation(null)
    try { const payload = body(); const value = await request<ClassValidation>('/api/classes/validate', { method: 'POST', body: payload }); setValidation(value); setValidatedBody(value.valid ? payload : '') }
    catch (reason) { setError((reason as Error).message) }
    finally { setPending(false) }
  }
  const save = async () => {
    if (!validation?.valid || !validatedBody) return
    setPending(true); setError('')
    try { if (body() !== validatedBody) throw new Error(t('Validate the updated configuration before saving.')); const value = await request<ClassDetail>('/api/classes', { method: 'POST', body: validatedBody }); await onCreated(value) }
    catch (reason) { setError((reason as Error).message) }
    finally { setPending(false) }
  }
  const target = (patch: Partial<ClassSpec['target']>) => setSpec(value => ({ ...value, target: { ...value.target, ...patch } }))
  const runner = (key: 'image' | 'terraformVersion', value: string) => setSpec(current => ({ ...current, runnerProfile: { ...current.runnerProfile, [key]: value }, executor: { ...current.executor, [key]: value } }))
  const bounds = (key: keyof ClassInfo['capacityBounds'], value: string) => setSpec(current => ({ ...current, capacityBounds: { ...current.capacityBounds, [key]: Number(value) } }))
  return <form className="class-form" onSubmit={event => { event.preventDefault(); void check() }}>
    <p className="class-help">{source ? t('Copying {{name}}. Set a unique name and version; the source class stays unchanged.', { name: source.name }) : t('Complete the template below, validate it, then save the class.')}</p>
    <fieldset disabled={pending}>
      <section className="panel class-form-section"><h3>01 · {t('Identity and target')}</h3><div className="form-grid">
        <Field label={t('Class name')} value={name} onChange={setName} required placeholder="local-dev-v1" hint={t('A unique lowercase Kubernetes name.')}/>
        <Field label={t('Version')} value={spec.version} required onChange={value => setSpec(current => ({ ...current, version: value }))}/>
        <label><span className="field-label">{t('Provider')}</span><select className="text-input" value={spec.target.provider.toLowerCase()} onChange={event => target({ provider: event.target.value })}><option value="kind">Kind</option><option value="aws">AWS EKS</option></select></label>
        <Field label={t('Target cluster / context')} value={spec.target.clusterName} onChange={value => target({ clusterName: value })} required hint={t('For Kind, use a context configured in the controller, such as kind-pcp-dev.')}/>
        <Field label={t('Account')} value={spec.target.account || ''} onChange={value => target({ account: value })} required/>
        <Field label={t('Default region')} value={spec.target.region || ''} onChange={value => target({ region: value })} required/>
        <Field label={t('Allowed regions')} value={regionsText} onChange={setRegionsText} hint={t('Separate regions with commas. Include the default region; blank allows any region.')}/>
      </div>{spec.target.provider.toLowerCase() === 'aws' && <div className="form-grid class-advanced-fields">{(['clusterARN', 'executionRoleARN', 'runtimeRoleARN'] as const).map(key => <Field key={key} label={t(key)} value={spec.target[key] || ''} onChange={value => target({ [key]: value })}/>)}</div>}</section>
      <section className="panel class-form-section"><h3>02 · {t('Infrastructure source')}</h3><div className="form-grid">
        <Field label={t('Source repository URL')} value={spec.source.url} required onChange={value => setSpec(current => ({ ...current, source: { ...current.source, url: value } }))} placeholder="https://github.com/organization/infrastructure.git"/>
        <Field label={t('Pinned source commit')} value={spec.source.revision} required onChange={value => setSpec(current => ({ ...current, source: { ...current.source, revision: value } }))} hint={t('Use the full 40-64 character commit hash, not a branch or tag.')}/>
        <Field label={t('Terraform directory')} value={spec.source.path} required onChange={value => setSpec(current => ({ ...current, source: { ...current.source, path: value } }))} hint={t('Relative to the repository root; use . for the root.')}/>
      </div></section>
      <section className="panel class-form-section"><h3>03 · {t('Backend and runner')}</h3><div className="form-grid">
        <Field label={t('Backend configuration name')} value={spec.backend.configRef.name} required onChange={value => setSpec(current => ({ ...current, backend: { ...current.backend, configRef: { name: value } } }))} hint={t('Name of the ConfigMap containing backend.hcl in the environment namespace.')}/>
        <Field label={t('Runner service account')} value={spec.runnerProfile.serviceAccountName} required onChange={value => setSpec(current => ({ ...current, runnerProfile: { ...current.runnerProfile, serviceAccountName: value }, backend: { ...current.backend, authRef: { serviceAccountName: value } } }))}/>
        <Field label={t('Runner image')} value={spec.runnerProfile.image} required onChange={value => runner('image', value)}/>
        <Field label={t('Runner image digest / local identity')} value={spec.runnerProfile.imageDigest} required onChange={value => setSpec(current => ({ ...current, runnerProfile: { ...current.runnerProfile, imageDigest: value } }))} hint={t('Use a pinned digest for published images; local development may use a local image identity.')}/>
        <Field label={t('Terraform version')} value={spec.runnerProfile.terraformVersion} required onChange={value => runner('terraformVersion', value)}/>
        <Field label={t('Execution timeout')} value={spec.executor.executionTimeout} required onChange={value => setSpec(current => ({ ...current, executor: { ...current.executor, executionTimeout: value } }))} hint={t('Duration, for example 12m or 1h.')}/>
      </div></section>
      <section className="panel class-form-section"><h3>04 · {t('Regions, capacity and policy')}</h3><div className="form-grid">
        {([['minNodeCount', 'Minimum nodes'], ['maxNodeCount', 'Maximum nodes'], ['maxEnvironments', 'Environment limit'], ['maxConcurrentPlans', 'Concurrent plan limit'], ['maxConcurrentApplies', 'Concurrent apply limit']] as const).map(([key, label]) => <Field key={key} label={t(label)} type="number" value={spec.capacityBounds[key] || 0} onChange={value => bounds(key, value)}/>)}
        <label><span className="field-label">{t('Approval policy')}</span><select className="text-input" value={spec.approvalPolicy || 'Manual'} onChange={event => setSpec(current => ({ ...current, approvalPolicy: event.target.value as ClassSpec['approvalPolicy'] }))}><option value="Manual">{t('Manual')}</option><option value="Automatic">{t('Automatic')}</option></select><small className="class-field-hint">{t('Manual requires approval of the exact Terraform plan.')}</small></label>
      </div><p className="class-help">{t('Zero maximum means no class-specific limit; controller-wide concurrency limits still apply.')}</p></section>
      <details className="panel class-form-section class-advanced"><summary>{t('Advanced execution and runtime settings')}</summary><div className="form-grid class-advanced-fields">
        <Field label={t('Lock timeout')} value={spec.backend.lockTimeout} onChange={value => setSpec(current => ({ ...current, backend: { ...current.backend, lockTimeout: value } }))}/>
        <Field label={t('Executor working directory')} value={spec.executor.workDir} onChange={value => setSpec(current => ({ ...current, executor: { ...current.executor, workDir: value } }))}/>
        <Field label={t('Terraform parallelism')} type="number" value={spec.executor.parallelism || ''} onChange={value => setSpec(current => ({ ...current, executor: { ...current.executor, parallelism: value ? Number(value) : undefined } }))}/>
        {(['runtimeOS', 'runtimeArchitecture'] as const).map(key => <Field key={key} label={t(key)} value={spec.executor[key] || ''} onChange={value => setSpec(current => ({ ...current, executor: { ...current.executor, [key]: value } }))}/>)}
        {(['clusterID', 'incarnationID', 'connectionProfileRef'] as const).map(key => <Field key={key} label={t(key)} value={spec.target[key] || ''} onChange={value => target({ [key]: value })}/>)}
        <Field label={t('Management role ARN')} value={spec.runnerProfile.managementRoleARN || ''} onChange={value => setSpec(current => ({ ...current, runnerProfile: { ...current.runnerProfile, managementRoleARN: value } }))}/>
        {(['valkeyImage', 'operatorImage', 'operatorImageDigest', 'operatorManifestRef', 'networkPolicyProfile'] as const).map(key => <Field key={key} label={t(key)} value={spec.runtimeProfile[key] || ''} onChange={value => setSpec(current => ({ ...current, runtimeProfile: { ...current.runtimeProfile, [key]: value } }))}/>)}
        <Field label={t('Data retention policy')} value={spec.dataRetentionPolicy || ''} onChange={value => setSpec(current => ({ ...current, dataRetentionPolicy: value }))}/>
      </div><label className="class-runtime-json"><span className="field-label">{t('Runtime resources (JSON, optional)')}</span><textarea className="text-area" rows={7} value={runtimeJSON} onChange={event => setRuntimeJSON(event.target.value)}/><small className="class-field-hint">{t('Leave [] for no static runtime resources. Use credential references; inline Secret resources are not accepted.')}</small></label></details>
    </fieldset>
    {error && <div className="class-validation invalid" role="alert">{error}</div>}
    {validation && <section className={`panel class-validation ${validation.valid ? 'valid' : 'invalid'}`} role="status"><h3>{validation.valid ? t('Configuration validation passed') : t('Fix these configuration errors')}</h3>{validation.errors.map((value, index) => <p key={index}>{value}</p>)}{validation.valid && <p>{t('Backend configuration must exist in each environment namespace. Source access, runner images and infrastructure execution are checked when an environment is created.')}</p>}{validation.yaml && <details><summary>{t('View configuration manifest')}</summary><pre className="yaml-preview">{validation.yaml}</pre></details>}</section>}
    <div className="class-form-actions"><button type="button" className="button button-secondary" disabled={pending} onClick={onCancel}>{t('Cancel')}</button><button className="button button-secondary" type="submit" disabled={pending}>{pending ? t('Working…') : t('Validate configuration')}</button><button type="button" className="button button-primary" disabled={pending || !validation?.valid} onClick={() => void save()}>{t('Create class')}</button></div>
  </form>
}
