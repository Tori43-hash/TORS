// Pure edits of a theme draft (used with immer). Button refs follow the Go
// renderer notation: "b.<block>.<item>", "k.<row>.<col>", "k.<row>.r"
// (repeat template), "k.<row>.f<fragmentRow>.<col>" (fragment), "k.<row>.p" (pager).

import type { Button, Entry, Repeat, Row, Screen, Theme } from './types'

export type Ref =
  | { area: 'body'; block: number; item: number }
  | { area: 'kb'; row: number; col: number }
  | { area: 'repeat'; row: number }
  | { area: 'frag'; row: number; fragRow: number; col: number }
  | { area: 'pager'; row: number }

export function parseRef(ref: string): Ref | null {
  const p = ref.split('.')
  const n = (s: string | undefined) => (s !== undefined && /^\d+$/.test(s) ? Number(s) : NaN)
  if (p[0] === 'b' && p.length === 3) return { area: 'body', block: n(p[1]), item: n(p[2]) }
  if (p[0] !== 'k') return null
  const row = n(p[1])
  if (p[2] === 'r') return { area: 'repeat', row }
  if (p[2] === 'p') return { area: 'pager', row }
  if (p[2]?.startsWith('f')) return { area: 'frag', row, fragRow: n(p[2].slice(1)), col: n(p[3]) }
  return { area: 'kb', row, col: n(p[2]) }
}

export const isRepeat = (r: Row): r is Repeat => !Array.isArray(r) && 'repeat' in r
export const isUse = (r: Row): r is { use: string } => !Array.isArray(r) && 'use' in r

/** The button a ref points to, and whether it lives in a shared fragment. */
export function locate(theme: Theme, screenId: string, ref: string): { button: Button; fragment?: string } | null {
  const s = theme.screens[screenId]
  const r = parseRef(ref)
  if (!s || !r) return null
  switch (r.area) {
    case 'body': {
      const b = s.blocks?.[r.block]?.buttons?.items[r.item]
      return b ? { button: b } : null
    }
    case 'kb': {
      const row = s.keyboard?.[r.row]
      const b = Array.isArray(row) ? row[r.col] : undefined
      return b ? { button: b } : null
    }
    case 'repeat': {
      const row = s.keyboard?.[r.row]
      return row && isRepeat(row) ? { button: row.button } : null
    }
    case 'frag': {
      const row = s.keyboard?.[r.row]
      if (!row || !isUse(row)) return null
      const b = theme.fragments?.[row.use]?.[r.fragRow]?.[r.col]
      return b ? { button: b, fragment: row.use } : null
    }
    default:
      return null
  }
}

export const KINDS = ['goto', 'action', 'url', 'web_app', 'copy_text', 'back', 'home'] as const
export type ButtonKind = (typeof KINDS)[number]

export function kindOf(b: Button): ButtonKind | '' {
  if (b.goto !== undefined) return 'goto'
  if (b.action !== undefined) return 'action'
  if (b.url !== undefined) return 'url'
  if (b.web_app !== undefined) return 'web_app'
  if (b.copy_text !== undefined) return 'copy_text'
  if (b.back) return 'back'
  if (b.home) return 'home'
  return ''
}

/** Switches what a button does, keeping its look. */
export function setKind(b: Button, kind: ButtonKind | '') {
  for (const k of ['goto', 'action', 'url', 'web_app', 'copy_text', 'back', 'home', 'on'] as const) delete b[k]
  switch (kind) {
    case 'goto': b.goto = ''; break
    case 'action': b.action = ''; break
    case 'url': b.url = 'https://'; break
    case 'web_app': b.web_app = 'https://'; break
    case 'copy_text': b.copy_text = ''; break
    case 'back': b.back = true; break
    case 'home': b.home = true; break
  }
}

export function setGoto(b: Button, screen: string) {
  setKind(b, 'goto')
  b.goto = screen
}

export function newButton(): Button {
  return { text: { ru: 'Кнопка', en: 'Button' } }
}

export function uniqueId(theme: Theme, base: string): string {
  const clean = base.replace(/[^a-z0-9_]/gi, '_').toLowerCase() || 'screen'
  if (!theme.screens[clean]) return clean
  for (let i = 2; ; i++) if (!theme.screens[`${clean}_${i}`]) return `${clean}_${i}`
}

