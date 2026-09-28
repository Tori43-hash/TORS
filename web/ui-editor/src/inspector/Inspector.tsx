import { useEditor } from '../store'
import { toEntry } from '../ops'
import { ButtonPanel } from './ButtonPanel'
import { EntryPanel } from './EntryPanel'
import { Icon, IconButton, Section } from './fields'
import { ScreenPanel } from './ScreenPanel'

function ThemePanel() {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const issues = useEditor((s) => s.issues)
  const { edit, select } = useEditor.getState()
  const errors = issues.filter((i) => i.level === 'error').length
  const commands = Object.entries(theme.commands ?? {})
  return (
    <div className="ins">
      <div className="ins-head">
        <div className="ins-kicker">Тема</div>
        <h2 className="ins-title">Бот целиком</h2>
        <p className="ins-note">
          {Object.keys(theme.screens).length} экранов · {errors ? `${errors} ошибок` : 'ошибок нет'}
        </p>
      </div>
      <Section
        title="Команды"
        aside={
          <IconButton
            title="Добавить команду"
            onClick={() => {
              let n = 1
              while (theme.commands?.[`cmd${n}`]) n++
              edit((t) => void ((t.commands ??= {})[`cmd${n}`] = { default: '' }))
              select({ kind: 'entry', id: `cmd:cmd${n}` })
            }}
          >
            {Icon.plus}
          </IconButton>
        }
      >
        {commands.map(([name, e]) => (
          <button key={name} type="button" className="list-row" onClick={() => select({ kind: 'entry', id: `cmd:${name}` })}>
            <code>/{name}</code>
            <span>{toEntry(e).default || '—'}</span>
          </button>
        ))}
      </Section>
      <Section title="События">
        {Object.entries(manifest.events).map(([name, ev]) => (
          <button key={name} type="button" className="list-row" onClick={() => select({ kind: 'entry', id: `ev:${name}` })}>
            <span>{ev.title}</span>
            <span>{theme.events?.[name] ?? ev.default ?? '—'}</span>
          </button>
        ))}
      </Section>
      <Section title="Как работать">
        <ul className="tips">
          <li>Нажмите на кнопку в экране, чтобы изменить её текст, цвет и действие.</li>
          <li>Потяните за точку справа от кнопки к другому экрану — появится переход.</li>
          <li>Отпустите стрелку на пустом месте — создастся новый экран.</li>
          <li>Выделите стрелку и нажмите Backspace, чтобы убрать переход.</li>
          <li>⌘Z / Ctrl+Z — отменить, ⇧⌘Z / Ctrl+Y — повторить.</li>
        </ul>
      </Section>
    </div>
  )
}

export function Inspector() {
  const selection = useEditor((s) => s.selection)
  const key = selection ? JSON.stringify(selection) : 'theme'
  return (
    <aside className="inspector" key={key}>
      {selection?.kind === 'screen' ? <ScreenPanel id={selection.id} />
        : selection?.kind === 'button' ? <ButtonPanel screen={selection.screen} refId={selection.ref} />
        : selection?.kind === 'entry' ? <EntryPanel id={selection.id} />
        : <ThemePanel />}
    </aside>
  )
}
