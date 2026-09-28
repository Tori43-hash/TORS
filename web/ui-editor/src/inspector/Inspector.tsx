import { useEditor } from '../store'
import { toEntry } from '../ops'
import { missing } from '../market'
import { ButtonPanel } from './ButtonPanel'
import { ConfigForm } from './ConfigForm'
import { EntryPanel } from './EntryPanel'
import { ModulePanel } from './ModulePanel'
import { Section } from './fields'
import { ScreenPanel } from './ScreenPanel'

/** Settings of the bot itself live in hidden modules: token, database, languages. */
const BASICS = ['telegram', 'database', 'users', 'http']

function BotPanel() {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const issues = useEditor((s) => s.issues)
  const modules = useEditor((s) => s.modules)
  const all = useEditor((s) => s.all)
  const resolved = useEditor((s) => s.resolved)
  const { edit, select, setSheet, addModule, editModules } = useEditor.getState()
  const errors = issues.filter((i) => i.level === 'error').length
  const commands = Object.entries(theme.commands ?? {})
  const added = [...resolved.set].map((id) => all.get(id)!).filter((m) => !m.hidden)

  return (
    <div className="ins">
      <div className="ins-head">
        <h2 className="ins-title">Бот</h2>
        <p className="ins-note">
          Экранов: {Object.keys(theme.screens).length} · {errors ? `ошибок: ${errors}` : 'ошибок нет'}
        </p>
      </div>

      <Section title="Модули" aside={<button type="button" className="link-btn" onClick={() => setSheet('market')}>Добавить</button>}>
        {added.length === 0 ? <p className="ins-note">Пока только основа. Добавьте модули из маркета.</p> : null}
        <div className="rows">
          {added.map((m) => {
            const miss = missing(m, modules[m.id]?.config)
            return (
              <button key={m.id} type="button" className="row row-btn" onClick={() => select({ kind: 'module', id: m.id })}>
                <span>{m.name}</span>
                <span className="spacer" />
                <span className="row-meta">{miss.length ? 'нужна настройка' : modules[m.id]?.added ? '' : 'по зависимости'}</span>
              </button>
            )
          })}
        </div>
        {resolved.needs.map((n) => (
          <div className="need" key={n.namespace}>
            <p className="ins-note strong">
              {all.get(n.by)?.name}: выберите {n.namespace === 'panels.providers' ? 'панель' : n.namespace === 'billing.gateways' ? 'способ оплаты' : 'модуль'}
            </p>
            <div className="need-options">
              {n.options.map((o) => (
                <button key={o.id} type="button" className="btn" onClick={() => { addModule(o.id); select({ kind: 'module', id: o.id }) }}>{o.name}</button>
              ))}
            </div>
          </div>
        ))}
      </Section>

      <Section
        title="Команды"
        aside={
          <button
            type="button"
            className="link-btn"
            onClick={() => {
              let n = 1
              while (theme.commands?.[`cmd${n}`]) n++
              edit((t) => void ((t.commands ??= {})[`cmd${n}`] = { default: '' }))
              select({ kind: 'entry', id: `cmd:cmd${n}` })
            }}
          >
            Добавить
          </button>
        }
      >
        <div className="rows">
          {commands.map(([name, e]) => (
            <button key={name} type="button" className="row row-btn" onClick={() => select({ kind: 'entry', id: `cmd:${name}` })}>
              <code>/{name}</code>
              <span className="spacer" />
              <span className="row-meta">{toEntry(e).default || 'не задан'}</span>
            </button>
          ))}
        </div>
      </Section>

      {Object.keys(manifest.events).length ? (
        <Section title="События">
          <div className="rows">
            {Object.entries(manifest.events).map(([name, ev]) => (
              <button key={name} type="button" className="row row-btn" onClick={() => select({ kind: 'entry', id: `ev:${name}` })}>
                <span>{ev.title}</span>
                <span className="spacer" />
                <span className="row-meta">{theme.events?.[name] ?? ev.default ?? '—'}</span>
              </button>
            ))}
          </div>
        </Section>
      ) : null}
      {BASICS.filter((id) => resolved.set.has(id) && all.get(id)?.config?.length).map((id) => {
        const m = all.get(id)!
        return (
          <Section key={id} title={m.name}>
            <ConfigForm
              fields={m.config!}
              values={modules[id]?.config}
              onChange={(key, v) => editModules((x) => void ((x[id] ??= { config: {} }).config[key] = v), `cfg:${id}:${key}`)}
            />
          </Section>
        )
      })}

    </div>
  )
}

export function Inspector() {
  const selection = useEditor((s) => s.selection)
  const key = selection ? JSON.stringify(selection) : 'bot'
  return (
    <aside className="inspector" key={key}>
      {selection?.kind === 'screen' ? <ScreenPanel id={selection.id} />
        : selection?.kind === 'button' ? <ButtonPanel screen={selection.screen} refId={selection.ref} />
        : selection?.kind === 'entry' ? <EntryPanel id={selection.id} />
        : selection?.kind === 'module' ? <ModulePanel id={selection.id} />
        : <BotPanel />}
    </aside>
  )
}
