import type { Issue, Manifest, Rendered, RenderedButton, Theme } from '../types'
import { locate, toEntry } from '../ops'
import type { Positions } from '../store'

// The canvas graph: entry points, screens and action nodes, and the edges
// between them. Node ids: "s:<screen>", "a:<screen>:<ref|input>",
// "e:cmd:<name>[:new]", "e:ev:<event>".

export interface OutcomeView {
  name: string
  title: string
  target: string
  terminal: boolean
  overridden: boolean
}

export type GNode =
  | { id: string; type: 'screen'; data: ScreenData }
  | { id: string; type: 'action'; data: ActionData }
  | { id: string; type: 'entry'; data: EntryData }

export interface ScreenData {
  [key: string]: unknown
  id: string
  r?: Rendered
  errors: number
  warnings: number
  input: boolean
  list: boolean
  empty?: string
}

export interface ActionData {
  [key: string]: unknown
  screen: string
  ref: string
  action: string
  title: string
  outcomes: OutcomeView[]
}

export interface EntryData {
  [key: string]: unknown
  kind: 'command' | 'event'
  name: string
  label: string
  sub: string
}

export interface GEdge {
  id: string
  source: string
  sourceHandle: string
  target: string
  kind: 'goto' | 'action' | 'outcome' | 'entry' | 'empty'
  dashed?: boolean
}

export function resolveOutcomes(theme: Theme, manifest: Manifest, action: string, on?: Record<string, string>): OutcomeView[] {
  const a = manifest.actions[action]
  if (!a) return []
  return a.outcomes.map((o) => {
    const own = on?.[o.name]
    const route = theme.routes?.[action]?.[o.name]
    const target = own || route || (o.terminal ? '' : o.default ?? '')
    return { name: o.name, title: o.title || o.name, target, terminal: !!o.terminal && !own && !route, overridden: !!own }
  })
}

export function buildGraph(theme: Theme, manifest: Manifest, rendered: Record<string, Rendered>, issues: Issue[]) {
  const nodes: GNode[] = []
  const edges: GEdge[] = []
  const exists = (s: string) => !!s && !!theme.screens[s]

  for (const [name, raw] of Object.entries(theme.commands ?? {})) {
    const e = toEntry(raw)
    if (name === 'start' && e.new_user) {
      nodes.push({ id: 'e:cmd:start:new', type: 'entry', data: { kind: 'command', name: 'start:new', label: '/start', sub: 'новый пользователь' } })
      if (exists(e.new_user)) edges.push({ id: 'en:start:new', source: 'e:cmd:start:new', sourceHandle: 'out', target: `s:${e.new_user}`, kind: 'entry' })
    }
    const sub = name === 'start' ? (e.new_user ? 'вернувшийся' : 'команда') : 'команда'
    nodes.push({ id: `e:cmd:${name}`, type: 'entry', data: { kind: 'command', name, label: `/${name}`, sub } })
    if (exists(e.default)) edges.push({ id: `en:${name}`, source: `e:cmd:${name}`, sourceHandle: 'out', target: `s:${e.default}`, kind: 'entry' })
  }
  for (const [name, ev] of Object.entries(manifest.events)) {
    const own = theme.events?.[name]
    const target = own ?? ev.default ?? ''
    nodes.push({ id: `e:ev:${name}`, type: 'entry', data: { kind: 'event', name, label: ev.title, sub: 'событие' } })
    if (exists(target)) edges.push({ id: `ev:${name}`, source: `e:ev:${name}`, sourceHandle: 'out', target: `s:${target}`, kind: 'entry', dashed: own === undefined })
  }

  for (const [id, s] of Object.entries(theme.screens)) {
    const r = rendered[id]
    const mine = issues.filter((i) => i.screen === id)
    const list = !!s.keyboard?.some((row) => !Array.isArray(row) && 'repeat' in row)
    nodes.push({
      id: `s:${id}`,
      type: 'screen',
      data: {
        id,
        r,
        errors: mine.filter((i) => i.level === 'error').length,
        warnings: mine.filter((i) => i.level === 'warning').length,
        input: !!s.input,
        list,
        empty: s.empty,
      },
    })
    if (s.empty && exists(s.empty)) edges.push({ id: `em:${id}`, source: `s:${id}`, sourceHandle: 'empty', target: `s:${s.empty}`, kind: 'empty', dashed: true })

    const buttons: RenderedButton[] = r ? [...r.blocks.flatMap((b) => b.buttons ?? []), ...r.keyboard.flat()] : []
    const seen = new Set<string>()
    for (const b of buttons) {
      if (seen.has(b.ref)) continue
      seen.add(b.ref)
      if (b.kind === 'goto' && exists(b.target ?? '')) {
        edges.push({ id: `g:${id}:${b.ref}`, source: `s:${id}`, sourceHandle: b.ref, target: `s:${b.target}`, kind: 'goto' })
      }
      if (b.kind === 'action' && b.target) {
        const a = manifest.actions[b.target]
        const on = locate(theme, id, b.ref)?.button.on
        const node = `a:${id}:${b.ref}`
        const outcomes = resolveOutcomes(theme, manifest, b.target, on)
        nodes.push({ id: node, type: 'action', data: { screen: id, ref: b.ref, action: b.target, title: a?.title ?? b.target, outcomes } })
        edges.push({ id: `ga:${id}:${b.ref}`, source: `s:${id}`, sourceHandle: b.ref, target: node, kind: 'action' })
        for (const o of outcomes) {
          if (exists(o.target)) edges.push({ id: `o:${id}:${b.ref}:${o.name}`, source: node, sourceHandle: o.name, target: `s:${o.target}`, kind: 'outcome', dashed: !o.overridden })
        }
      }
    }
    if (s.input) {
      const node = `a:${id}:input`
      const outcomes = resolveOutcomes(theme, manifest, s.input.action, s.input.on)
      nodes.push({ id: node, type: 'action', data: { screen: id, ref: 'input', action: s.input.action, title: manifest.actions[s.input.action]?.title ?? s.input.action, outcomes } })
      edges.push({ id: `gi:${id}`, source: `s:${id}`, sourceHandle: 'input', target: node, kind: 'action' })
      for (const o of outcomes) {
        if (exists(o.target)) edges.push({ id: `o:${id}:input:${o.name}`, source: node, sourceHandle: o.name, target: `s:${o.target}`, kind: 'outcome', dashed: !o.overridden })
      }
    }
  }
  return { nodes, edges }
}

