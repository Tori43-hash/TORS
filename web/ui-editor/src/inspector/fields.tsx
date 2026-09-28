import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'

export function Section({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="ins-section">
      <header>
        <h3>{title}</h3>
        {aside}
      </header>
      {children}
    </section>
  )
}

export function Field({ label, hint, children }: { label?: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="field">
      {label ? <span className="field-label">{label}</span> : null}
      {children}
      {hint ? <span className="field-hint">{hint}</span> : null}
    </label>
  )
}

/** Text input that commits on every keystroke but keeps the caret stable. */
export function TextInput({ value, onChange, placeholder, mono, onCommit, autoFocus }: {
  value: string
  onChange?(v: string): void
  onCommit?(v: string): void
  placeholder?: string
  mono?: boolean
  autoFocus?: boolean
}) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])
  return (
    <input
      className={`input${mono ? ' mono' : ''}`}
      value={draft}
      placeholder={placeholder}
      autoFocus={autoFocus}
      spellCheck={false}
      onChange={(e) => {
        setDraft(e.target.value)
        onChange?.(e.target.value)
      }}
      onBlur={() => onCommit && draft !== value && onCommit(draft)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
        if (e.key === 'Escape') {
          setDraft(value)
          ;(e.target as HTMLInputElement).blur()
        }
      }}
    />
  )
}

export function TextArea({ value, onChange, placeholder, areaRef }: {
  value: string
  onChange(v: string): void
  placeholder?: string
  areaRef?: React.RefObject<HTMLTextAreaElement | null>
}) {
  const own = useRef<HTMLTextAreaElement>(null)
  const ref = areaRef ?? own
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = '0px'
    el.style.height = `${el.scrollHeight + 2}px`
  }, [value, ref])
  return (
    <textarea
      ref={ref}
      className="input textarea"
      value={value}
      placeholder={placeholder}
      spellCheck={false}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

export function Segmented<T extends string>({ options, value, onChange }: {
  options: { value: T; label: ReactNode; title?: string }[]
  value: T
  onChange(v: T): void
}) {
  return (
    <div className="segmented" role="radiogroup">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          title={o.title}
          className={o.value === value ? 'is-on' : ''}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Select({ value, onChange, options, placeholder }: {
  value: string
  onChange(v: string): void
  options: { value: string; label: string; group?: string }[]
  placeholder?: string
}) {
  const groups = [...new Set(options.map((o) => o.group ?? ''))]
  return (
    <div className="select">
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {placeholder !== undefined ? <option value="">{placeholder}</option> : null}
        {groups.map((g) =>
          g ? (
            <optgroup key={g} label={g}>
              {options.filter((o) => o.group === g).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </optgroup>
          ) : (
            options.filter((o) => !o.group).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)
          ),
        )}
      </select>
      <svg viewBox="0 0 12 12" aria-hidden><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></svg>
    </div>
  )
}

export function LangTabs({ langs, value, onChange, filled }: { langs: string[]; value: string; onChange(l: string): void; filled: (l: string) => boolean }) {
  if (langs.length < 2) return null
  return (
    <div className="lang-tabs">
      {langs.map((l) => (
        <button key={l} type="button" className={l === value ? 'is-on' : ''} onClick={() => onChange(l)}>
          {l.toUpperCase()}
          {!filled(l) ? <i className="lang-missing" title="Нет перевода" /> : null}
        </button>
      ))}
    </div>
  )
}

export function IconButton({ title, onClick, children, danger, disabled }: { title: string; onClick(): void; children: ReactNode; danger?: boolean; disabled?: boolean }) {
  return (
    <button type="button" className={`icon-btn${danger ? ' is-danger' : ''}`} title={title} aria-label={title} onClick={onClick} disabled={disabled}>
      {children}
    </button>
  )
}

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange(v: boolean): void; label: string }) {
  return (
    <label className="toggle">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span className="toggle-track"><span className="toggle-thumb" /></span>
      <span>{label}</span>
    </label>
  )
}

export interface VarGroup {
  title: string
  items: { expr: string; title: string; sample?: string }[]
}

/** A popover with template variables and helpers to insert at the caret. */
export function VarMenu({ groups, onPick }: { groups: VarGroup[]; onPick(expr: string): void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])
  return (
    <div className="var-menu" ref={ref}>
      <button type="button" className="tool-btn" onClick={() => setOpen((v) => !v)} title="Вставить переменную">
        {'{ }'} Переменная
      </button>
      {open ? (
        <div className="popover">
          {groups.filter((g) => g.items.length).map((g) => (
            <div key={g.title} className="popover-group">
              <div className="popover-title">{g.title}</div>
              {g.items.map((i) => (
                <button
                  key={i.expr}
                  type="button"
                  className="popover-item"
                  onClick={() => {
                    onPick(i.expr)
                    setOpen(false)
                  }}
                >
                  <span>{i.title}</span>
                  <code>{i.sample ?? i.expr}</code>
                </button>
              ))}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

export const Icon = {
  up: <svg viewBox="0 0 16 16"><path d="M8 12.5v-9M4 7l4-4 4 4" /></svg>,
  down: <svg viewBox="0 0 16 16"><path d="M8 3.5v9M4 9l4 4 4-4" /></svg>,
  left: <svg viewBox="0 0 16 16"><path d="M12.5 8h-9M7 4 3 8l4 4" /></svg>,
  right: <svg viewBox="0 0 16 16"><path d="M3.5 8h9M9 4l4 4-4 4" /></svg>,
  close: <svg viewBox="0 0 16 16"><path d="m4 4 8 8M12 4l-8 8" /></svg>,
  plus: <svg viewBox="0 0 16 16"><path d="M8 3v10M3 8h10" /></svg>,
  back: <svg viewBox="0 0 16 16"><path d="M10 3.5 5.5 8l4.5 4.5" /></svg>,
  trash: <svg viewBox="0 0 16 16"><path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5" /></svg>,
  row: <svg viewBox="0 0 16 16"><path d="M2.5 6h11v4h-11z" /></svg>,
  copy: <svg viewBox="0 0 16 16"><rect x="5.5" y="5.5" width="7.5" height="7.5" rx="1.5" /><path d="M10.5 3H4.5A1.5 1.5 0 0 0 3 4.5v6" /></svg>,
}
