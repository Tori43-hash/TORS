import type { Button, Repeat } from '../types'
import { useEditor } from '../store'
import { addScreen, isRepeat, kindOf, locate, moveKeyboardButton, parseRef, removeButton, setKind, type ButtonKind } from '../ops'
import { resolveOutcomes } from '../canvas/graph'
import { Field, Icon, IconButton, Section, Segmented, Select, TextInput, Toggle } from './fields'
import { TextEditor } from './TextEditor'
import { screenVars } from './vars'

const NONE: string[] = []

const KIND_OPTIONS: { value: ButtonKind | ''; label: string }[] = [
  { value: 'goto', label: 'Открыть экран' },
  { value: 'action', label: 'Действие модуля' },
  { value: 'url', label: 'Открыть ссылку' },
  { value: 'web_app', label: 'Открыть Mini App' },
  { value: 'copy_text', label: 'Скопировать текст' },
  { value: 'back', label: 'Назад' },
  { value: 'home', label: 'В начало' },
  { value: '', label: 'Не выбрано' },
]

function Condition({ value, onChange, label, manifest }: { value?: string; onChange(v?: string): void; label: string; manifest: ReturnType<typeof useEditor.getState>['manifest'] }) {
  const neg = value?.startsWith('!') ?? false
  const name = value?.replace(/^!/, '') ?? ''
  return (
    <Field label={label}>
      <div className="pair">
        <Select
          value={name}
          placeholder="Всегда"
          onChange={(v) => onChange(v ? (neg ? `!${v}` : v) : undefined)}
          options={Object.entries(manifest.conditions).map(([k, c]) => ({ value: k, label: c.title }))}
        />
        {name ? (
          <Segmented value={neg ? 'no' : 'yes'} onChange={(v) => onChange(v === 'no' ? `!${name}` : name)} options={[{ value: 'yes', label: 'да' }, { value: 'no', label: 'нет' }]} />
        ) : null}
      </div>
    </Field>
  )
}

