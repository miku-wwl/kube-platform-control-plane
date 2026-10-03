import { useCallback, useEffect, useMemo, useState } from 'react'
import { Boxes, ChevronRight, Layers, LayoutDashboard, Plus, RefreshCw } from 'lucide-react'
import {
  request,
  envPath,
  type Approval,
  type ClassInfo,
  type Environment,
  type EnvironmentDetail,
  type PlanView,
  type TerraformRun,
} from './api'
import { useI18n } from './i18n'
import { ClassManager } from './ClassManager'
import { Alert, LoadingState } from './components/ui'
import { Builder, Dashboard, Detail } from './ConsoleViews'

type Screen = 'dashboard' | 'builder' | 'detail' | 'classes'
type DetailTab = 'overview' | 'architecture' | 'terraform' | 'timeline'
type Notice = { key: string; values?: Record<string, string | number> }

function App() {
  const { language, setLanguage, t } = useI18n()
  const [screen, setScreen] = useState<Screen>(() =>
    window.location.hash === '#classes' ? 'classes' : window.location.hash === '#builder' ? 'builder' : 'dashboard'
  )
  const [tab, setTab] = useState<DetailTab>('overview')
  const [environments, setEnvironments] = useState<Environment[]>([])
  const [classes, setClasses] = useState<ClassInfo[]>([])
  const [builderClass, setBuilderClass] = useState('')
  const [selected, setSelected] = useState<Pick<Environment, 'namespace' | 'name'> | null>(null)
  const [detail, setDetail] = useState<EnvironmentDetail | null>(null)
  const [plan, setPlan] = useState<PlanView | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)
  const [apiState, setApiState] = useState<'connecting' | 'connected' | 'unavailable'>('connecting')

  useEffect(() => {
    const hash = screen === 'classes' ? '#classes' : screen === 'builder' ? '#builder' : ''
    window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}${hash}`)
  }, [screen])

  const refresh = useCallback(async () => {
    try {
      const [items, classItems] = await Promise.all([
        request<Environment[]>('/api/environments'),
        request<ClassInfo[]>('/api/classes'),
      ])
      setEnvironments(items)
      setClasses(classItems)
      setApiState('connected')
      setError('')
    } catch (value) {
      setApiState('unavailable')
      setError((value as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  const openEnvironment = useCallback(async (environment: Pick<Environment, 'namespace' | 'name'>) => {
    setSelected(environment)
    setScreen('detail')
    setTab('overview')
    setDetail(null)
    setPlan(null)
    setError('')
    try {
      const value = await request<EnvironmentDetail>(envPath(environment))
      setDetail(value)
      const planRun = value.infrastructureDetail?.latestPlanRun
      if (planRun)
        setPlan(
          await request<PlanView>(
            `/api/terraform-runs/${encodeURIComponent(environment.namespace)}/${encodeURIComponent(planRun)}/plan`
          )
        )
    } catch (reason) {
      setError((reason as Error).message)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 8000)
    return () => window.clearInterval(timer)
  }, [refresh])
  useEffect(() => {
    if (!selected || screen !== 'detail') return
    let active = true
    const load = async () => {
      try {
        const value = await request<EnvironmentDetail>(envPath(selected))
        if (!active) return
        setDetail(value)
        const latest = value.infrastructureDetail?.latestPlanRun
        setPlan(
          latest
            ? await request<PlanView>(
                `/api/terraform-runs/${encodeURIComponent(selected.namespace)}/${encodeURIComponent(latest)}/plan`
              )
            : null
        )
      } catch (reason) {
        if (!active) return
        if ((reason as Error).message === 'PlatformEnvironment was not found') {
          setSelected(null)
          setDetail(null)
          setPlan(null)
          setScreen('dashboard')
          setError('')
          setNotice({ key: 'Environment removed after finalizer cleanup.' })
          void refresh()
        } else {
          setError((reason as Error).message)
        }
      }
    }
    const timer = window.setInterval(() => void load(), 6000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [selected, screen, refresh, t])

  const readyCount = useMemo(() => environments.filter((item) => item.ready).length, [environments])
  const navigate = (next: Screen) => {
    setScreen(next)
    setError('')
    setNotice(null)
  }

  const approve = async (run: TerraformRun) => {
    if (!selected) return
    setBusy(true)
    setError('')
    setNotice(null)
    try {
      const result = await request<Approval>(
        `/api/terraform-runs/${encodeURIComponent(run.namespace)}/${encodeURIComponent(run.name)}/approve`,
        {
          method: 'POST',
          body: JSON.stringify({
            planRunUID: run.uid,
            planDigest: run.planDigest,
            executionContextDigest: run.executionContextDigest,
            effectivePlanInputDigest: run.effectivePlanInputDigest,
            planReportRef: run.planReportRef,
            planReportDigest: run.planReportDigest,
          }),
        }
      )
      setNotice({
        key: 'Approval {{name}} recorded against this exact plan. The existing controller will reconcile it.',
        values: { name: result.name },
      })
      await openEnvironment(selected)
      await refresh()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const created = async (environment: Pick<Environment, 'namespace' | 'name'>) => {
    await refresh()
    setNotice({
      key: 'PlatformEnvironment submitted. Terraform Plan and all later steps remain under the existing control plane.',
    })
    await openEnvironment(environment)
  }

  const updateCapacity = async (nodeCount: number) => {
    if (!detail) return
    setBusy(true)
    setError('')
    setNotice(null)
    try {
      await request(envPath(detail), {
        method: 'PUT',
        body: JSON.stringify({ uid: detail.uid, generation: detail.generation, nodeCount }),
      })
    } catch (reason) {
      const message = (reason as Error).message
      if (message.includes('StaleEnvironment')) {
        setError(t('The environment changed while this page was open. Refresh the detail and try again.'))
      } else {
        setError(message)
      }
    } finally {
      setBusy(false)
      await openEnvironment(detail)
    }
  }

  const removeEnvironment = async () => {
    if (!detail || detail.deleting) return
    setBusy(true)
    setError('')
    setNotice(null)
    try {
      await request(envPath(detail), { method: 'DELETE', body: JSON.stringify({ uid: detail.uid }) })
      setNotice({
        key: 'Deletion requested. Cleanup is running through existing finalizers; this console does not execute Terraform.',
      })
      await refresh()
    } catch (reason) {
      setError((reason as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const connectionLabel =
    apiState === 'connected' ? t('API connected') : apiState === 'connecting' ? t('Connecting…') : t('API unavailable')
  return (
    <div className="app-shell">
      <a href="#main-content" className="skip-link">
        {t('Skip to content')}
      </a>
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <Layers size={21} aria-hidden="true" />
          </div>
          <div className="brand-copy">
            <strong>KPCP</strong>
            <small>{t('Platform Control Plane')}</small>
          </div>
        </div>
        <div className="workspace-label">{t('WORKSPACE')}</div>
        <nav className="primary-nav" aria-label={t('Workspace navigation')}>
          <div className="nav-section-label">{t('Operate')}</div>
          <button
            aria-label={t('Environments')}
            aria-current={screen === 'dashboard' || screen === 'detail' ? 'page' : undefined}
            title={t('Environments')}
            className={screen === 'dashboard' || screen === 'detail' ? 'active' : ''}
            onClick={() => navigate('dashboard')}
          >
            <LayoutDashboard size={18} aria-hidden="true" />
            <span className="nav-label">{t('Environments')}</span>
            <span className="nav-count">{environments.length}</span>
          </button>
          <div className="nav-section-label">{t('Build')}</div>
          <button
            aria-label={t('Platform builder')}
            aria-current={screen === 'builder' ? 'page' : undefined}
            title={t('Platform builder')}
            className={screen === 'builder' ? 'active' : ''}
            onClick={() => navigate('builder')}
          >
            <Boxes size={18} aria-hidden="true" />
            <span className="nav-label">{t('Platform builder')}</span>
          </button>
          <button
            aria-label={t('Environment classes')}
            aria-current={screen === 'classes' ? 'page' : undefined}
            title={t('Environment classes')}
            className={screen === 'classes' ? 'active' : ''}
            onClick={() => navigate('classes')}
          >
            <Layers size={18} aria-hidden="true" />
            <span className="nav-label">{t('Environment classes')}</span>
            <span className="nav-count">{classes.length}</span>
          </button>
        </nav>
        <div className="sidebar-bottom">
          <div
            className="local-indicator"
            role="status"
            aria-label={t('LOCAL MODE') + ' · Kind + LocalStack'}
            title={t('LOCAL MODE') + ' · Kind + LocalStack'}
          >
            <span className="dot good" />
            <span>{t('LOCAL MODE')}</span>
            <small className="rail-mode">{t('LOCAL')}</small>
          </div>
          <p>Kind + LocalStack</p>
          <small>{t('Local workspace')}</small>
        </div>
      </aside>
      <main className="main-area" id="main-content" tabIndex={-1}>
        <header className="topbar">
          <div className="topbar-context">
            <span className="context-label">{t('Local workspace')}</span>
            <ChevronRight size={14} aria-hidden="true" />
            <h1>
              {screen === 'classes'
                ? t('Environment classes')
                : screen === 'builder'
                  ? t('Platform builder')
                  : screen === 'detail'
                    ? selected?.name || t('Environment')
                    : t('Environments')}
            </h1>
          </div>
          <div className="top-actions">
            <div className="language-switch" role="group" aria-label={t('Language')}>
              <button
                type="button"
                aria-label={t('Switch language to English')}
                aria-pressed={language === 'en'}
                className={language === 'en' ? 'selected' : ''}
                onClick={() => setLanguage('en')}
              >
                EN
              </button>
              <button
                type="button"
                aria-label={t('Switch language to Chinese')}
                aria-pressed={language === 'zh'}
                className={language === 'zh' ? 'selected' : ''}
                onClick={() => setLanguage('zh')}
              >
                中文
              </button>
            </div>
            <span className={`connected ${apiState}`} role="status" title={connectionLabel}>
              <i />
              <span>{connectionLabel}</span>
            </span>
            <button
              className="icon-button"
              title={t('Refresh')}
              aria-label={t('Refresh')}
              onClick={() => void refresh()}
            >
              <RefreshCw size={17} aria-hidden="true" />
            </button>
            <button className="button button-primary" onClick={() => navigate('builder')}>
              <Plus size={16} aria-hidden="true" />
              {t('New environment')}
            </button>
          </div>
        </header>
        <div className="global-feedback">
          {error && (
            <Alert tone="error" title={t('Request could not be completed')} onClose={() => setError('')}>
              {error}
            </Alert>
          )}
          {notice && (
            <Alert tone="success" title={t('Workflow updated')} onClose={() => setNotice(null)}>
              {t(notice.key, notice.values)}
            </Alert>
          )}
        </div>
        {screen === 'dashboard' && (
          <Dashboard
            environments={environments}
            readyCount={readyCount}
            loading={loading}
            apiState={apiState}
            onOpen={openEnvironment}
            onBuild={() => navigate('builder')}
          />
        )}
        {screen === 'builder' && (
          <Builder
            classes={classes}
            initialClass={builderClass}
            onManageClasses={() => navigate('classes')}
            onCancel={() => navigate('dashboard')}
            onCreated={created}
          />
        )}
        {screen === 'classes' && (
          <ClassManager
            classes={classes}
            loading={loading}
            onChanged={refresh}
            onUse={(name) => {
              setBuilderClass(name)
              navigate('builder')
            }}
          />
        )}
        {screen === 'detail' && detail && (
          <Detail
            key={detail.uid}
            detail={detail}
            plan={plan}
            tab={tab}
            busy={busy}
            onBack={() => navigate('dashboard')}
            onTab={setTab}
            onApprove={approve}
            onUpdate={updateCapacity}
            onDelete={removeEnvironment}
            onOpenRun={() => setTab('terraform')}
          />
        )}
        {screen === 'detail' && !detail && !error && <LoadingState label={t('Loading environment evidence…')} />}
      </main>
    </div>
  )
}

export default App
