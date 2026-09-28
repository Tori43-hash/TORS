import { useState } from 'react'
import type { Block, Row, Screen } from '../types'
import { useEditor } from '../store'
import { isRepeat, isUse, moveKeyboardButton, newButton } from '../ops'
import { Field, Icon, IconButton, Section, Select, TextInput, Toggle } from './fields'
import { TextEditor } from './TextEditor'
import { screenVars } from './vars'

const NONE: string[] = []

const DRAG = 'application/x-tors-button'

function label(b: { text?: Record<string, string>; emoji?: string }, lang: string) {
  const t = b.text?.[lang] ?? Object.values(b.text ?? {})[0] ?? ''
  return [b.emoji, t].filter(Boolean).join(' ') || 'Без текста'
}

export function ScreenPanel({ id }: { id: string }) {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const incoming = useEditor((s) => s.incoming[id] ?? NONE)
  const lang = useEditor((s) => s.lang)
  const { edit, rename, select } = useEditor.getState()
  const screen = theme.screens[id]
  const [dragOver, setDragOver] = useState<string>('')
  if (!screen) return null

  const vars = screenVars(theme, manifest, id, incoming)
  const change = (fn: (s: Screen) => void, coalesce?: string) => edit((t) => fn(t.screens[id]), coalesce)
  const screens = Object.keys(theme.screens).filter((s) => s !== id).sort()
  const listAliases = Object.entries(screen.data ?? {}).filter(([, n]) => manifest.data[n]?.list).map(([a]) => a)

  const moveBlock = (i: number, d: number) =>
    change((s) => {
      const b = s.blocks!
      const j = i + d
      if (j < 0 || j >= b.length) return
      ;[b[i], b[j]] = [b[j], b[i]]
    })

  const addBlock = (b: Block) => change((s) => void (s.blocks ??= []).push(b))
  const addRow = (row: Row) => change((s) => void (s.keyboard ??= []).push(row))

  const dropTo = (e: React.DragEvent, to: { row: number; col: number; newRow?: boolean }) => {
    e.preventDefault()
    setDragOver('')
    const raw = e.dataTransfer.getData(DRAG)
    if (!raw) return
    const from = JSON.parse(raw) as { row: number; col: number }
    change((s) => moveKeyboardButton(s, from, to))
  }
  const over = (key: string) => ({
    onDragOver: (e: React.DragEvent) => {
      if (!e.dataTransfer.types.includes(DRAG)) return
      e.preventDefault()
      setDragOver(key)
    },
    onDragLeave: () => setDragOver((k) => (k === key ? '' : k)),
  })

  return (
    <div className="ins">
      <div className="ins-head">
        <div className="ins-kicker">Экран</div>
        <TextInput
          value={id}
          mono
          onCommit={(v) => {
            const clean = v.trim().toLowerCase().replace(/[^a-z0-9_]/g, '_')
            if (clean && clean !== id) rename(id, clean)
          }}
        />
      </div>

      <Section title="Сообщение">
        {(screen.blocks ?? []).map((b, i) => (
          <div className="block-card" key={i}>
            <div className="block-card-head">
              <span>{b.photo !== undefined ? 'Фото' : b.buttons ? 'Кнопки в сообщении' : 'Текст'}</span>
              <span className="spacer" />
              <IconButton title="Выше" onClick={() => moveBlock(i, -1)} disabled={i === 0}>{Icon.up}</IconButton>
              <IconButton title="Ниже" onClick={() => moveBlock(i, 1)} disabled={i === (screen.blocks?.length ?? 0) - 1}>{Icon.down}</IconButton>
              <IconButton title="Удалить блок" danger onClick={() => change((s) => void s.blocks!.splice(i, 1))}>{Icon.close}</IconButton>
            </div>
            {b.photo !== undefined ? (
              <Field hint="Ссылка https://…, Telegram file_id или путь к файлу в assets/">
                <TextInput value={b.photo} mono placeholder="assets/banner.jpg" onChange={(v) => change((s) => void (s.blocks![i].photo = v), `photo:${id}:${i}`)} />
              </Field>
            ) : b.buttons ? (
              <>
                <div className="chip-row">
                  {b.buttons.items.map((btn, j) => (
                    <button key={j} type="button" className="chip" onClick={() => select({ kind: 'button', screen: id, ref: `b.${i}.${j}` })}>
                      {label(btn, lang)}
                    </button>
                  ))}
                  <IconButton title="Добавить кнопку" onClick={() => change((s) => void s.blocks![i].buttons!.items.push(newButton()))}>{Icon.plus}</IconButton>
                </div>
                <Field label="Выравнивание">
                  <Select
                    value={b.buttons.align ?? ''}
                    onChange={(v) => change((s) => void (s.blocks![i].buttons!.align = v as 'left'))}
                    options={[{ value: '', label: 'По левому краю' }, { value: 'center', label: 'По центру' }, { value: 'right', label: 'По правому краю' }]}
                  />
                </Field>
              </>
            ) : (
              <TextEditor value={b.text} vars={vars} onChange={(t) => change((s) => void (s.blocks![i].text = t), `text:${id}:${i}`)} />
            )}
          </div>
        ))}
        <div className="add-row">
          <button type="button" className="ghost-btn" onClick={() => addBlock({ text: { ru: '', en: '' } })}>{Icon.plus} Текст</button>
          <button type="button" className="ghost-btn" onClick={() => addBlock({ photo: '' })}>{Icon.plus} Фото</button>
          <button type="button" className="ghost-btn" onClick={() => addBlock({ buttons: { items: [newButton()] } })}>{Icon.plus} Кнопки</button>
        </div>
      </Section>

      <Section title="Кнопки под сообщением">
        <div className="kb-grid">
          {(screen.keyboard ?? []).map((row, i) => (
            <div key={i}>
              <div className={`kb-gap${dragOver === `gap:${i}` ? ' is-over' : ''}`} {...over(`gap:${i}`)} onDrop={(e) => dropTo(e, { row: i, col: 0, newRow: true })} />
              <div className="kb-grid-row">
                {Array.isArray(row) ? (
                  <>
                    {row.map((btn, j) => (
                      <button
                        key={j}
                        type="button"
                        draggable
                        className={`chip chip-kb${btn.style ? ` chip-${btn.style}` : ''}${dragOver === `c:${i}:${j}` ? ' is-over' : ''}`}
                        onDragStart={(e) => e.dataTransfer.setData(DRAG, JSON.stringify({ row: i, col: j }))}
                        {...over(`c:${i}:${j}`)}
                        onDrop={(e) => dropTo(e, { row: i, col: j })}
                        onClick={() => select({ kind: 'button', screen: id, ref: `k.${i}.${j}` })}
                      >
                        {label(btn, lang)}
                      </button>
                    ))}
                    {row.length < 8 ? (
                      <span className={`kb-add${dragOver === `end:${i}` ? ' is-over' : ''}`} {...over(`end:${i}`)} onDrop={(e) => dropTo(e, { row: i, col: row.length })}>
                        <IconButton title="Кнопка в этот ряд" onClick={() => change((s) => void (s.keyboard![i] as ReturnType<typeof newButton>[]).push(newButton()))}>{Icon.plus}</IconButton>
                      </span>
                    ) : null}
                  </>
                ) : isRepeat(row) ? (
                  <button type="button" className="chip chip-list" onClick={() => select({ kind: 'button', screen: id, ref: `k.${i}.r` })}>
                    Список {row.repeat} · {label(row.button, lang)}
                  </button>
                ) : isUse(row) ? (
                  <div className="chip chip-frag">
                    Фрагмент
                    <Select value={row.use} onChange={(v) => change((s) => void ((s.keyboard![i] as { use: string }).use = v))} options={Object.keys(theme.fragments ?? {}).map((f) => ({ value: f, label: f }))} />
                  </div>
                ) : null}
                <span className="spacer" />
                <IconButton title="Удалить ряд" danger onClick={() => change((s) => void s.keyboard!.splice(i, 1))}>{Icon.close}</IconButton>
              </div>
            </div>
          ))}
          <div
            className={`kb-gap is-last${dragOver === 'gap:end' ? ' is-over' : ''}`}
            {...over('gap:end')}
            onDrop={(e) => dropTo(e, { row: screen.keyboard?.length ?? 0, col: 0, newRow: true })}
          />
        </div>
        <div className="add-row">
          <button type="button" className="ghost-btn" onClick={() => addRow([newButton()])}>{Icon.plus} Ряд</button>
          <button
            type="button"
            className="ghost-btn"
            disabled={listAliases.length === 0}
            title={listAliases.length ? 'Кнопка на каждый элемент списка' : 'Сначала подключите список в «Данных»'}
            onClick={() => addRow({ repeat: `.${listAliases[0]}`, columns: 1, button: { text: { ru: '{{ .ID }}' } } })}
          >
            {Icon.plus} Список
          </button>
          <button
            type="button"
            className="ghost-btn"
            disabled={!Object.keys(theme.fragments ?? {}).length}
            onClick={() => addRow({ use: Object.keys(theme.fragments ?? {})[0] })}
          >
            {Icon.plus} Фрагмент
          </button>
        </div>
        <p className="ins-note">Перетаскивайте кнопки между рядами. Кнопки в одном ряду делят ширину поровну — так в Telegram.</p>
      </Section>

      <Section
        title="Данные"
        aside={
          <IconButton
            title="Подключить данные"
            onClick={() => change((s) => {
              s.data ??= {}
              let n = 1
              while (s.data[`Data${n}`]) n++
              s.data[`Data${n}`] = Object.keys(manifest.data)[0] ?? ''
            })}
          >
            {Icon.plus}
          </IconButton>
        }
      >
        {Object.entries(screen.data ?? {}).length === 0 ? <p className="ins-note">Подключите данные модулей, чтобы выводить их в тексте и кнопках.</p> : null}
        {Object.entries(screen.data ?? {}).map(([alias, name]) => (
          <div className="pair" key={alias}>
            <TextInput
              value={alias}
              mono
              onCommit={(v) =>
                change((s) => {
                  const clean = v.replace(/[^A-Za-z0-9_]/g, '')
                  if (!clean || s.data![clean]) return
                  s.data = Object.fromEntries(Object.entries(s.data!).map(([k, val]) => [k === alias ? clean : k, val]))
                })
              }
            />
            <Select
              value={name}
              onChange={(v) => change((s) => void (s.data![alias] = v))}
              options={Object.entries(manifest.data).map(([k, d]) => ({ value: k, label: `${d.title}${d.list ? ' (список)' : ''}` }))}
            />
            <IconButton title="Отключить" danger onClick={() => change((s) => void delete s.data![alias])}>{Icon.close}</IconButton>
          </div>
        ))}
      </Section>

      {listAliases.length ? (
        <Section title="Если список пуст">
          <Select value={screen.empty ?? ''} onChange={(v) => change((s) => void (v ? (s.empty = v) : delete s.empty))} placeholder="Ничего не показывать" options={screens.map((s) => ({ value: s, label: s }))} />
        </Section>
      ) : null}

      <Section title="Ввод текста">
        <Toggle
          label="Экран ждёт сообщение от пользователя"
          checked={!!screen.input}
          onChange={(on) =>
            change((s) => {
              if (!on) delete s.input
              else {
                const action = Object.entries(manifest.actions).find(([, a]) => a.input)?.[0] ?? ''
                s.input = { action, param: manifest.actions[action]?.params?.at(-1) ?? 'text' }
              }
            })
          }
        />
        {screen.input ? (
          <>
            <Field label="Передать в действие">
              <Select
                value={screen.input.action}
                onChange={(v) => change((s) => void (s.input!.action = v))}
                options={Object.entries(manifest.actions).filter(([, a]) => a.input).map(([k, a]) => ({ value: k, label: a.title }))}
              />
            </Field>
            <Field label="Параметр" hint="Куда положить введённый текст">
              <TextInput value={screen.input.param} mono onCommit={(v) => change((s) => void (s.input!.param = v.trim()))} />
            </Field>
          </>
        ) : null}
      </Section>

      <div className="ins-danger">
        <button type="button" className="danger-btn" onClick={() => { edit((t) => void delete t.screens[id]); select(null); useEditor.getState().notify('Экран удалён', true) }}>
          {Icon.trash} Удалить экран
        </button>
      </div>
    </div>
  )
}
