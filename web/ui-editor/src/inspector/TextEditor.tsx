import { useRef, useState } from 'react'
import type { Text } from '../types'
import { useEditor } from '../store'
import { LangTabs, TextArea, VarMenu, type VarGroup } from './fields'
import { insertAt, wrapSelection } from './vars'

/** Multilingual text with Markdown tools and variable insertion. */
export function TextEditor({ value, onChange, vars, multiline = true, placeholder }: {
  value: Text | undefined
  onChange(t: Text): void
  vars: VarGroup[]
  multiline?: boolean
  placeholder?: string
}) {
  const langs = useEditor((s) => s.manifest.languages)
  const globalLang = useEditor((s) => s.lang)
  const [lang, setLang] = useState(langs.includes(globalLang) ? globalLang : langs[0])
  const area = useRef<HTMLTextAreaElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const text = value?.[lang] ?? ''
  const set = (v: string) => onChange({ ...value, [lang]: v })

  const tools: [string, string, string, string][] = [
    ['B', 'Жирный', '**', '**'],
    ['I', 'Курсив', '_', '_'],
    ['S', 'Зачёркнутый', '~~', '~~'],
    ['</>', 'Код', '`', '`'],
    ['||', 'Спойлер', '||', '||'],
    ['🔗', 'Ссылка', '[', '](https://)'],
  ]

  return (
    <div className="text-editor">
      <div className="text-editor-bar">
        <LangTabs langs={langs} value={lang} onChange={setLang} filled={(l) => !!value?.[l]} />
        <span className="spacer" />
        {multiline
          ? tools.map(([label, title, l, r]) => (
              <button key={title} type="button" className="tool-btn" title={title} onClick={() => set(wrapSelection(area.current, text, l, r))}>
                {label}
              </button>
            ))
          : null}
        <VarMenu groups={vars} onPick={(expr) => set(insertAt(multiline ? area.current : input.current, text, expr))} />
      </div>
      {multiline ? (
        <TextArea areaRef={area} value={text} onChange={set} placeholder={placeholder ?? 'Текст сообщения. Markdown: **жирный**, _курсив_, `код`'} />
      ) : (
        <input ref={input} className="input" value={text} placeholder={placeholder} spellCheck={false} onChange={(e) => set(e.target.value)} />
      )}
    </div>
  )
}
