import { useState } from 'react'
import { useReactFlow } from '@xyflow/react'
import { useEditor } from '../store'
import { locate } from '../ops'

export function Issues() {
  const issues = useEditor((s) => s.issues)
  const loadError = useEditor((s) => s.loadError)
  const theme = useEditor((s) => s.theme)
  const select = useEditor((s) => s.select)
  const flow = useReactFlow()
  const [open, setOpen] = useState(false)
  const empty = Object.keys(theme.screens).length === 0
  const errors = issues.filter((i) => i.level === 'error').length
  const warnings = issues.length - errors

  const focus = (screen?: string, ref?: string) => {
    if (!screen) return
    if (ref && locate(theme, screen, ref)) select({ kind: 'button', screen, ref })
    else select({ kind: 'screen', id: screen })
    const node = flow.getNode(`s:${screen}`)
    if (node) flow.fitView({ nodes: [node], duration: 400, maxZoom: 1, padding: 0.6 })
  }

  if (loadError) {
    return (
      <div className="issues is-open">
        <div className="issues-head is-error">Тема не читается</div>
        <div className="issues-list"><div className="issue level-error">{loadError}</div></div>
      </div>
    )
  }

  if (empty) return null
  return (
    <div className={`issues${open ? ' is-open' : ''}`}>
      <button type="button" className={`issues-head${errors ? ' is-error' : warnings ? ' is-warn' : ' is-ok'}`} onClick={() => setOpen((v) => !v)} disabled={!issues.length}>
        <i className="issues-dot" />
        {issues.length === 0 ? 'Ошибок нет' : [errors ? `Ошибок: ${errors}` : '', warnings ? `Предупреждений: ${warnings}` : ''].filter(Boolean).join(' · ')}
      </button>
      {open && issues.length ? (
        <div className="issues-list">
          {issues.map((i, n) => (
            <button key={n} type="button" className={`issue level-${i.level}`} onClick={() => focus(i.screen, i.ref)}>
              {i.screen ? <code>{i.screen}</code> : i.ref ? <code>{i.ref.replace('command:', '/').replace(/^(event|route):/, '')}</code> : null}
              <span>{i.message}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
