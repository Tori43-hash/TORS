// The module market on the builder side: which modules a bot is made of, the
// manifest screens are checked against, and the bot.json the bot runs from.

import indexJson from '../../../registry/index.json'
import type { ConfigField, Field, Manifest, MarketModule, Pack, Theme } from './types'

export const index = indexJson as unknown as Pack

/** Per-module state in a project. `added` marks modules the person chose;
 * the rest are pulled in by requirements and only keep their settings. */
export interface ModuleState {
  added?: boolean
  config: Record<string, unknown>
  /** Instance name for modules placed into a named map, such as panels. */
  instance?: string
}

export type Modules = Record<string, ModuleState>

/** The bot itself: always in the build. */
export const BASE = 'telegram'

export const CATEGORY: Record<string, string> = {
  sales: 'Продажи',
  panel: 'Панели',
  payment: 'Оплата',
  feature: 'Возможности',
  core: 'Основа',
}

export const TRUST: Record<string, string> = {
  official: 'Официальный',
  verified: 'Проверенный',
  own: 'Свой · не проверен',
}

/** Template fields of .User, as the ui module defines them. */
export const USER_FIELDS: Field[] = [
  { name: 'FirstName', type: 'string', title: 'Имя', sample: 'Анна' },
  { name: 'Username', type: 'string', title: 'Username', sample: 'anna' },
  { name: 'ID', type: 'int', title: 'Telegram ID', sample: 123456789 },
]

export function catalog(own: MarketModule[]): Map<string, MarketModule> {
  const all = new Map<string, MarketModule>()
  for (const m of index.modules) all.set(m.id, m)
  for (const m of own) if (!all.has(m.id)) all.set(m.id, { ...m, trust: 'own' })
  return all
}

const namespaceOf = (id: string) => id.split('.').slice(0, -1).join('.')

export interface Need {
  namespace: string
  by: string
  options: MarketModule[]
}

export interface Resolved {
  /** Every module the bot is built with. */
  set: Set<string>
  /** Namespaces a module needs but nothing fills yet. */
  needs: Need[]
  /** Who pulled a module in: id → modules that require it. */
  requiredBy: Map<string, string[]>
}

export function resolve(modules: Modules, all: Map<string, MarketModule>): Resolved {
  const set = new Set<string>()
  const requiredBy = new Map<string, string[]>()
  const visit = (id: string, by?: string) => {
    if (by) requiredBy.set(id, [...(requiredBy.get(id) ?? []), by])
    if (set.has(id)) return
    const m = all.get(id)
    if (!m) return
    set.add(id)
    for (const r of m.requires ?? []) visit(r, id)
    // Some settings need more modules, e.g. webhooks need the http server.
    for (const f of m.config ?? []) {
      const v = value(f, modules[id]?.config)
      for (const o of f.options ?? []) if (o.value === v) for (const r of o.requires ?? []) visit(r, id)
    }
  }
  visit(BASE)
  for (const [id, st] of Object.entries(modules)) if (st.added) visit(id)

  const needs: Need[] = []
  for (let changed = true; changed; ) {
    changed = false
    for (const id of [...set]) {
      for (const ns of all.get(id)?.needs ?? []) {
        if ([...set].some((x) => namespaceOf(x) === ns)) continue
        const options = [...all.values()].filter((m) => namespaceOf(m.id) === ns)
        const pick = options.find((m) => m.default && m.trust === 'official') ?? options.find((m) => m.default)
        if (pick) {
          visit(pick.id, id)
          changed = true
        } else if (!needs.some((n) => n.namespace === ns)) needs.push({ namespace: ns, by: id, options })
      }
    }
  }
  return { set, needs: needs.filter((n) => ![...set].some((x) => namespaceOf(x) === n.namespace)), requiredBy }
}

export function languagesOf(modules: Modules): string[] {
  const v = modules.users?.config.languages
  return Array.isArray(v) && v.length ? (v as string[]) : ['ru', 'en']
}

/** The manifest of the modules in the build: what screens may use. */
export function manifestOf(set: Set<string>, all: Map<string, MarketModule>, languages: string[]): Manifest {
  const m: Manifest = { version: 1, languages, user: USER_FIELDS, data: {}, actions: {}, conditions: {}, events: {} }
  for (const id of [...set].sort()) {
    const ui = all.get(id)?.ui
    if (!ui) continue
    Object.assign(m.data, ui.data)
    Object.assign(m.actions, ui.actions)
    Object.assign(m.conditions, ui.conditions)
    Object.assign(m.events, ui.events)
  }
  return m
}

/** A setting's value: what the person entered, or the default. */
export function value(f: ConfigField, config: Record<string, unknown> | undefined): unknown {
  const v = config?.[f.key]
  return v === undefined ? f.default : v
}