// --- layout -----------------------------------------------------------------

const SCREEN_W = 344
const COL = 760

export function estimateHeight(n: GNode): number {
  if (n.type === 'entry') return 52
  if (n.type === 'action') return 44 + n.data.outcomes.length * 28
  const r = n.data.r
  let h = 36 + 24 + 20
  if (!r) return h + 120
  for (const b of r.blocks) {
    if (b.kind === 'photo') h += 180
    if (b.kind === 'buttons') h += 44
    if (b.kind === 'text') {
      const lines = (b.text ?? '').split('\n').reduce((sum, l) => sum + Math.max(1, Math.ceil(l.length / 34)), 0)
      h += lines * 21 + 6
    }
  }
  h += r.keyboard.length * 42
  if (n.data.input || n.data.list) h += 32
  return h
}

/** Positions for nodes that have none yet: entries on the left, screens in
 * columns by distance from the entries, action nodes beside their screens. */
export function layoutMissing(nodes: GNode[], edges: GEdge[], existing: Positions): Positions {
  const out: Positions = {}
  const missing = nodes.filter((n) => !existing[n.id])
  if (missing.length === 0) return out

  const next = new Map<string, string[]>()
  for (const e of edges) {
    const from = e.source.startsWith('a:') ? `s:${e.source.split(':')[1]}` : e.source
    if (!e.target.startsWith('s:')) continue
    next.set(from, [...(next.get(from) ?? []), e.target])
  }
  const depth = new Map<string, number>()
  const queue: string[] = []
  for (const n of nodes) {
    if (n.type !== 'entry') continue
    depth.set(n.id, 0)
    queue.push(n.id)
  }
  while (queue.length) {
    const id = queue.shift()!
    for (const t of next.get(id) ?? []) {
      if (!depth.has(t)) {
        depth.set(t, depth.get(id)! + 1)
        queue.push(t)
      }
    }
  }
  // Screens nothing leads to yet (a module's starter screens, say) are laid
  // out as their own trees to the right, rooted where nothing points to them.
  const reached = Math.max(0, ...depth.values())
  const pointed = new Set([...next.values()].flat())
  const rest = () => nodes.filter((n) => n.type === 'screen' && !depth.has(n.id))
  for (let left = rest(); left.length; left = rest()) {
    const root = left.find((n) => !pointed.has(n.id) || !left.some((m) => (next.get(m.id) ?? []).includes(n.id))) ?? left[0]
    const base = reached + 1
    depth.set(root.id, base)
    const q = [root.id]
    while (q.length) {
      const id = q.shift()!
      for (const t of next.get(id) ?? []) {
        if (!depth.has(t)) {
          depth.set(t, depth.get(id)! + 1)
          q.push(t)
        }
      }
    }
  }
  const maxDepth = Math.max(1, ...depth.values())
  const colX = (d: number) => (d === 0 ? 0 : 260 + (d - 1) * COL)
  const colY = new Map<number, number>()
  for (const n of nodes) {
    if (n.type === 'action') continue
    const d = depth.get(n.id) ?? maxDepth + 1
    const y = colY.get(d) ?? 0
    const h = estimateHeight(n)
    if (!existing[n.id]) out[n.id] = { x: colX(d), y }
    colY.set(d, y + h + (n.type === 'entry' ? 24 : 56))
  }
  for (const n of nodes) {
    if (n.type !== 'action' || existing[n.id]) continue
    const host = existing[`s:${n.data.screen}`] ?? out[`s:${n.data.screen}`] ?? { x: 0, y: 0 }
    const row = Number(n.data.ref.split('.')[1] ?? 0)
    out[n.id] = { x: host.x + SCREEN_W + 48, y: host.y + 60 + (Number.isFinite(row) ? row * 42 : 0) }
  }
  return out
}
