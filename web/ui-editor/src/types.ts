// Mirrors modules/telegram/theme. The Go package is the source of truth:
// the editor only builds this JSON, validation and rendering run in Go (WASM).

export type Text = Record<string, string>

export interface Button {
  text?: Text
  emoji?: string
  icon?: string
  style?: '' | 'primary' | 'success' | 'danger' | 'link'
  goto?: string
  action?: string
  on?: Record<string, string>
  params?: Record<string, string>
  url?: string
  web_app?: string
  copy_text?: string
  back?: boolean
  home?: boolean
  visible_if?: string
  disabled_if?: string
}

export interface Repeat {
  repeat: string
  columns?: number
  page_size?: number
  button: Button
}

export type Row = Button[] | Repeat | { use: string }

export interface Inline {
  align?: '' | 'left' | 'center' | 'right'
  items: Button[]
}

export interface Block {
  photo?: string
  text?: Text
  buttons?: Inline
}

export interface Input {
  action: string
  param: string
  on?: Record<string, string>
}

export interface Screen {
  data?: Record<string, string>
  blocks?: Block[]
  keyboard?: Row[]
  empty?: string
  input?: Input
}

export interface Entry {
  default: string
  new_user?: string
}

export interface Theme {
  version: number
  commands?: Record<string, Entry | string>
  events?: Record<string, string>
  routes?: Record<string, Record<string, string>>
  fragments?: Record<string, Button[][]>
  screens: Record<string, Screen>
  editor?: { positions?: Record<string, { x: number; y: number }> }
}

export interface Field {
  name: string
  type: string
  title?: string
  sample?: unknown
}

export interface DataSource {
  title: string
  params?: string[]
  list?: boolean
  fields: Field[]
  samples?: Record<string, unknown>[]
}

export interface Outcome {
  name: string
  title?: string
  default?: string
  terminal?: boolean
}

export interface Action {
  title: string
  label?: Text
  params?: string[]
  input?: boolean
  outcomes: Outcome[]
}

export interface Manifest {
  version: number
  languages: string[]
  user: Field[]
  data: Record<string, DataSource>
  actions: Record<string, Action>
  conditions: Record<string, { title: string; sample: boolean }>
  events: Record<string, { title: string; default?: string; fields?: Field[] }>
}

export interface Issue {
  level: 'error' | 'warning'
  screen?: string
  ref?: string
  message: string
}

export type Kind = 'goto' | 'action' | 'url' | 'web_app' | 'copy_text' | 'back' | 'home' | 'page' | 'noop'

export interface RenderedButton {
  ref: string
  label: string
  icon?: string
  style?: string
  kind: Kind
  target?: string
  params?: Record<string, string>
  outcomes?: Record<string, string>
  disabled?: boolean
  hidden?: boolean
  repeat?: boolean
}

export interface RenderedBlock {
  kind: 'photo' | 'text' | 'buttons'
  photo?: string
  text?: string
  align?: string
  buttons?: RenderedButton[]
}

export interface Rendered {
  screen: string
  blocks: RenderedBlock[]
  keyboard: RenderedButton[][]
  input?: Input
  redirect?: string
  errors?: Issue[]
}

export interface RenderOptions {
  Lang: string
  Params?: Record<string, string>
  Conditions?: Record<string, boolean>
  Pages?: Record<string, number>
  ShowHidden?: boolean
}