export function addScreen(theme: Theme, base = 'screen'): string {
  const id = uniqueId(theme, base)
  theme.screens[id] = { blocks: [{ text: { ru: 'Новый экран', en: 'New screen' } }], keyboard: [] }
  return id
}

/** Renames a screen and every reference to it. */
export function renameScreen(theme: Theme, from: string, to: string) {
  if (from === to || theme.screens[to] || !theme.screens[from]) return
  const swap = (v: string | undefined) => (v === from ? to : v)
  const fixButton = (b: Button) => {
    if (b.goto === from) b.goto = to
    if (b.on) for (const k in b.on) b.on[k] = swap(b.on[k])!
  }
  const next: Record<string, Screen> = {}
  for (const [id, s] of Object.entries(theme.screens)) next[id === from ? to : id] = s
  theme.screens = next
  for (const s of Object.values(theme.screens)) {
    eachButton(s, fixButton)
    if (s.empty === from) s.empty = to
    if (s.input?.on) for (const k in s.input.on) s.input.on[k] = swap(s.input.on[k])!
  }
  for (const rows of Object.values(theme.fragments ?? {})) rows.flat().forEach(fixButton)
  for (const m of Object.values(theme.routes ?? {})) for (const k in m) m[k] = swap(m[k])!
  for (const k in theme.events ?? {}) theme.events![k] = swap(theme.events![k])!
  for (const [k, e] of Object.entries(theme.commands ?? {})) {
    const entry = toEntry(e)
    theme.commands![k] = { default: swap(entry.default)!, ...(entry.new_user ? { new_user: swap(entry.new_user) } : {}) }
  }
  const pos = theme.editor?.positions
  if (pos?.[from]) {
    pos[to] = pos[from]
    delete pos[from]
  }
}

export function eachButton(s: Screen, fn: (b: Button) => void) {
  for (const blk of s.blocks ?? []) blk.buttons?.items.forEach(fn)
  for (const row of s.keyboard ?? []) {
    if (Array.isArray(row)) row.forEach(fn)
    else if (isRepeat(row)) fn(row.button)
  }
}

export const toEntry = (e: Entry | string | undefined): Entry => (typeof e === 'string' ? { default: e } : (e ?? { default: '' }))

/** Removes a button; empties rows are dropped. Returns false for shared/generated buttons. */
export function removeButton(theme: Theme, screenId: string, ref: string): boolean {
  const s = theme.screens[screenId]
  const r = parseRef(ref)
  if (!s || !r) return false
  if (r.area === 'body') {
    const items = s.blocks?.[r.block]?.buttons?.items
    if (!items) return false
    items.splice(r.item, 1)
    if (items.length === 0) s.blocks!.splice(r.block, 1)
    return true
  }
  if (r.area === 'kb') {
    const row = s.keyboard?.[r.row]
    if (!Array.isArray(row)) return false
    row.splice(r.col, 1)
    if (row.length === 0) s.keyboard!.splice(r.row, 1)
    return true
  }
  if (r.area === 'repeat') {
    s.keyboard!.splice(r.row, 1)
    return true
  }
  return false
}

/** Moves a keyboard button to another place; row === rows.length or newRow creates a row. */
export function moveKeyboardButton(s: Screen, from: { row: number; col: number }, to: { row: number; col: number; newRow?: boolean }) {
  const kb = (s.keyboard ??= [])
  const src = kb[from.row]
  if (!Array.isArray(src)) return
  const [b] = src.splice(from.col, 1)
  if (!b) return
  let targetRow = to.row
  if (src.length === 0) {
    kb.splice(from.row, 1)
    if (to.row > from.row) targetRow--
  }
  if (to.newRow || !Array.isArray(kb[targetRow])) {
    kb.splice(Math.max(0, Math.min(targetRow, kb.length)), 0, [b])
    return
  }
  const dst = kb[targetRow] as Button[]
  dst.splice(Math.max(0, Math.min(to.col, dst.length)), 0, b)
}

export function screenHasPhoto(s: Screen | undefined): boolean {
  return !!s?.blocks?.some((b) => b.photo)
}

export function clone<T>(v: T): T {
  return structuredClone(v)
}
