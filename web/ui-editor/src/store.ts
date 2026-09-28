import { create } from 'zustand'
import { produce } from 'immer'
import type { Issue, Manifest, MarketModule, Rendered, Theme } from './types'
import { engine, load, render, type LoadResult } from './wasm'
import { renameScreen } from './ops'
import { catalog, languagesOf, manifestOf, resolve, type Modules, type Resolved } from './market'

export type XY = { x: number; y: number }
export type Positions = Record<string, XY>

export type Selection =
  | { kind: 'screen'; id: string }
  | { kind: 'button'; screen: string; ref: string }
  | { kind: 'entry'; id: string }
  | { kind: 'module'; id: string }
  | null

/** Everything a person builds; saved, exported and undone as one. */
export interface Project {
  theme: Theme
  positions: Positions
  modules: Modules
  /** Descriptors of the person's own modules (tors-module.json). */
  own: MarketModule[]
}

export interface State extends Project {
  ready: boolean
  engineError?: string
  all: Map<string, MarketModule>
  resolved: Resolved
  manifest: Manifest
  selection: Selection
  lang: string
  sheet: '' | 'market' | 'export'
  simulator: boolean
  issues: Issue[]
  incoming: Record<string, string[]>
  loadError?: string
  rendered: Record<string, Rendered>
  past: Project[]
  future: Project[]
  toast?: { id: number; text: string; undo?: boolean }
  layoutNonce: number

  init(): Promise<void>
  edit(fn: (t: Theme) => void, coalesce?: string): void
  editModules(fn: (m: Modules) => void, coalesce?: string): void
  rename(from: string, to: string): void
  movePositions(p: Positions, record: boolean): void
  select(s: Selection): void
  undo(): void
  redo(): void
  setLang(l: string): void
  setSheet(s: State['sheet']): void
  setSimulator(v: boolean): void
  addModule(id: string): void
  removeModule(id: string): void
  addScreens(ids: string[], from: string): void
  addOwn(ms: MarketModule[]): string
  removeOwn(id: string): void
  replaceProject(p: Partial<Project>): void
  notify(text: string, undo?: boolean): void
}

const STORAGE_KEY = 'tors-builder:v1'
const HISTORY = 100

let lastCoalesce = { key: '', at: 0 }
let analyzeTimer: ReturnType<typeof setTimeout> | undefined
let saveTimer: ReturnType<typeof setTimeout> | undefined
let toastSeq = 0

export const emptyTheme = (): Theme => ({ version: 1, commands: { start: { default: '' } }, screens: {} })

function derive(p: Pick<Project, 'modules' | 'own'>) {
  const all = catalog(p.own)
  const resolved = resolve(p.modules, all)
  const manifest = manifestOf(resolved.set, all, languagesOf(p.modules))
  return { all, resolved, manifest }
}

function readSaved(): Project | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const v = JSON.parse(raw)
    if (v?.theme?.screens && v?.modules) return { theme: v.theme, positions: v.positions ?? {}, modules: v.modules, own: v.own ?? [] }
  } catch {
    // Storage may be unavailable (private mode, blocked site data).
  }
  return null
}

function writeSaved(s: Project) {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ theme: s.theme, positions: s.positions, modules: s.modules, own: s.own }))
    } catch {
      // Saving is a convenience; the builder works without it.
    }
  }, 400)
}

/** Theme with editor positions embedded — what export and the bot see. */
export function exportable(s: Pick<State, 'theme' | 'positions'>): Theme {
  return produce(s.theme, (t) => {
    t.editor = { positions: Object.fromEntries(Object.entries(s.positions).map(([k, v]) => [k, { x: Math.round(v.x), y: Math.round(v.y) }])) }
  })
}

const project = (s: Project): Project => ({ theme: s.theme, positions: s.positions, modules: s.modules, own: s.own })

