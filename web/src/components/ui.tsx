import type { ReactNode } from 'react'
import {
  AlertCircle,
  AlertTriangle,
  Check,
  CheckCircle2,
  CircleDot,
  Info,
  LoaderCircle,
  X,
  type LucideIcon,
} from 'lucide-react'
import { useI18n } from '../i18n'

export function StatusBadge({ value }: { value: string }) {
  const { t } = useI18n()
  const state = value.toLowerCase()
  const tone = ['ready', 'recorded', 'approved', 'succeeded', 'online', 'true'].includes(state)
    ? 'good'
    : state.includes('fail') || state.includes('reject') || state.includes('error') || state === 'blocked'
      ? 'bad'
      : ['waiting', 'pending', 'running', 'applying', 'notready', 'blocked', 'fenced', 'reconcil'].some((part) =>
            state.includes(part)
          ) || ['pending', 'required', 'reconciling', 'running', 'progressing', 'deleting', 'false'].includes(state)
        ? 'warn'
        : 'neutral'
  const Icon = tone === 'good' ? CheckCircle2 : tone === 'bad' ? AlertCircle : CircleDot
  return (
    <span className={`state-pill ${tone}`}>
      <Icon size={13} aria-hidden="true" />
      {t(value || 'Unknown')}
    </span>
  )
}

export function MetricCard({
  label,
  value,
  icon: Icon,
  tone = '',
  note,
}: {
  label: string
  value: ReactNode
  icon: LucideIcon
  tone?: string
  note?: string
}) {
  return (
    <div className={`metric-card ${tone}`}>
      <div className="metric-heading">
        <span>{label}</span>
        <Icon size={17} aria-hidden="true" />
      </div>
      <strong>{value}</strong>
      {note && <small>{note}</small>}
    </div>
  )
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  compact = false,
}: {
  icon: LucideIcon
  title: string
  description?: string
  action?: ReactNode
  compact?: boolean
}) {
  return (
    <div className={`empty-state ${compact ? 'compact' : ''}`}>
      <div className="empty-icon">
        <Icon size={23} aria-hidden="true" />
      </div>
      <strong>{title}</strong>
      {description && <p>{description}</p>}
      {action}
    </div>
  )
}

export function LoadingState({ label }: { label: string }) {
  return (
    <div className="loading-state" role="status">
      <LoaderCircle size={20} className="spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}

export function Alert({
  tone = 'info',
  title,
  children,
  onClose,
}: {
  tone?: 'error' | 'success' | 'warning' | 'info'
  title?: string
  children: ReactNode
  onClose?: () => void
}) {
  const { t } = useI18n()
  const Icon =
    tone === 'error' ? AlertCircle : tone === 'success' ? CheckCircle2 : tone === 'warning' ? AlertTriangle : Info
  return (
    <div className={`alert alert-${tone}`} role={tone === 'error' ? 'alert' : 'status'}>
      <Icon size={18} aria-hidden="true" />
      <div>
        {title && <strong>{title}</strong>}
        <div className="alert-message">{children}</div>
      </div>
      {onClose && (
        <button className="icon-button" type="button" aria-label={t('Close')} onClick={onClose}>
          <X size={16} aria-hidden="true" />
        </button>
      )}
    </div>
  )
}

export function Stepper({ steps, active }: { steps: string[]; active: number }) {
  const { t } = useI18n()
  return (
    <ol className="workflow-steps" aria-label={t('Workflow steps')}>
      {steps.map((step, index) => (
        <li
          key={step}
          className={index === active ? 'active' : index < active ? 'complete' : ''}
          aria-current={index === active ? 'step' : undefined}
        >
          <span className="step-number">
            {index < active ? <Check size={14} aria-hidden="true" /> : String(index + 1).padStart(2, '0')}
          </span>
          <span>{step}</span>
        </li>
      ))}
    </ol>
  )
}

export function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  const locale = document.documentElement.lang.startsWith('zh') ? 'zh-CN' : 'en-NZ'
  return Number.isNaN(date.getTime())
    ? '—'
    : date.toLocaleString(locale, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function short(value: string) {
  return value.length > 34 ? `${value.slice(0, 13)}…${value.slice(-12)}` : value
}
