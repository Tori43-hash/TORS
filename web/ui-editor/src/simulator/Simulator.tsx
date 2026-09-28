import { useEffect, useMemo, useRef, useState } from 'react'
import { useEditor } from '../store'
import { engine, render, type Bridge } from '../wasm'
import { resolveOutcomes, type OutcomeView } from '../canvas/graph'
import { screenHasPhoto, toEntry } from '../ops'
import { TgMessage } from '../tg/Message'
import type { Rendered, RenderedButton } from '../types'
import { Segmented, Toggle } from '../inspector/fields'

interface Nav {
  screen: string
  params: Record<string, string>
}

type Msg =
  | { kind: 'bot'; id: number; nav: Nav; history: Nav[]; pages: Record<string, number>; time: string }
  | { kind: 'user'; id: number; text: string; time: string }
  | { kind: 'service'; id: number; text: string }
  | { kind: 'invoice'; id: number; title: string; paid: boolean; time: string }

interface Pending {
  msgId?: number
  title: string
  outcomes: OutcomeView[]
  params: Record<string, string>
}

let seq = 0
const now = () => new Date().toTimeString().slice(0, 5)

export function Simulator() {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const lang = useEditor((s) => s.lang)
  useEditor((s) => s.rendered) // re-render messages after the theme is re-analyzed
  const { setSimulator, notify } = useEditor.getState()

  const [bridge, setBridge] = useState<Bridge>()
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [newUser, setNewUser] = useState(true)
  const [conditions, setConditions] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(Object.entries(manifest.conditions).map(([k, c]) => [k, c.sample])),
  )
  const [pending, setPending] = useState<Pending>()
  const [text, setText] = useState('')
  const [menu, setMenu] = useState(false)
  const scroller = useRef<HTMLDivElement>(null)

  useEffect(() => void engine().then(setBridge), [])
  useEffect(() => {
    scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: 'smooth' })
  }, [msgs, pending])

  const renderNav = (nav: Nav, pages: Record<string, number>): Rendered | null => {
    if (!bridge) return null
    let current = nav
    for (let hops = 0; hops < 5; hops++) {
      if (!theme.screens[current.screen]) return null
      const r = render(bridge, current.screen, { Lang: lang, Params: current.params, Conditions: conditions, Pages: pages })
      if ('error' in r) return null
      if (!r.redirect) return r
      current = { screen: r.redirect, params: current.params }
    }
    return null
  }

  const bot = (nav: Nav): Msg => ({ kind: 'bot', id: ++seq, nav, history: [], pages: {}, time: now() })

  const send = (raw: string) => {
    const value = raw.trim()
    if (!value) return
    setText('')
    setMenu(false)
    const user: Msg = { kind: 'user', id: ++seq, text: value, time: now() }
    if (value.startsWith('/')) {
      const name = value.slice(1).split(/[\s@]/)[0]
      const entry = toEntry(theme.commands?.[name])
      const target = (name === 'start' && newUser && entry.new_user) || entry.default
      if (name === 'start') setNewUser(false)
      setMsgs((m) => [...m, user, ...(target ? [bot({ screen: target, params: {} })] : [{ kind: 'service' as const, id: ++seq, text: `Команда /${name} не настроена` }])])
      return
    }
    const last = [...msgs].reverse().find((m) => m.kind === 'bot')
    const input = last?.kind === 'bot' ? theme.screens[last.nav.screen]?.input : undefined
    setMsgs((m) => [...m, user])
    if (!input) {
      notify('Этот экран не ждёт текст')
      return
    }
    const a = manifest.actions[input.action]
    setPending({
      title: a?.title ?? input.action,
      outcomes: resolveOutcomes(theme, manifest, input.action, input.on),
      params: { ...(last as { nav: Nav }).nav.params, [input.param]: value },
    })
  }

  const update = (id: number, fn: (m: Extract<Msg, { kind: 'bot' }>) => Extract<Msg, { kind: 'bot' }>) =>
    setMsgs((all) => all.map((m) => (m.id === id && m.kind === 'bot' ? fn(m) : m)))

  /** Opens a screen from a button: the bot edits the same message, unless a
   * photo appears or disappears — Telegram cannot edit that, so it sends anew. */
  const navigate = (id: number, to: Nav, push = true) => {
    const msg = msgs.find((m) => m.id === id)
    if (msg?.kind !== 'bot') return
    const needsNewMessage = screenHasPhoto(theme.screens[msg.nav.screen]) !== screenHasPhoto(theme.screens[to.screen])
    if (needsNewMessage) {
      setMsgs((all) => [...all.filter((m) => m.id !== id), { ...msg, id: ++seq, nav: to, history: push ? [...msg.history, msg.nav] : msg.history, pages: {} }])
      return
    }
    update(id, (m) => ({ ...m, nav: to, history: push ? [...m.history, m.nav] : m.history, pages: {} }))
  }

  const press = (msg: Extract<Msg, { kind: 'bot' }>, b: RenderedButton) => {
    const params = b.params ?? {}
    switch (b.kind) {
      case 'goto':
        return navigate(msg.id, { screen: b.target!, params })
      case 'back': {
        const prev = msg.history.at(-1)
        if (prev) update(msg.id, (m) => ({ ...m, nav: prev, history: m.history.slice(0, -1), pages: {} }))
        return
      }
      case 'home': {
        const home = toEntry(theme.commands?.start).default
        if (home) update(msg.id, (m) => ({ ...m, nav: { screen: home, params: {} }, history: [], pages: {} }))
        return
      }
      case 'page':
        return update(msg.id, (m) => ({ ...m, pages: { ...m.pages, [b.ref]: Number(b.target) } }))
      case 'url':
      case 'web_app':
        return notify(`${b.kind === 'url' ? 'Откроется ссылка' : 'Откроется Mini App'}: ${b.target}`)
      case 'copy_text':
        navigator.clipboard?.writeText(b.target ?? '').catch(() => {})
        return notify('Скопировано в буфер обмена')
      case 'action': {
        const a = manifest.actions[b.target!]
        const on = Object.fromEntries(Object.entries(b.outcomes ?? {}).filter(([, v]) => v))
        return setPending({ msgId: msg.id, title: a?.title ?? b.target!, outcomes: resolveOutcomes(theme, manifest, b.target!, on), params })
      }
    }
  }

  const choose = (o: OutcomeView) => {
    const p = pending!
    setPending(undefined)
    if (!o.target) {
      setMsgs((m) => [...m, { kind: 'invoice', id: ++seq, title: p.title, paid: false, time: now() }])
      return
    }
    if (p.msgId) navigate(p.msgId, { screen: o.target, params: p.params })
    else setMsgs((m) => [...m, bot({ screen: o.target, params: p.params })])
  }

  const fireEvent = (name: string) => {
    const target = theme.events?.[name] ?? manifest.events[name]?.default
    if (target && theme.screens[target]) setMsgs((m) => [...m, bot({ screen: target, params: {} })])
    else notify('Для этого события экран не задан')
  }

  const pay = (id: number) => {
    setMsgs((m) => [...m.map((x) => (x.id === id && x.kind === 'invoice' ? { ...x, paid: true } : x)), { kind: 'service', id: ++seq, text: 'Вы успешно оплатили счёт' }])
    const paid = Object.keys(manifest.events).find((e) => e.endsWith('.paid'))
    if (paid) setTimeout(() => fireEvent(paid), 350)
  }

  const commands = useMemo(() => Object.keys(theme.commands ?? {}), [theme])

  return (
    <aside className="simulator">
      <div className="sim-controls">
        <div className="sim-controls-row">
          <strong>Проверка</strong>
          <span className="spacer" />
          <button type="button" className="btn" onClick={() => { setMsgs([]); setPending(undefined); setNewUser(true) }}>Сначала</button>
          <button type="button" className="btn" onClick={() => setSimulator(false)}>Закрыть</button>
        </div>
        <div className="sim-controls-row">
          <Segmented value={newUser ? 'new' : 'old'} onChange={(v) => setNewUser(v === 'new')} options={[{ value: 'new', label: 'Новый пользователь' }, { value: 'old', label: 'Вернувшийся' }]} />
        </div>
        <details className="sim-more">
          <summary>Условия и события</summary>
          {Object.entries(manifest.conditions).map(([k, c]) => (
            <Toggle key={k} label={c.title} checked={!!conditions[k]} onChange={(v) => setConditions((s) => ({ ...s, [k]: v }))} />
          ))}
          <div className="sim-events">
            {Object.entries(manifest.events).map(([k, ev]) => (
              <button key={k} type="button" className="btn" onClick={() => fireEvent(k)}>{ev.title}</button>
            ))}
          </div>
        </details>
      </div>

      <div className="phone tg">
        <div className="phone-head">
          <span className="phone-avatar">Б</span>
          <div>
            <div className="phone-name">Бот</div>
            <div className="phone-status">бот</div>
          </div>
        </div>
        <div className="phone-chat tg-wall" ref={scroller}>
          {msgs.length === 0 ? <div className="tg-service phone-hint">Отправьте /start, чтобы начать</div> : null}
          {msgs.map((m) => {
            if (m.kind === 'user') return <div key={m.id} className="tg-out">{m.text}<span className="tg-time">{m.time}</span></div>
            if (m.kind === 'service') return <div key={m.id} className="tg-service">{m.text}</div>
            if (m.kind === 'invoice') {
              return (
                <TgMessage
                  key={m.id}
                  time={m.time}
                  r={{ screen: 'invoice', blocks: [{ kind: 'text', text: `**${m.title}**\nСчёт на оплату звёздами Telegram` }], keyboard: m.paid ? [] : [[{ ref: 'pay', label: 'Оплатить', kind: 'goto' }]] }}
                  onPress={() => pay(m.id)}
                />
              )
            }
            const r = renderNav(m.nav, m.pages)
            return r ? (
              <TgMessage key={m.id} r={r} time={m.time} onPress={(b) => press(m, b)} />
            ) : (
              <div key={m.id} className="tg-service">Экран «{m.nav.screen}» недоступен</div>
            )
          })}
          {pending ? (
            <div className="sim-sheet">
              <div className="sim-sheet-title">{pending.title}: что ответит модуль?</div>
              {pending.outcomes.map((o) => (
                <button key={o.name} type="button" className="sim-sheet-btn" onClick={() => choose(o)}>
                  <span>{o.title}</span>
                  <span className="sim-sheet-target">{o.target ? `→ ${o.target}` : 'счёт'}</span>
                </button>
              ))}
              <button type="button" className="sim-sheet-cancel" onClick={() => setPending(undefined)}>Отмена</button>
            </div>
          ) : null}
        </div>
        {menu ? (
          <div className="phone-menu">
            {commands.map((c) => (
              <button key={c} type="button" onClick={() => send(`/${c}`)}>/{c}</button>
            ))}
          </div>
        ) : null}
        <form className="phone-input" onSubmit={(e) => { e.preventDefault(); send(text) }}>
          <button type="button" className="phone-menu-btn" onClick={() => setMenu((v) => !v)}>Меню</button>
          <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Сообщение" />
          <button type="submit" className="phone-send" aria-label="Отправить" disabled={!text.trim()}>
            <svg viewBox="0 0 24 24"><path d="M4 12 20 4l-4 16-4.5-6.5L4 12Z" /></svg>
          </button>
        </form>
      </div>
    </aside>
  )
}
