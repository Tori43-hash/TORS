import type { Issue, Manifest, Rendered, RenderOptions, Theme } from './types'

interface Bridge {
  load(theme: string, manifest: string): string
  render(id: string, options: string): string
  format(theme: string): string
  standard(): string
}

declare global {
  // Provided by wasm_exec.js (Go distribution) and by the Go program.
  interface Window {
    Go: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): Promise<void> }
    torsTheme?: Bridge
    onTorsThemeReady?: () => void
  }
}

export interface LoadResult {
  ok: boolean
  error?: string
  issues: Issue[]
  incoming: Record<string, string[]>
}

let bridge: Promise<Bridge> | null = null

/** Starts the Go theme engine once; resolves when it is ready to call. */
export function engine(): Promise<Bridge> {
  bridge ??= new Promise<Bridge>((resolve, reject) => {
    window.onTorsThemeReady = () => resolve(window.torsTheme!)
    const go = new window.Go()
    const url = new URL('tors-theme.wasm', document.baseURI)
    WebAssembly.instantiateStreaming(fetch(url), go.importObject)
      .catch(async () => WebAssembly.instantiate(await (await fetch(url)).arrayBuffer(), go.importObject))
      .then((r) => {
        const instance = 'instance' in r ? r.instance : (r as WebAssembly.Instance)
        void go.run(instance)
      })
      .catch(reject)
  })
  return bridge
}

export async function standard(): Promise<{ theme: Theme; manifest: Manifest }> {
  return JSON.parse((await engine()).standard())
}

export function load(b: Bridge, theme: Theme, manifest: Manifest): LoadResult {
  return JSON.parse(b.load(JSON.stringify(theme), JSON.stringify(manifest)))
}

export function render(b: Bridge, id: string, opt: RenderOptions): Rendered | { error: string } {
  return JSON.parse(b.render(id, JSON.stringify(opt)))
}

/** Canonical, indented JSON produced by the Go encoder — what the bot reads. */
export async function formatTheme(theme: Theme): Promise<{ json?: string; error?: string }> {
  return JSON.parse((await engine()).format(JSON.stringify(theme)))
}

export type { Bridge }
