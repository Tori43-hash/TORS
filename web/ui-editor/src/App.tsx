import { useEffect, useRef } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import { useEditor } from './store'
import { addScreen, toEntry } from './ops'
import { importBot, type Bot } from './market'
import { Canvas } from './canvas/Canvas'
import { Inspector } from './inspector/Inspector'
import { Simulator } from './simulator/Simulator'
import { Issues } from './ui/Issues'
import { Segmented } from './inspector/fields'
import { MarketSheet } from './sheets/Market'
import { ExportSheet } from './sheets/Export'
import type { MarketModule, Pack, Theme } from './types'

function useShortcuts() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (el.closest('input, textarea, select, [contenteditable]')) return
      const mod = e.metaKey || e.ctrlKey
      const st = useEditor.getState()
      if (mod && e.key.toLowerCase() === 'z') {
        e.preventDefault()
        e.shiftKey ? st.redo() : st.undo()
      } else if (mod && e.key.toLowerCase() === 'y') {
        e.preventDefault()
        st.redo()
      } else if (e.key === 'Escape') {
        if (st.sheet) st.setSheet('')
        else st.select(null)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}

/** Opens a bot.json, a tors-module.json or a bare theme. */
export function openFile(text: string) {
  const st = useEditor.getState()
  let v: Partial<Bot & Pack & Theme>
  try {
    v = JSON.parse(text)
  } catch {
    return st.notify('Не удалось прочитать JSON')
  }
  if (v.apps) {
    const { modules, theme, unknown } = importBot(v as Bot, st.all)
    st.replaceProject({ modules, theme: theme ?? { version: 1, commands: { start: { default: '' } }, screens: {} }, positions: theme?.editor?.positions ?? {} })
    st.notify(unknown.length ? `Бот открыт. Нет в маркете: ${unknown.join(', ')}` : 'Бот открыт')
  } else if (Array.isArray(v.modules)) {
    st.notify(st.addOwn(v.modules as MarketModule[]))
    st.setSheet('market')
  } else if (v.screens) {
    st.replaceProject({ theme: v as Theme, positions: (v as Theme).editor?.positions ?? {} })
    st.notify('Экраны открыты')
  } else st.notify('Это не bot.json, не tors-module.json и не тема')
}

/** Adds a screen; the first one becomes what /start opens. */
export function newScreen() {
  const st = useEditor.getState()
  let id = ''
  st.edit((t) => {
    id = addScreen(t)
    if (!toEntry(t.commands?.start).default) (t.commands ??= {}).start = { ...toEntry(t.commands?.start), default: id }
  })
  st.select({ kind: 'screen', id })
}

function Toolbar() {
  const lang = useEditor((s) => s.lang)
  const langs = useEditor((s) => s.manifest.languages)
  const simulator = useEditor((s) => s.simulator)
  const st = useEditor.getState()
  const file = useRef<HTMLInputElement>(null)

  return (
    <header className="toolbar">
      <div className="brand">
        <span className="brand-name">TORS</span>
        <span className="brand-sub">Конструктор бота</span>
      </div>
      <div className="bar-group">
        <button type="button" className="btn" onClick={() => st.setSheet('market')}>Модули</button>
        <button type="button" className="btn" onClick={newScreen}>Новый экран</button>
      </div>
      <span className="spacer" />
      <div className="bar-group">
        {langs.length > 1 ? <Segmented value={lang} onChange={st.setLang} options={langs.map((l) => ({ value: l, label: l.toUpperCase() }))} /> : null}
        <button type="button" className={`btn${simulator ? ' is-on' : ''}`} onClick={() => st.setSimulator(!simulator)}>Проверить</button>
        <button type="button" className="btn" onClick={() => file.current?.click()}>Открыть</button>
        <button type="button" className="btn btn-primary" onClick={() => st.setSheet('export')}>Экспорт</button>
      </div>
      <input
        ref={file}
        type="file"
        accept="application/json,.json"
        hidden
        onChange={async (e) => {
          const f = e.target.files?.[0]
          if (f) openFile(await f.text())
          e.target.value = ''
        }}
      />
    </header>
  )
}

function EmptyCanvas() {
  const empty = useEditor((s) => Object.keys(s.theme.screens).length === 0)
  if (!empty) return null
  return (
    <div className="empty">
      <h2>Пустой холст</h2>
      <p>Добавьте модули — их экраны появятся здесь, и вы соберёте из них бота. Или начните с экрана.</p>
      <div className="empty-actions">
        <button type="button" className="btn btn-primary" onClick={() => useEditor.getState().setSheet('market')}>Модули</button>
        <button type="button" className="btn" onClick={newScreen}>Новый экран</button>
      </div>
    </div>
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
  const sheet = useEditor((s) => s.sheet)
  useShortcuts()
  useEffect(() => void useEditor.getState().init(), [])
  useEffect(() => {
    const over = (e: DragEvent) => e.dataTransfer?.types.includes('Files') && e.preventDefault()
    const drop = async (e: DragEvent) => {
      const f = e.dataTransfer?.files[0]
      if (!f) return
      e.preventDefault()
      openFile(await f.text())
    }
    window.addEventListener('dragover', over)
    window.addEventListener('drop', drop)
    return () => {
      window.removeEventListener('dragover', over)
      window.removeEventListener('drop', drop)
    }
  }, [])

  if (engineError) return <div className="splash">Не удалось запустить проверку экранов: {engineError}</div>
  if (!ready) return <div className="splash">Загрузка…</div>
  return (
    <div className="app">
      <Toolbar />
      <main className="workspace">
        <ReactFlowProvider>
          <div className="canvas-wrap">
            <Canvas />
            <EmptyCanvas />
            <Issues />
          </div>
        </ReactFlowProvider>
        {simulator ? <Simulator /> : <Inspector />}
      </main>
      {sheet === 'market' ? <MarketSheet /> : sheet === 'export' ? <ExportSheet /> : null}
      <Toast />
    </div>
  )
}
