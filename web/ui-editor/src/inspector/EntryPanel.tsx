import { useEditor } from '../store'
import { toEntry } from '../ops'
import { Field, Section, Select, TextInput } from './fields'

/** Command or event: which screen it opens. */
export function EntryPanel({ id }: { id: string }) {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const { edit, select } = useEditor.getState()
  const screens = Object.keys(theme.screens).sort().map((s) => ({ value: s, label: s }))

  if (id.startsWith('ev:')) {
    const name = id.slice(3)
    const ev = manifest.events[name]
    const own = theme.events?.[name]
    return (
      <div className="ins">
        <div className="ins-head">
          <button type="button" className="crumb" onClick={() => select(null)}>‹ Бот</button>
        <div className="ins-kicker">Событие</div>
          <h2 className="ins-title">{ev?.title ?? name}</h2>
          <p className="ins-note">Бот показывает экран, когда это происходит, без нажатия кнопки.</p>
        </div>
        <Section title="Открывает экран">
          <Select
            value={own ?? ''}
            placeholder={`По умолчанию: ${ev?.default ?? '—'}`}
            onChange={(v) => edit((t) => {
              if (v) (t.events ??= {})[name] = v
              else if (t.events) delete t.events[name]
            })}
            options={screens}
          />
          <button type="button" className="btn" onClick={() => edit((t) => void ((t.events ??= {})[name] = ''))}>Не показывать экран</button>
        </Section>
      </div>
    )
  }

  const raw = id.slice(4)
  const isNew = raw === 'start:new'
  const name = isNew ? 'start' : raw
  const entry = toEntry(theme.commands?.[name])
  return (
    <div className="ins">
      <div className="ins-head">
        <button type="button" className="crumb" onClick={() => select(null)}>‹ Бот</button>
        <div className="ins-kicker">Команда</div>
        {name === 'start' ? (
          <h2 className="ins-title">/start</h2>
        ) : (
          <TextInput
            value={name}
            mono
            onCommit={(v) => {
              const clean = v.replace(/^\//, '').toLowerCase().replace(/[^a-z0-9_]/g, '')
              if (!clean || clean === name || theme.commands?.[clean]) return
              edit((t) => {
                t.commands = Object.fromEntries(Object.entries(t.commands!).map(([k, e]) => [k === name ? clean : k, e]))
              })
              select({ kind: 'entry', id: `cmd:${clean}` })
            }}
          />
        )}
      </div>
      <Section title="Открывает экран">
        {name === 'start' ? (
          <>
            <Field label="Новому пользователю" hint="Если не задан — тот же экран, что и остальным">
              <Select
                value={entry.new_user ?? ''}
                placeholder="Как всем"
                onChange={(v) => edit((t) => {
                  const e = toEntry(t.commands?.start)
                  ;(t.commands ??= {}).start = v ? { ...e, new_user: v } : { default: e.default }
                })}
                options={screens}
              />
            </Field>
            <Field label="Вернувшемуся пользователю">
              <Select value={entry.default} placeholder="Выберите экран" onChange={(v) => edit((t) => void ((t.commands ??= {}).start = { ...toEntry(t.commands?.start), default: v }))} options={screens} />
            </Field>
          </>
        ) : (
          <Select value={entry.default} placeholder="Выберите экран" onChange={(v) => edit((t) => void ((t.commands ??= {})[name] = { ...toEntry(t.commands?.[name]), default: v }))} options={screens} />
        )}
      </Section>
      {name !== 'start' ? (
        <div className="ins-danger">
          <button type="button" className="btn btn-danger" onClick={() => { edit((t) => void delete t.commands![name]); select(null) }}>
            Удалить команду
          </button>
        </div>
      ) : null}
    </div>
  )
}
