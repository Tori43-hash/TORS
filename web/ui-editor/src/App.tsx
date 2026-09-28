import { useEffect, useRef, useState } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import { exportable, useEditor } from './store'
import { formatTheme } from './wasm'
import { addScreen } from './ops'
import { Canvas } from './canvas/Canvas'
import { Inspector } from './inspector/Inspector'
import { Simulator } from './simulator/Simulator'
import { Issues } from './ui/Issues'
import { Segmented } from './inspector/fields'
import type { Manifest, Theme } from './types'

function useShortcuts() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (el.closest('input, textarea, select, [contenteditable]')) return
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'z') {
        e.preventDefault()
        e.shiftKey ? useEditor.getState().redo() : useEditor.getState().undo()
      } else if (mod && e.key.toLowerCase() === 'y') {
        e.preventDefault()
        useEditor.getState().redo()
      } else if (e.key === 'Escape') useEditor.getState().select(null)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}

function openJson(text: string) {
  const { replaceTheme, replaceManifest, notify } = useEditor.getState()
  try {
    const v = JSON.parse(text) as Partial<Theme & Manifest>
    if (v.screens) {
      replaceTheme(v as Theme)
      notify('Тема открыта')
    } else if (v.data && v.actions) {
      replaceManifest(v as Manifest)
      notify('Манифест бота загружен')
    } else notify('Это не тема и не манифест TORS')
  } catch {
    notify('Не удалось прочитать JSON')
  }
}

async function exportTheme(copy: boolean) {
  const s = useEditor.getState()
  const errors = s.issues.filter((i) => i.level === 'error').length
  if (errors && !window.confirm(`В теме ошибок: ${errors}. Бот не примет такую тему. Всё равно ${copy ? 'скопировать' : 'скачать'}?`)) return
  const res = await formatTheme(exportable(s))
  if (!res.json) return s.notify(`Не удалось собрать JSON: ${res.error}`)
  if (copy) {
    try {
      await navigator.clipboard.writeText(res.json)
      s.notify('JSON темы скопирован')
    } catch {
      s.notify('Браузер не дал доступ к буферу обмена')
    }
    return
  }
  const url = URL.createObjectURL(new Blob([res.json + '\n'], { type: 'application/json' }))
  const a = Object.assign(document.createElement('a'), { href: url, download: 'theme.json' })
  a.click()
  URL.revokeObjectURL(url)
}

function Menu({ label, left, children }: { label: string; left?: boolean; children: (close: () => void) => React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])
  return (
    <div className={`menu${left ? ' menu-left' : ''}`} ref={ref}>
      <button type="button" className="bar-btn" onClick={() => setOpen((v) => !v)} aria-expanded={open}>{label}</button>
      {open ? <div className="popover menu-pop">{children(() => setOpen(false))}</div> : null}
    </div>
  )
}

