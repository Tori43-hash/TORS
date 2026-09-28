import { useEditor } from '../store'
import { TRUST, missing } from '../market'
import { ConfigForm } from './ConfigForm'
import { Field, Section, TextInput } from './fields'

/** One module: its settings, its screens and what it offers to screens. */
export function ModulePanel({ id }: { id: string }) {
  const all = useEditor((s) => s.all)
  const state = useEditor((s) => s.modules[id])
  const resolved = useEditor((s) => s.resolved)
  const screens = useEditor((s) => s.theme.screens)
  const { editModules, select, removeModule, addScreens, addModule } = useEditor.getState()
  const m = all.get(id)
  if (!m) return <div className="ins"><p className="ins-note">Модуль {id} не найден в маркете.</p></div>

  const inBot = resolved.set.has(id)
  const neededBy = (resolved.requiredBy.get(id) ?? []).filter((x) => x !== id && resolved.set.has(x) && all.get(x))
  const starter = Object.keys(m.ui?.screens ?? {})
  const miss = missing(m, state?.config)
  const offers = [
    ...Object.values(m.ui?.data ?? {}).map((d) => d.title),
    ...Object.values(m.ui?.actions ?? {}).map((a) => a.title),
    ...Object.values(m.ui?.events ?? {}).map((e) => e.title),
  ]

  return (
    <div className="ins">
      <div className="ins-head">
        <button type="button" className="crumb" onClick={() => select(null)}>‹ Бот</button>
        <h2 className="ins-title">{m.name}</h2>
        <p className="ins-note">{m.summary}</p>
        <p className="ins-meta">{TRUST[m.trust ?? 'official']}{m.version ? ` · ${m.version}` : ''}</p>
      </div>

      {!inBot ? (
        <Section title="Модуль не в боте">
          <button type="button" className="btn btn-primary" onClick={() => addModule(id)}>Добавить</button>
        </Section>
      ) : null}

      {inBot && (m.config?.length || m.host?.named) ? (
        <Section title="Настройки">
          {miss.length ? <p className="ins-note strong">Заполните: {miss.join(', ')}</p> : null}
          {m.host?.named ? (
            <Field label="Имя" hint="Тарифы выбирают панель по этому имени">
              <TextInput
                value={state?.instance || 'main'}
                mono
                onCommit={(v) => editModules((x) => void ((x[id] ??= { config: {} }).instance = v.trim().replace(/[^a-z0-9_-]/gi, '') || 'main'))}
              />
            </Field>
          ) : null}
          <ConfigForm
            fields={m.config ?? []}
            values={state?.config}
            onChange={(key, v) => editModules((x) => void ((x[id] ??= { config: {} }).config[key] = v), `cfg:${id}:${key}`)}
          />
        </Section>
      ) : null}

      {starter.length ? (
        <Section title="Экраны модуля">
          <div className="rows">
            {starter.map((sid) => (
              <div className="row" key={sid}>
                <code>{sid}</code>
                <span className="spacer" />
                {screens[sid] ? (
                  <button type="button" className="link-btn" onClick={() => select({ kind: 'screen', id: sid })}>Открыть</button>
                ) : (
                  <button type="button" className="link-btn" disabled={!inBot} onClick={() => addScreens([sid], id)}>Вернуть на холст</button>
                )}
              </div>
            ))}
          </div>
        </Section>
      ) : null}

      {offers.length ? (
        <Section title="Даёт экранам">
          <p className="ins-note">{offers.join(' · ')}</p>
        </Section>
      ) : null}

      {m.requires?.some((r) => !all.get(r)?.hidden) ? (
        <Section title="Нужны модули">
          <p className="ins-note">{m.requires.map((r) => all.get(r)).filter((r) => r && !r.hidden).map((r) => r!.name).join(' · ')}</p>
        </Section>
      ) : null}

      {state?.added ? (
        <div className="ins-danger">
          {neededBy.some((x) => x !== 'telegram') ? (
            <p className="ins-note">Нужен модулям: {neededBy.map((x) => all.get(x)!.name).join(', ')}. Сначала удалите их.</p>
          ) : (
            <button type="button" className="btn btn-danger" onClick={() => removeModule(id)}>Удалить модуль</button>
          )}
        </div>
      ) : null}
    </div>
  )
}
