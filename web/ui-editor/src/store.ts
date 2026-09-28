import { create } from 'zustand'
import { produce } from 'immer'
import type { Issue, Manifest, Rendered, Theme } from './types'
import { engine, load, render, standard, type LoadResult } from './wasm'
import { renameScreen } from './ops'

export type XY = { x: number; y: number }
export type Positions = Record<string, XY>

export type Selection =
  | { kind: 'screen'; id: string }
  | { kind: 'button'; screen: string; ref: string }
  | { kind: 'entry'; id: string }
  | null

interface Snapshot {
  theme: Theme
  positions: Positions
}

export interface State {
  ready: boolean
  engineError?: string
  theme: Theme
  manifest: Manifest
  positions: Positions
  selection: Selection
  lang: string
  tgDark: boolean
  renderer: 'rich' | 'classic'
  simulator: boolean
  issues: Issue[]
  incoming: Record<string, string[]>
  loadError?: string
  rendered: Record<string, Rendered>
  past: Snapshot[]
  future: Snapshot[]
  toast?: { id: number; text: string; undo?: boolean }
  layoutNonce: number

  init(): Promise<void>
  edit(fn: (t: Theme) => void, coalesce?: string): void
  rename(from: string, to: string): void
  movePositions(p: Positions, record: boolean): void
  select(s: Selection): void
  undo(): void
  redo(): void
  setLang(l: string): void
  setTgDark(v: boolean): void
  setRenderer(r: 'rich' | 'classic'): void
  setSimulator(v: boolean): void
  replaceTheme(t: Theme): void
  replaceManifest(m: Manifest): void
  resetStandard(): Promise<void>
  relayout(): void
  notify(text: string, undo?: boolean): void
}

const STORAGE_KEY = 'tors-editor:v1'
const HISTORY = 100

let lastCoalesce = { key: '', at: 0 }
let analyzeTimer: ReturnType<typeof setTimeout> | undefined
let saveTimer: ReturnType<typeof setTimeout> | undefined
let toastSeq = 0

const emptyTheme: Theme = { version: 1, screens: {} }
const emptyManifest: Manifest = { version: 1, languages: ['ru'], user: [], data: {}, actions: {}, conditions: {}, events: {} }

function readSaved(): { theme: Theme; manifest: Manifest; positions: Positions } | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const v = JSON.parse(raw)
    if (v?.theme?.screens && v?.manifest?.data) return { theme: v.theme, manifest: v.manifest, positions: v.positions ?? {} }
  } catch {
    // Storage may be unavailable (private mode, blocked site data).
  }
  return null
}

function writeSaved(s: State) {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ theme: s.theme, manifest: s.manifest, positions: s.positions }))
    } catch {
      // Saving is a convenience; the editor works without it.
    }
  }, 400)
}

/** Theme with editor positions embedded — what export and the bot see. */
export function exportable(s: Pick<State, 'theme' | 'positions'>): Theme {
  return produce(s.theme, (t) => {
    t.editor = { positions: Object.fromEntries(Object.entries(s.positions).map(([k, v]) => [k, { x: Math.round(v.x), y: Math.round(v.y) }])) }
  })
}

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

  const snapshot = (): Snapshot => ({ theme: get().theme, positions: get().positions })
  const push = (coalesce?: string) => {
    const now = Date.now()
    if (coalesce && lastCoalesce.key === coalesce && now - lastCoalesce.at < 1000) {
      lastCoalesce.at = now
      return
    }
    lastCoalesce = { key: coalesce ?? '', at: now }
    set((s) => ({ past: [...s.past.slice(-HISTORY + 1), snapshot()], future: [] }))
  }

  return {
    ready: false,
    theme: emptyTheme,
    manifest: emptyManifest,
    positions: {},
    selection: null,
    lang: 'ru',
    tgDark: false,
    renderer: 'rich',
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
        if (saved) {
          set({ theme: saved.theme, manifest: saved.manifest, positions: saved.positions })
        } else {
          const std = await standard()
          set({ theme: std.theme, manifest: std.manifest, positions: std.theme.editor?.positions ?? {} })
        }
        set({ ready: true, lang: get().manifest.languages[0] ?? 'ru' })
        analyze(0)
      } catch (e) {
        set({ engineError: String(e) })
      }
    },

    edit(fn, coalesce) {
      push(coalesce)
      set((s) => ({ theme: produce(s.theme, fn) }))
      writeSaved(get())
      analyze()
    },

    rename(from, to) {
      if (!to || from === to || get().theme.screens[to]) return
      push()
      set((s) => {
        const positions = { ...s.positions }
        for (const key of Object.keys(positions)) {
          if (key === `s:${from}`) positions[`s:${to}`] = positions[key]
          else if (key.startsWith(`a:${from}:`)) positions[`a:${to}:${key.slice(3 + from.length)}`] = positions[key]
          else continue
          delete positions[key]
        }
        const sel = s.selection
        const selection =
          sel?.kind === 'screen' && sel.id === from ? { kind: 'screen' as const, id: to }
          : sel?.kind === 'button' && sel.screen === from ? { ...sel, screen: to }
          : sel
        return { theme: produce(s.theme, (t) => renameScreen(t, from, to)), positions, selection }
      })
      writeSaved(get())
      analyze()
    },

    movePositions(p, record) {
      if (record) push()
      set((s) => ({ positions: { ...s.positions, ...p } }))
      writeSaved(get())
    },

    select: (selection) => set({ selection }),

    undo() {
      const { past } = get()
      const prev = past.at(-1)
      if (!prev) return
      set((s) => ({ past: s.past.slice(0, -1), future: [snapshot(), ...s.future], theme: prev.theme, positions: prev.positions }))
      lastCoalesce = { key: '', at: 0 }
      writeSaved(get())
      analyze(0)
    },

    redo() {
      const next = get().future[0]
      if (!next) return
      set((s) => ({ future: s.future.slice(1), past: [...s.past, snapshot()], theme: next.theme, positions: next.positions }))
      writeSaved(get())
      analyze(0)
    },

    setLang(lang) {
      set({ lang })
      analyze(0)
    },
    setTgDark: (tgDark) => set({ tgDark }),
    setRenderer: (renderer) => set({ renderer }),
    setSimulator: (simulator) => set({ simulator }),

    replaceTheme(theme) {
      push()
      set({ theme: { ...theme, screens: theme.screens ?? {} }, positions: theme.editor?.positions ?? {}, selection: null, layoutNonce: get().layoutNonce + 1 })
      writeSaved(get())
      analyze(0)
    },

    replaceManifest(manifest) {
      push()
      set({ manifest, lang: manifest.languages.includes(get().lang) ? get().lang : manifest.languages[0] ?? 'ru' })
      writeSaved(get())
      analyze(0)
    },

    async resetStandard() {
      const std = await standard()
      push()
      set({ theme: std.theme, manifest: std.manifest, positions: {}, selection: null, layoutNonce: get().layoutNonce + 1 })
      writeSaved(get())
      analyze(0)
    },

    relayout() {
      push()
      set((s) => ({ positions: {}, layoutNonce: s.layoutNonce + 1 }))
      writeSaved(get())
    },

    notify(text, undo) {
      const id = ++toastSeq
      set({ toast: { id, text, undo } })
      setTimeout(() => {
        if (get().toast?.id === id) set({ toast: undefined })
      }, 3200)
    },
  }
})