function Toolbar() {
  const lang = useEditor((s) => s.lang)
  const langs = useEditor((s) => s.manifest.languages)
  const tgDark = useEditor((s) => s.tgDark)
  const renderer = useEditor((s) => s.renderer)
  const simulator = useEditor((s) => s.simulator)
  const canUndo = useEditor((s) => s.past.length > 0)
  const canRedo = useEditor((s) => s.future.length > 0)
  const st = useEditor.getState()
  const file = useRef<HTMLInputElement>(null)

  return (
    <header className="toolbar">
      <div className="brand">
        <span className="brand-mark" aria-hidden />
        <span className="brand-name">TORS</span>
        <span className="brand-sub">Редактор экранов</span>
      </div>
      <div className="bar-group">
        <Menu label="Файл" left>
          {(close) => (
            <>
              <button type="button" className="popover-item" onClick={() => { file.current?.click(); close() }}>Открыть тему или манифест…</button>
              <button type="button" className="popover-item" onClick={() => { void st.resetStandard(); close() }}>Стандартная тема</button>
              <button type="button" className="popover-item" onClick={() => { st.relayout(); close() }}>Расставить экраны заново</button>
            </>
          )}
        </Menu>
        <button type="button" className="bar-btn" onClick={() => {
          let id = ''
          st.edit((t) => void (id = addScreen(t)))
          st.select({ kind: 'screen', id })
        }}>+ Экран</button>
        <button type="button" className="bar-icon" title="Отменить (⌘Z)" disabled={!canUndo} onClick={st.undo}>
          <svg viewBox="0 0 16 16"><path d="M6 4 3 7l3 3M3.5 7H10a3 3 0 0 1 0 6H8" /></svg>
        </button>
        <button type="button" className="bar-icon" title="Повторить (⇧⌘Z)" disabled={!canRedo} onClick={st.redo}>
          <svg viewBox="0 0 16 16"><path d="m10 4 3 3-3 3M12.5 7H6a3 3 0 0 0 0 6h2" /></svg>
        </button>
      </div>
      <span className="spacer" />
      <div className="bar-group">
        {langs.length > 1 ? <Segmented value={lang} onChange={st.setLang} options={langs.map((l) => ({ value: l, label: l.toUpperCase() }))} /> : null}
        <Segmented value={renderer} onChange={st.setRenderer} options={[{ value: 'rich', label: 'Rich', title: 'Rich messages (Bot API 10.1+)' }, { value: 'classic', label: 'Classic', title: 'Обычные сообщения — запасной режим' }]} />
        <button type="button" className="bar-icon" title={tgDark ? 'Светлая тема Telegram' : 'Тёмная тема Telegram'} onClick={() => st.setTgDark(!tgDark)}>
          {tgDark ? (
            <svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="3" /><path d="M8 1.5v1.5M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1" /></svg>
          ) : (
            <svg viewBox="0 0 16 16"><path d="M13 9.5A5.5 5.5 0 0 1 6.5 3a5.5 5.5 0 1 0 6.5 6.5Z" /></svg>
          )}
        </button>
      </div>
      <div className="bar-group">
        <button type="button" className={`bar-btn${simulator ? ' is-on' : ''}`} onClick={() => st.setSimulator(!simulator)}>
          <svg viewBox="0 0 16 16" className="bar-glyph"><path d="M5 3.5v9l7-4.5-7-4.5Z" /></svg>
          Проверить
        </button>
        <Menu label="Экспорт">
          {(close) => (
            <>
              <button type="button" className="popover-item" onClick={() => { void exportTheme(false); close() }}>Скачать theme.json</button>
              <button type="button" className="popover-item" onClick={() => { void exportTheme(true); close() }}>Скопировать JSON</button>
            </>
          )}
        </Menu>
      </div>
      <input
        ref={file}
        type="file"
        accept="application/json,.json"
        hidden
        onChange={async (e) => {
          const f = e.target.files?.[0]
          if (f) openJson(await f.text())
          e.target.value = ''
        }}
      />
    </header>
  )
}

function Toast() {
  const toast = useEditor((s) => s.toast)
  if (!toast) return null
  return (
    <div className="toast" role="status" key={toast.id}>
      {toast.text}
      {toast.undo ? <button type="button" onClick={() => { useEditor.getState().undo(); useEditor.setState({ toast: undefined }) }}>Отменить</button> : null}
    </div>
  )
}

export function App() {
  const ready = useEditor((s) => s.ready)
  const engineError = useEditor((s) => s.engineError)
  const simulator = useEditor((s) => s.simulator)
  useShortcuts()
  useEffect(() => void useEditor.getState().init(), [])
  useEffect(() => {
    const over = (e: DragEvent) => e.dataTransfer?.types.includes('Files') && e.preventDefault()
    const drop = async (e: DragEvent) => {
      const f = e.dataTransfer?.files[0]
      if (!f) return
      e.preventDefault()
      openJson(await f.text())
    }
    window.addEventListener('dragover', over)
    window.addEventListener('drop', drop)
    return () => {
      window.removeEventListener('dragover', over)
      window.removeEventListener('drop', drop)
    }
  }, [])

  if (engineError) {
    return <div className="splash">Не удалось запустить движок проверки: {engineError}</div>
  }
  if (!ready) {
    return (
      <div className="splash">
        <span className="spinner" />
        Загружаем редактор…
      </div>
    )
  }
  return (
    <div className="app">
      <Toolbar />
      <main className="workspace">
        <ReactFlowProvider>
          <div className="canvas-wrap">
            <Canvas />
            <Issues />
          </div>
        </ReactFlowProvider>
        {simulator ? <Simulator /> : <Inspector />}
      </main>
      <Toast />
    </div>
  )
}
