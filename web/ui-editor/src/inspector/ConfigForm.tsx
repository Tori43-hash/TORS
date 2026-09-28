import type { ConfigField } from '../types'
import { useEditor } from '../store'
import { instances, languagesOf, value } from '../market'
import { Field, Select, TextInput, Toggle } from './fields'

type Values = Record<string, unknown>

/** A settings form built from a module's descriptor. */
export function ConfigForm({ fields, values, onChange }: { fields: ConfigField[]; values: Values | undefined; onChange(key: string, v: unknown): void }) {
  return (
    <div className="form">
      {fields.map((f) => (
        <FieldInput key={f.key} f={f} v={value(f, values)} onChange={(v) => onChange(f.key, v)} />
      ))}
    </div>
  )
}

function FieldInput({ f, v, onChange }: { f: ConfigField; v: unknown; onChange(v: unknown): void }) {
  const modules = useEditor((s) => s.modules)
  const resolved = useEditor((s) => s.resolved)
  const all = useEditor((s) => s.all)
  const label = f.title + (f.required ? '' : ' · необязательно')
  const hint = f.hint ?? (f.type === 'secret' ? 'Лучше ссылка на переменную окружения: {env.ИМЯ}' : undefined)

  switch (f.type) {
    case 'bool':
      return <Toggle label={f.title} checked={!!v} onChange={onChange} />
    case 'number':
      return (
        <Field label={label} hint={hint}>
          <TextInput value={v === undefined || v === null ? '' : String(v)} placeholder={f.placeholder} onChange={(x) => onChange(x === '' ? '' : Number(x.replace(/[^\d.-]/g, '')) || 0)} />
        </Field>
      )
    case 'select': {
      const options = f.options_from === 'panels'
        ? instances(modules, resolved.set, all, 'panels').map((p) => ({ value: p, label: p }))
        : (f.options ?? []).map((o) => ({ value: o.value, label: o.title }))
      return (
        <Field label={label} hint={hint}>
          <Select value={String(v ?? '')} placeholder={f.options_from === 'panels' ? 'Основная' : f.required ? undefined : 'Не выбрано'} onChange={onChange} options={options} />
        </Field>
      )
    }
    case 'strings':
      return (
        <Field label={label} hint={hint ?? 'Через запятую'}>
          <TextInput
            value={Array.isArray(v) ? (v as string[]).join(', ') : ''}
            mono
            placeholder={f.placeholder}
            onCommit={(x) => onChange(x.split(',').map((s) => s.trim()).filter(Boolean))}
          />
        </Field>
      )
    case 'i18n':
      return <I18nInput f={f} label={label} v={v as Record<string, string> | undefined} onChange={onChange} langs={languagesOf(modules)} />
    case 'list':
      return <ListInput f={f} label={label} v={Array.isArray(v) ? (v as Values[]) : []} onChange={onChange} />
    default:
      return (
        <Field label={label} hint={hint}>
          <TextInput value={String(v ?? '')} mono={f.type === 'secret'} placeholder={f.placeholder} onChange={onChange} />
        </Field>
      )
  }
}

function I18nInput({ f, label, v, onChange, langs }: { f: ConfigField; label: string; v?: Record<string, string>; onChange(v: unknown): void; langs: string[] }) {
  return (
    <Field label={label} hint={f.hint}>
      <div className="i18n">
        {langs.map((l) => (
          <div className="i18n-row" key={l}>
            <span className="i18n-lang">{l.toUpperCase()}</span>
            <TextInput value={v?.[l] ?? ''} onChange={(x) => onChange({ ...v, [l]: x })} />
          </div>
        ))}
      </div>
    </Field>
  )
}

function ListInput({ f, label, v, onChange }: { f: ConfigField; label: string; v: Values[]; onChange(v: unknown): void }) {
  const set = (i: number, key: string, x: unknown) => onChange(v.map((item, j) => (j === i ? { ...item, [key]: x } : item)))
  return (
    <div className="list-field">
      <div className="field-label">{label}</div>
      {v.map((item, i) => (
        <div className="list-item" key={i}>
          <div className="list-item-head">
            <span>{String((item.title as Record<string, string> | undefined)?.ru ?? item.id ?? `№ ${i + 1}`)}</span>
            <span className="spacer" />
            <button type="button" className="link-btn" disabled={i === 0} onClick={() => onChange(v.map((x, j) => (j === i - 1 ? v[i] : j === i ? v[i - 1] : x)))}>Выше</button>
            <button type="button" className="link-btn" onClick={() => onChange(v.filter((_, j) => j !== i))}>Удалить</button>
          </div>
          <ConfigForm fields={f.item ?? []} values={item} onChange={(key, x) => set(i, key, x)} />
        </div>
      ))}
      <button type="button" className="btn" onClick={() => onChange([...v, Object.fromEntries((f.item ?? []).filter((x) => x.default !== undefined).map((x) => [x.key, x.default]))])}>
        Добавить
      </button>
    </div>
  )
}
