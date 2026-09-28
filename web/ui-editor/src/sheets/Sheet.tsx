import type { ReactNode } from 'react'
import { useEditor } from '../store'

/** A centered sheet over the builder, closed with Done, Esc or a click outside. */
export function Sheet({ title, aside, children, wide }: { title: string; aside?: ReactNode; children: ReactNode; wide?: boolean }) {
  const close = () => useEditor.getState().setSheet('')
  return (
    <div className="sheet-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className={`sheet${wide ? ' is-wide' : ''}`} role="dialog" aria-modal="true" aria-label={title}>
        <header className="sheet-head">
          <h2>{title}</h2>
          <span className="spacer" />
          {aside}
          <button type="button" className="btn" onClick={close}>Готово</button>
        </header>
        <div className="sheet-body">{children}</div>
      </div>
    </div>
  )
}
