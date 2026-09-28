import type { Field, Manifest, Theme } from '../types'
import type { VarGroup } from './fields'

const fmt = (f: Field): string | undefined => {
  if (f.sample === undefined || f.sample === null) return undefined
  const s = String(f.sample)
  return s.length > 24 ? `${s.slice(0, 22)}…` : s
}

const pipe = (f: Field) => (f.type === 'time' ? ' | date' : f.type === 'bytes' ? ' | bytes' : '')

/** Variables a text on this screen can use; inRepeat adds the item fields of a list. */
export function screenVars(theme: Theme, manifest: Manifest, screenId: string, incoming: string[], repeatSource?: string): VarGroup[] {
  const s = theme.screens[screenId]
  const groups: VarGroup[] = []
  if (repeatSource) {
    const src = manifest.data[s?.data?.[repeatSource.replace(/^\./, '')] ?? '']
    if (src) {
      groups.push({
        title: 'Элемент списка',
        items: src.fields.map((f) => ({ expr: `{{ .${f.name}${pipe(f)} }}`, title: f.title ?? f.name, sample: `.${f.name}` })),
      })
    }
  }
  groups.push({
    title: 'Пользователь',
    items: manifest.user.map((f) => ({ expr: `{{ .User.${f.name} }}`, title: f.title ?? f.name, sample: fmt(f) })),
  })
  for (const [alias, name] of Object.entries(s?.data ?? {})) {
    const src = manifest.data[name]
    if (!src) continue
    if (src.list) {
      groups.push({ title: `${alias} · ${src.title}`, items: [{ expr: `{{ len .${alias} }}`, title: 'Количество', sample: `len .${alias}` }] })
    } else {
      groups.push({
        title: `${alias} · ${src.title}`,
        items: src.fields.map((f) => ({ expr: `{{ .${alias}.${f.name}${pipe(f)} }}`, title: f.title ?? f.name, sample: fmt(f) ?? `.${alias}.${f.name}` })),
      })
    }
  }
  if (incoming.length) {
    groups.push({ title: 'Параметры экрана', items: incoming.map((p) => ({ expr: `{{ .Params.${p} }}`, title: p })) })
  }
  const eventFields = Object.entries(manifest.events)
    .filter(([name, ev]) => (theme.events?.[name] ?? ev.default) === screenId)
    .flatMap(([, ev]) => ev.fields ?? [])
  if (eventFields.length) {
    groups.push({
      title: 'Событие',
      items: eventFields.map((f) => ({ expr: `{{ .Event.${f.name}${pipe(f)} }}`, title: f.title ?? f.name, sample: fmt(f) })),
    })
  }
  groups.push({
    title: 'Функции',
    items: [
      { expr: '{{ plural .N "день" "дня" "дней" }}', title: 'Склонение', sample: 'plural' },
      { expr: '{{ if .X }}…{{ else }}…{{ end }}', title: 'Условие', sample: 'if / else' },
    ],
  })
  return groups
}

/** Inserts text at the caret of a textarea or input and returns the new value. */
export function insertAt(el: HTMLTextAreaElement | HTMLInputElement | null, value: string, text: string): string {
  if (!el) return value + text
  const start = el.selectionStart ?? value.length
  const end = el.selectionEnd ?? value.length
  const next = value.slice(0, start) + text + value.slice(end)
  requestAnimationFrame(() => {
    el.focus()
    el.setSelectionRange(start + text.length, start + text.length)
  })
  return next
}

/** Wraps the selection with markup (bold, italic, …). */
export function wrapSelection(el: HTMLTextAreaElement | null, value: string, left: string, right = left): string {
  if (!el) return value
  const start = el.selectionStart
  const end = el.selectionEnd
  const inner = value.slice(start, end) || 'текст'
  const next = value.slice(0, start) + left + inner + right + value.slice(end)
  requestAnimationFrame(() => {
    el.focus()
    el.setSelectionRange(start + left.length, start + left.length + inner.length)
  })
  return next
}