const empty = (v: unknown) =>
  v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0) ||
  (typeof v === 'object' && !Array.isArray(v) && Object.values(v as object).every((x) => x === '' || x === undefined))

/** Settings that are required but empty, as field titles. */
export function missing(m: MarketModule, config: Record<string, unknown> | undefined): string[] {
  return (m.config ?? []).filter((f) => f.required && empty(value(f, config))).map((f) => f.title)
}

function clean(fields: ConfigField[] | undefined, config: Record<string, unknown> | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields ?? []) {
    let v = value(f, config)
    if (empty(v)) continue
    if (f.type === 'number') v = Number(v)
    if (f.type === 'list' && Array.isArray(v)) v = v.map((item) => clean(f.item, item as Record<string, unknown>))
    out[f.key] = v
  }
  return out
}

/** Instance names of named guest modules for a host field, e.g. panel names. */
export function instances(modules: Modules, set: Set<string>, all: Map<string, MarketModule>, app: string): string[] {
  const out: string[] = []
  for (const id of set) {
    const h = all.get(id)?.host
    if (h?.app === app && h.named) out.push(modules[id]?.instance || 'main')
  }
  return out.sort()
}

export interface Bot {
  build: { core: string; modules: { source: string; version?: string }[] }
  apps: Record<string, Record<string, unknown>>
}

/** The config the bot runs from: build section, settings of every app and the theme. */
export function exportBot(theme: Theme, modules: Modules, all: Map<string, MarketModule>): Bot {
  const { set } = resolve(modules, all)
  const apps: Record<string, Record<string, unknown>> = {}
  const sources = new Map<string, string | undefined>()
  for (const id of [...set].sort()) {
    const m = all.get(id)!
    sources.set(m.package, m.version)
    const cfg = clean(m.config, modules[id]?.config)
    if (m.host) {
      const obj = { [m.host.key]: id.split('.').at(-1)!, ...cfg }
      const app = (apps[m.host.app] ??= {})
      if (m.host.named) ((app[m.host.field] ??= {}) as Record<string, unknown>)[modules[id]?.instance || 'main'] = obj
      else app[m.host.field] = obj
    } else if (!id.includes('.')) {
      apps[id] = { ...cfg, ...apps[id] }
    }
  }
  apps.telegram = { ...apps.telegram, theme }
  return {
    build: {
      core: index.core || 'latest',
      modules: [...sources].map(([source, version]) => (version ? { source, version } : { source })),
    },
    apps,
  }
}

/** Environment variables the config refers to, such as BOT_TOKEN. */
export function envVars(bot: Bot): string[] {
  const out = new Set<string>()
  const walk = (v: unknown) => {
    if (typeof v === 'string') for (const m of v.matchAll(/\{env\.([A-Za-z0-9_]+)\}/g)) out.add(m[1])
    else if (Array.isArray(v)) v.forEach(walk)
    else if (v && typeof v === 'object') Object.values(v).forEach(walk)
  }
  const { telegram, ...rest } = bot.apps
  const { theme: _theme, ...tg } = telegram ?? {}
  walk(rest)
  walk(tg)
  return [...out].sort()
}

/** Reads a bot.json back into modules and a theme. */
export function importBot(bot: Bot, all: Map<string, MarketModule>): { modules: Modules; theme: Theme | undefined; unknown: string[] } {
  const modules: Modules = {}
  const unknown: string[] = []
  const byPackage = new Map<string, MarketModule[]>()
  for (const m of all.values()) byPackage.set(m.package, [...(byPackage.get(m.package) ?? []), m])
  const present = new Set<string>()
  for (const src of bot.build?.modules ?? []) {
    const ms = byPackage.get(src.source)
    if (!ms) unknown.push(src.source)
    for (const m of ms ?? []) present.add(m.id)
  }
  for (const id of present) {
    const m = all.get(id)!
    let raw: Record<string, unknown> | undefined
    let instance: string | undefined
    if (m.host) {
      const field = bot.apps?.[m.host.app]?.[m.host.field] as Record<string, unknown> | undefined
      const short = id.split('.').at(-1)
      if (m.host.named) {
        for (const [name, v] of Object.entries(field ?? {})) {
          if ((v as Record<string, unknown>)?.[m.host.key] === short) {
            raw = v as Record<string, unknown>
            instance = name
          }
        }
      } else if (field?.[m.host.key] === short) raw = field
      if (!raw) continue
    } else if (!id.includes('.')) {
      raw = bot.apps?.[id]
      if (!raw) continue
    } else continue
    const config: Record<string, unknown> = {}
    for (const f of m.config ?? []) if (raw[f.key] !== undefined) config[f.key] = raw[f.key]
    modules[id] = { added: !m.hidden, config, ...(instance && instance !== 'main' ? { instance } : {}) }
  }
  return { modules, theme: bot.apps?.telegram?.theme as Theme | undefined, unknown }
}
