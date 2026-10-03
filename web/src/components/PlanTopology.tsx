import { useLayoutEffect, useRef, useState } from 'react'
import { Box, GitBranch } from 'lucide-react'
import type { PlanGraph } from '../api'
import { useI18n } from '../i18n'

type Layout = { width: number; height: number; paths: string[] }

export function PlanTopology({ graph }: { graph: PlanGraph }) {
  const { t } = useI18n()
  const canvas = useRef<HTMLDivElement>(null)
  const [layout, setLayout] = useState<Layout>({ width: 1, height: 1, paths: [] })

  // Measure the rendered cards so long allowlisted values never overlap the next row.
  useLayoutEffect(() => {
    const element = canvas.current
    if (!element) return
    const measure = () => {
      const cards = new Map(
        Array.from(element.querySelectorAll<HTMLElement>('[data-address]')).map((card) => [card.dataset.address, card])
      )
      const paths = graph.edges.flatMap((edge) => {
        const from = cards.get(edge.from),
          to = cards.get(edge.to)
        if (!from || !to) return []
        const sameColumn = from.offsetLeft === to.offsetLeft
        const forward = to.offsetLeft > from.offsetLeft
        const downward = to.offsetTop > from.offsetTop
        const verticalDirection = downward ? 1 : -1
        const x1 = from.offsetLeft + (sameColumn ? from.offsetWidth / 2 : forward ? from.offsetWidth : 0)
        const y1 = from.offsetTop + (sameColumn ? (downward ? from.offsetHeight : 0) : from.offsetHeight / 2)
        const x2 = to.offsetLeft + (sameColumn ? to.offsetWidth / 2 : forward ? 0 : to.offsetWidth)
        const y2 = to.offsetTop + (sameColumn ? (downward ? 0 : to.offsetHeight) : to.offsetHeight / 2)
        return [
          sameColumn
          ? `M ${x1} ${y1} C ${x1} ${y1 + verticalDirection * 24}, ${x2} ${y2 - verticalDirection * 24}, ${x2} ${y2}`
            : `M ${x1} ${y1} C ${(x1 + x2) / 2} ${y1}, ${(x1 + x2) / 2} ${y2}, ${x2} ${y2}`,
        ]
      })
      setLayout({ width: element.clientWidth, height: element.clientHeight, paths })
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    element.querySelectorAll('[data-address]').forEach((card) => observer.observe(card))
    return () => observer.disconnect()
  }, [graph])

  return (
    <div className="topology-scroll" tabIndex={0} aria-label={t('Resource topology')}>
      <div className="graph-canvas" ref={canvas}>
        <svg className="graph-edges" viewBox={`0 0 ${layout.width} ${layout.height}`} aria-hidden="true">
          {layout.paths.map((path, index) => (
            <path key={index} d={path} />
          ))}
        </svg>
        {graph.nodes.map((node) => (
          <article key={node.address} data-address={node.address} className={`graph-node action-${node.action}`}>
            <div className="graph-node-head">
              <Box size={16} aria-hidden="true" />
              <span className="resource-type">{node.type}</span>
              <span className={`action-label ${node.action}`}>{t(node.action)}</span>
            </div>
            <code className="resource-address">{node.address}</code>
            <div className="node-columns">
              <div>
                <h4>{t('CURRENT')}</h4>
                <Metadata values={node.before} />
              </div>
              <div>
                <h4>{t('PLANNED')}</h4>
                <Metadata values={node.after} />
              </div>
            </div>
          </article>
        ))}
      </div>
      <div className="topology-caption">
        <GitBranch size={13} aria-hidden="true" />
        {t('Connections follow the saved Plan dependency graph.')}
      </div>
    </div>
  )
}

function Metadata({ values }: { values?: Record<string, unknown> }) {
  if (!values || Object.keys(values).length === 0) return <span className="metadata-empty">—</span>
  return (
    <dl className="metadata-list">
      {Object.entries(values).map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{typeof value === 'object' ? JSON.stringify(value) : String(value)}</dd>
        </div>
      ))}
    </dl>
  )
}