export const useEditor = create<State>()((set, get) => {
  const analyze = (delay = 120) => {
    clearTimeout(analyzeTimer)
    analyzeTimer = setTimeout(async () => {
      const b = await engine()
      const { theme, manifest, lang } = get()
      let res: LoadResult
      try {
        res = load(b, theme, manifest)
      } catch (e) {
        set({ loadError: String(e) })
        return
      }
      if (!res.ok) {
        set({ loadError: res.error, issues: [] })
        return
      }
      const rendered: Record<string, Rendered> = {}
      for (const id of Object.keys(theme.screens)) {
        const params = Object.fromEntries((res.incoming[id] ?? []).map((p) => [p, '…']))
        const r = render(b, id, { Lang: lang, ShowHidden: true, Params: params })
        if (!('error' in r)) rendered[id] = r
      }
      set({ loadError: undefined, issues: res.issues, incoming: res.incoming, rendered })
    }, delay)
  }

  const push = (coalesce?: string) => {
    const now = Date.now()
    if (coalesce && lastCoalesce.key === coalesce && now - lastCoalesce.at < 1000) {
      lastCoalesce.at = now
      return
    }
    lastCoalesce = { key: coalesce ?? '', at: now }
    set((s) => ({ past: [...s.past.slice(-HISTORY + 1), project(s)], future: [] }))
  }

  /** Applies a new project state: derives the manifest, saves, re-checks. */
  const apply = (p: Partial<Project>) => {
    const next = { ...project(get()), ...p }
    const derived = p.modules || p.own ? derive(next) : undefined
    const langs = (derived?.manifest ?? get().manifest).languages
    set({ ...p, ...derived, lang: langs.includes(get().lang) ? get().lang : langs[0] ?? 'ru' })
    writeSaved(get())
    analyze()
  }

  const initial: Project = { theme: emptyTheme(), positions: {}, modules: {}, own: [] }

  return {
    ...initial,
    ...derive(initial),
    ready: false,
    selection: null,
    lang: 'ru',
    sheet: '',
    simulator: false,
    issues: [],
    incoming: {},
    rendered: {},
    past: [],
    future: [],
    layoutNonce: 0,

    async init() {
      try {
        await engine()
        const saved = readSaved()
        if (saved) set({ ...saved, ...derive(saved) })
        set({ ready: true, lang: get().manifest.languages[0] ?? 'ru' })
        analyze(0)
      } catch (e) {
        set({ engineError: String(e) })
      }
    },

    edit(fn, coalesce) {
      push(coalesce)
      apply({ theme: produce(get().theme, fn) })
    },

    editModules(fn, coalesce) {
      push(coalesce)
      apply({ modules: produce(get().modules, fn) })
    },

    rename(from, to) {
      if (!to || from === to || get().theme.screens[to]) return
      push()
      const s = get()
      const positions = { ...s.positions }
      for (const key of Object.keys(positions)) {
        if (key === `s:${from}`) positions[`s:${to}`] = positions[key]
        else if (key.startsWith(`a:${from}:`)) positions[`a:${to}:${key.slice(3 + from.length)}`] = positions[key]
        else continue
        delete positions[key]
      }
      const sel = s.selection
      const selection: Selection =
        sel?.kind === 'screen' && sel.id === from ? { kind: 'screen', id: to }
        : sel?.kind === 'button' && sel.screen === from ? { ...sel, screen: to }
        : sel
      set({ selection })
      apply({ theme: produce(s.theme, (t) => renameScreen(t, from, to)), positions })
    },

    movePositions(p, record) {
      if (record) push()
      set((s) => ({ positions: { ...s.positions, ...p } }))
      writeSaved(get())
    },

    select: (selection) => set({ selection }),

    undo() {
      const prev = get().past.at(-1)
      if (!prev) return
      set((s) => ({ past: s.past.slice(0, -1), future: [project(s), ...s.future] }))
      lastCoalesce = { key: '', at: 0 }
      apply(prev)
    },

    redo() {
      const next = get().future[0]
      if (!next) return
      set((s) => ({ future: s.future.slice(1), past: [...s.past, project(s)] }))
      apply(next)
    },

    setLang(lang) {
      set({ lang })
      analyze(0)
    },
    setSheet: (sheet) => set({ sheet }),
    setSimulator: (simulator) => set({ simulator }),

    addModule(id) {
      const before = get().resolved.set
      push()
      const modules = produce(get().modules, (m) => {
        m[id] = { ...m[id], config: m[id]?.config ?? {}, added: true }
      })
      const { all, resolved } = derive({ modules, own: get().own })
      // Starter screens of every module the addition brought in.
      const screens: string[] = []
      for (const x of resolved.set) {
        if (before.has(x)) continue
        for (const sid of Object.keys(all.get(x)?.ui?.screens ?? {})) screens.push(sid)
      }
      const theme = produce(get().theme, (t) => {
        for (const x of resolved.set) {
          if (before.has(x)) continue
          for (const [sid, sc] of Object.entries(all.get(x)?.ui?.screens ?? {})) if (!t.screens[sid]) t.screens[sid] = structuredClone(sc)
        }
      })
      apply({ modules, theme })
      const added = screens.filter((s) => !get().positions[`s:${s}`]).length
      const name = all.get(id)?.name ?? id
      get().notify(added ? `${name}: добавлено экранов — ${added}` : `${name} добавлен`)
    },

    removeModule(id) {
      const s = get()
      const m = s.all.get(id)
      push()
      const modules = produce(s.modules, (x) => {
        if (x[id]) x[id].added = false
      })
      const { resolved } = derive({ modules, own: s.own })
      // Screens of modules that leave the bot go with them.
      const gone = [...s.resolved.set].filter((x) => !resolved.set.has(x))
      let removed = 0
      const theme = produce(s.theme, (t) => {
        for (const x of gone) {
          for (const sid of Object.keys(s.all.get(x)?.ui?.screens ?? {})) {
            if (t.screens[sid]) {
              delete t.screens[sid]
              removed++
            }
          }
        }
      })
      set({ selection: null })
      apply({ modules, theme })
      get().notify(removed ? `${m?.name ?? id} удалён вместе с экранами: ${removed}` : `${m?.name ?? id} удалён`, true)
    },

    addScreens(ids, from) {
      const sc = get().all.get(from)?.ui?.screens ?? {}
      get().edit((t) => {
        for (const id of ids) if (!t.screens[id] && sc[id]) t.screens[id] = structuredClone(sc[id])
      })
    },

    addOwn(ms) {
      const s = get()
      const official = new Set(s.all.keys())
      const fresh = ms.filter((m) => !official.has(m.id) || s.own.some((o) => o.id === m.id))
      if (!fresh.length) return 'Эти модули уже есть в маркете'
      push()
      const own = [...s.own.filter((o) => !fresh.some((f) => f.id === o.id)), ...fresh.map((m) => ({ ...m, trust: 'own' as const }))]
      apply({ own })
      const skipped = ms.length - fresh.length
      return `Добавлено в «Свои»: ${fresh.length}${skipped ? `, пропущено (уже есть): ${skipped}` : ''}`
    },

    removeOwn(id) {
      push()
      apply({ own: get().own.filter((o) => o.id !== id), modules: produce(get().modules, (m) => void delete m[id]) })
    },

    replaceProject(p) {
      push()
      set({ selection: null, layoutNonce: get().layoutNonce + 1 })
      apply({ ...p, ...(p.theme ? { theme: { ...p.theme, screens: p.theme.screens ?? {} } } : {}) })
    },

    notify(text, undo) {
      const id = ++toastSeq
      set({ toast: { id, text, undo } })
      setTimeout(() => {
        if (get().toast?.id === id) set({ toast: undefined })
      }, 3600)
    },
  }
})