export function ButtonPanel({ screen, refId }: { screen: string; refId: string }) {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const incoming = useEditor((s) => s.incoming[screen] ?? NONE)
  const { edit, select, movePositions, positions } = useEditor.getState()
  const loc = locate(theme, screen, refId)
  const ref = parseRef(refId)
  if (!loc || !ref) return <div className="ins"><p className="ins-note">Кнопка не найдена.</p></div>

  const b = loc.button
  const s = theme.screens[screen]
  const row = ref.area === 'kb' || ref.area === 'repeat' || ref.area === 'frag' ? s.keyboard?.[ref.row] : undefined
  const repeat = row && isRepeat(row) ? row : undefined
  const inBody = ref.area === 'body'
  const kind = kindOf(b)
  const vars = screenVars(theme, manifest, screen, incoming, repeat?.repeat)
  const screens = Object.keys(theme.screens).sort()

  const change = (fn: (b: Button) => void, coalesce?: string) =>
    edit((t) => {
      const l = locate(t, screen, refId)
      if (l) fn(l.button)
    }, coalesce)
  const rowIndex = 'row' in ref ? ref.row : -1
  const changeRepeat = (fn: (r: Repeat) => void) => edit((t) => fn(t.screens[screen].keyboard![rowIndex] as Repeat))

  const requiredParams = [
    ...(kind === 'goto' && b.goto ? Object.values(theme.screens[b.goto]?.data ?? {}).flatMap((d) => manifest.data[d]?.params ?? []) : []),
    ...(kind === 'action' && b.action ? manifest.actions[b.action]?.params ?? [] : []),
  ].filter((p, i, a) => a.indexOf(p) === i && !(b.params && p in b.params))

  const styles = [
    { value: '', label: <span className="swatch swatch-default" />, title: 'Обычная' },
    { value: 'primary', label: <span className="swatch swatch-primary" />, title: 'Синяя' },
    { value: 'success', label: <span className="swatch swatch-success" />, title: 'Зелёная' },
    { value: 'danger', label: <span className="swatch swatch-danger" />, title: 'Красная' },
    ...(inBody ? [{ value: 'link', label: <span className="swatch swatch-link">Aa</span>, title: 'Ссылка' }] : []),
  ]

  const newScreenFor = () => {
    let id = ''
    edit((t) => {
      id = addScreen(t)
      const l = locate(t, screen, refId)
      if (l) {
        setKind(l.button, 'goto')
        l.button.goto = id
      }
    })
    const host = positions[`s:${screen}`] ?? { x: 0, y: 0 }
    movePositions({ [`s:${id}`]: { x: host.x + 640, y: host.y } }, false)
  }

  return (
    <div className="ins">
      <div className="ins-head">
        <button type="button" className="crumb" onClick={() => select({ kind: 'screen', id: screen })}>
          {Icon.back} {screen}
        </button>
        <div className="ins-kicker">{repeat ? 'Кнопка списка' : inBody ? 'Кнопка в сообщении' : 'Кнопка'}</div>
        {loc.fragment ? <p className="ins-note warn">Кнопка из фрагмента «{loc.fragment}» — изменения применятся на всех экранах с ним.</p> : null}
      </div>

      <Section title="Надпись">
        <TextEditor value={b.text} multiline={false} vars={vars} placeholder="Текст кнопки" onChange={(t) => change((x) => void (x.text = t), `btext:${screen}:${refId}`)} />
        <div className="pair">
          <Field label="Эмодзи">
            <TextInput value={b.emoji ?? ''} placeholder="🛒" onChange={(v) => change((x) => void (v ? (x.emoji = v) : delete x.emoji), `bemoji:${screen}:${refId}`)} />
          </Field>
          <Field label="Цвет">
            <Segmented value={(b.style ?? '') as string} onChange={(v) => change((x) => void (v ? (x.style = v as Button['style']) : delete x.style))} options={styles} />
          </Field>
        </div>
      </Section>

      <Section title="Что делает">
        <Select value={kind} onChange={(v) => change((x) => setKind(x, v as ButtonKind))} options={KIND_OPTIONS.filter((o) => o.value !== '' || kind === '')} />
        {kind === 'goto' ? (
          <div className="pair">
            <Select value={b.goto ?? ''} placeholder="Выберите экран" onChange={(v) => change((x) => void (x.goto = v))} options={screens.map((id) => ({ value: id, label: id }))} />
            <button type="button" className="ghost-btn" onClick={newScreenFor}>{Icon.plus} Новый</button>
          </div>
        ) : null}
        {kind === 'action' ? (
          <>
            <Select
              value={b.action ?? ''}
              placeholder="Выберите действие"
              onChange={(v) => change((x) => { x.action = v; delete x.on })}
              options={Object.entries(manifest.actions).filter(([, a]) => !a.input).map(([k, a]) => ({ value: k, label: a.title, group: k.split('.')[0] }))}
            />
            {b.action ? (
              <div className="outcomes">
                {resolveOutcomes(theme, manifest, b.action, b.on).map((o) => (
                  <div className="outcome" key={o.name}>
                    <span className="outcome-name">{o.title}</span>
                    {manifest.actions[b.action!]?.outcomes.find((x) => x.name === o.name)?.terminal && !o.target ? (
                      <span className="outcome-terminal">бот отправит счёт</span>
                    ) : (
                      <Select
                        value={b.on?.[o.name] ?? ''}
                        placeholder={`По умолчанию: ${o.target || '—'}`}
                        onChange={(v) => change((x) => {
                          if (v) x.on = { ...x.on, [o.name]: v }
                          else if (x.on) delete x.on[o.name]
                        })}
                        options={screens.map((id) => ({ value: id, label: id }))}
                      />
                    )}
                  </div>
                ))}
              </div>
            ) : null}
          </>
        ) : null}
        {kind === 'url' || kind === 'web_app' || kind === 'copy_text' ? (
          <Field hint={kind === 'copy_text' ? 'Можно вставить переменную, например {{ .Sub.URL }}' : kind === 'web_app' ? 'Только https://' : 'https://, http:// или tg://'}>
            <TextInput
              value={(kind === 'url' ? b.url : kind === 'web_app' ? b.web_app : b.copy_text) ?? ''}
              mono
              onChange={(v) => change((x) => void (x[kind] = v), `burl:${screen}:${refId}`)}
            />
          </Field>
        ) : null}
      </Section>

      {kind === 'goto' || kind === 'action' || b.params ? (
        <Section
          title="Параметры"
          aside={<IconButton title="Добавить параметр" onClick={() => change((x) => void (x.params = { ...x.params, [`p${Object.keys(x.params ?? {}).length + 1}`]: '' }))}>{Icon.plus}</IconButton>}
        >
          {requiredParams.map((p) => (
            <button key={p} type="button" className="hint-chip" onClick={() => change((x) => void (x.params = { ...x.params, [p]: repeat ? '{{ .ID }}' : `{{ .Params.${p} }}` }))}>
              {Icon.plus} Нужен параметр <b>{p}</b>
            </button>
          ))}
          {Object.entries(b.params ?? {}).map(([k, v]) => (
            <div className="pair" key={k}>
              <TextInput value={k} mono onCommit={(nk) => change((x) => { if (!nk || nk === k) return; x.params = Object.fromEntries(Object.entries(x.params!).map(([a, val]) => [a === k ? nk : a, val])) })} />
              <TextInput value={v} mono onChange={(nv) => change((x) => void (x.params![k] = nv), `bparam:${screen}:${refId}:${k}`)} />
              <IconButton title="Удалить" danger onClick={() => change((x) => { delete x.params![k]; if (!Object.keys(x.params!).length) delete x.params })}>{Icon.close}</IconButton>
            </div>
          ))}
        </Section>
      ) : null}

      {repeat ? (
        <Section title="Список">
          <div className="pair">
            <Field label="Источник">
              <Select
                value={repeat.repeat.replace(/^\./, '')}
                onChange={(v) => changeRepeat((r) => void (r.repeat = `.${v}`))}
                options={Object.entries(s.data ?? {}).filter(([, n]) => manifest.data[n]?.list).map(([a]) => ({ value: a, label: a }))}
              />
            </Field>
            <Field label="Колонок">
              <Segmented value={String(repeat.columns ?? 1)} onChange={(v) => changeRepeat((r) => void (r.columns = Number(v)))} options={['1', '2', '3', '4'].map((n) => ({ value: n, label: n }))} />
            </Field>
          </div>
          <Toggle
            label="Постранично"
            checked={!!repeat.page_size}
            onChange={(on) => changeRepeat((r) => void (on ? (r.page_size = 5) : delete r.page_size))}
          />
          {repeat.page_size ? (
            <Field label="Кнопок на странице">
              <TextInput value={String(repeat.page_size)} onCommit={(v) => changeRepeat((r) => void (r.page_size = Math.max(1, Number(v) || 1)))} />
            </Field>
          ) : null}
        </Section>
      ) : null}

      <Section title="Условия">
        <Condition label="Показывать, если" manifest={manifest} value={b.visible_if} onChange={(v) => change((x) => void (v ? (x.visible_if = v) : delete x.visible_if))} />
        <Condition label="Неактивна, если" manifest={manifest} value={b.disabled_if} onChange={(v) => change((x) => void (v ? (x.disabled_if = v) : delete x.disabled_if))} />
        <Field label="Кастомный эмодзи" hint="ID из Telegram. Работает при Premium у владельца бота или купленном username на Fragment.">
          <TextInput value={b.icon ?? ''} mono placeholder="5368324170671202286" onChange={(v) => change((x) => void (v ? (x.icon = v.replace(/\D/g, '')) : delete x.icon), `bicon:${screen}:${refId}`)} />
        </Field>
      </Section>

      {ref.area === 'kb' ? (
        <Section title="Расположение">
          <div className="move-pad">
            <IconButton title="В предыдущий ряд" onClick={() => { edit((t) => moveKeyboardButton(t.screens[screen], { row: ref.row, col: ref.col }, { row: ref.row - 1, col: 99 })); select({ kind: 'screen', id: screen }) }} disabled={ref.row === 0}>{Icon.up}</IconButton>
            <IconButton title="Левее" onClick={() => { edit((t) => moveKeyboardButton(t.screens[screen], { row: ref.row, col: ref.col }, { row: ref.row, col: ref.col - 1 })); select({ kind: 'button', screen, ref: `k.${ref.row}.${ref.col - 1}` }) }} disabled={ref.col === 0}>{Icon.left}</IconButton>
            <IconButton title="Правее" onClick={() => { edit((t) => moveKeyboardButton(t.screens[screen], { row: ref.row, col: ref.col }, { row: ref.row, col: ref.col + 1 })); select({ kind: 'button', screen, ref: `k.${ref.row}.${ref.col + 1}` }) }}>{Icon.right}</IconButton>
            <IconButton title="В следующий ряд" onClick={() => { edit((t) => moveKeyboardButton(t.screens[screen], { row: ref.row, col: ref.col }, { row: ref.row + 1, col: 99 })); select({ kind: 'screen', id: screen }) }}>{Icon.down}</IconButton>
            <button type="button" className="ghost-btn" onClick={() => { edit((t) => moveKeyboardButton(t.screens[screen], { row: ref.row, col: ref.col }, { row: ref.row + 1, col: 0, newRow: true })); select({ kind: 'screen', id: screen }) }}>
              {Icon.row} Отдельный ряд
            </button>
          </div>
        </Section>
      ) : null}

      {ref.area !== 'frag' ? (
        <div className="ins-danger">
          <button type="button" className="danger-btn" onClick={() => { edit((t) => void removeButton(t, screen, refId)); select({ kind: 'screen', id: screen }) }}>
            {Icon.trash} {repeat ? 'Удалить список' : 'Удалить кнопку'}
          </button>
        </div>
      ) : null}
    </div>
  )
}
