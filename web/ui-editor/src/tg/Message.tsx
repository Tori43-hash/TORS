import { memo, type ReactNode } from 'react'
import { Handle, Position } from '@xyflow/react'
import type { Rendered, RenderedBlock, RenderedButton } from '../types'
import { markdown } from '../markdown'

// Renders a bot message the way Telegram draws it: a bubble with the body and
// the inline keyboard under it. Metrics follow Telegram Web: 40px keyboard
// rows, 2px gaps, 6px button radius, 15px outer corners, 14px/500 labels.

export interface Editing {
  selectedRef?: string
  onSelect(ref: string): void
  handles: boolean
}

interface Props {
  r: Rendered
  mode: 'rich' | 'classic'
  time?: string
  editing?: Editing
  onPress?(b: RenderedButton): void
  footer?: ReactNode
}

const cornerIcon: Partial<Record<RenderedButton['kind'], ReactNode>> = {
  url: (
    <svg viewBox="0 0 10 10" className="tg-kb-corner" aria-hidden>
      <path d="M3 2h5v5M8 2 2.5 7.5" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  ),
  copy_text: (
    <svg viewBox="0 0 10 10" className="tg-kb-corner" aria-hidden>
      <rect x="3.2" y="3.2" width="5" height="5" rx="1.2" fill="none" stroke="currentColor" strokeWidth="1.2" />
      <path d="M6.8 1.8H2.8a1 1 0 0 0-1 1v4" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  ),
  web_app: (
    <svg viewBox="0 0 10 10" className="tg-kb-corner" aria-hidden>
      <rect x="1.8" y="1.8" width="6.4" height="6.4" rx="1.6" fill="none" stroke="currentColor" strokeWidth="1.2" />
      <path d="M1.8 4h6.4" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  ),
}

function Photo({ src }: { src: string }) {
  if (/^(https?:|data:image\/)/.test(src)) return <img className="tg-photo" src={src} alt="" draggable={false} />
  return (
    <div className="tg-photo tg-photo-placeholder">
      <svg viewBox="0 0 24 24" aria-hidden>
        <rect x="3" y="5" width="18" height="14" rx="3" fill="none" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="9" cy="10" r="1.6" fill="currentColor" />
        <path d="m4 17 5-4.5 3.5 3 3-2.5L20 17" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      </svg>
      <span>{src}</span>
    </div>
  )
}

function KbButton({ b, editing, onPress, rich, handle }: { b: RenderedButton; editing?: Editing; onPress?(b: RenderedButton): void; rich?: boolean; handle: boolean }) {
  const cls = [
    rich ? 'tg-rich-btn' : 'tg-kb-btn',
    b.style ? `tg-style-${b.style}` : '',
    b.disabled ? 'is-disabled' : '',
    b.hidden ? 'is-hidden' : '',
    editing?.selectedRef === b.ref ? 'is-selected' : '',
  ].join(' ')
  const pressable = !!onPress && !b.disabled && b.kind !== 'noop'
  return (
    <button
      type="button"
      className={cls}
      title={b.hidden ? 'Скрыта по условию' : undefined}
      onClick={(e) => {
        if (editing && b.kind !== 'page' && b.kind !== 'noop') {
          e.stopPropagation()
          editing.onSelect(b.ref)
        } else if (pressable) onPress!(b)
      }}
    >
      {b.icon ? <span className="tg-custom-emoji" aria-hidden /> : null}
      <span className="tg-kb-text">{b.label || ' '}</span>
      {cornerIcon[b.kind]}
      {handle && editing?.handles ? <Handle type="source" id={b.ref} position={Position.Right} className="tg-handle" /> : null}
    </button>
  )
}

function Keyboard({ rows, editing, onPress }: { rows: RenderedButton[][]; editing?: Editing; onPress?(b: RenderedButton): void }) {
  const seen = new Set<string>()
  return (
    <div className="tg-kb">
      {rows.map((row, i) => (
        <div className={`tg-kb-row${i === rows.length - 1 ? ' is-last' : ''}`} key={i}>
          {row.map((b, j) => {
            const first = !seen.has(b.ref)
            seen.add(b.ref)
            return <KbButton key={j} b={b} editing={editing} onPress={onPress} handle={first && b.kind !== 'page' && b.kind !== 'noop'} />
          })}
        </div>
      ))}
    </div>
  )
}

function Body({ blocks, mode, editing, onPress, time }: { blocks: RenderedBlock[]; mode: 'rich' | 'classic'; editing?: Editing; onPress?(b: RenderedButton): void; time: string }) {
  const out: ReactNode[] = []
  const last = blocks.length - 1
  blocks.forEach((b, i) => {
    if (b.kind === 'photo') {
      out.push(<div key={i} className={`tg-media${i === 0 ? ' is-first' : ''}${i === last ? ' is-last' : ''}`}><Photo src={b.photo!} /></div>)
    } else if (b.kind === 'text') {
      out.push(
        <div key={i} className="tg-text" dangerouslySetInnerHTML={{ __html: markdown(b.text ?? '', { headings: mode === 'rich' }) }} />,
      )
    } else if (b.kind === 'buttons') {
      out.push(
        <div key={i} className={`tg-rich-row align-${b.align || 'left'}`}>
          {b.buttons!.map((btn, j) => (
            <KbButton key={j} b={btn} editing={editing} onPress={onPress} rich handle />
          ))}
        </div>,
      )
    }
  })
  const endsWithMedia = blocks.at(-1)?.kind === 'photo'
  out.push(
    <span key="time" className={`tg-time${endsWithMedia ? ' on-media' : ''}`}>
      {time}
    </span>,
  )
  return <>{out}</>
}

/** Classic messages: one media on top (or under the caption), text as caption,
 * body buttons moved to the keyboard — what the fallback renderer sends. */
function toClassic(r: Rendered): Rendered {
  const photoIndex = r.blocks.findIndex((b) => b.kind === 'photo')
  const firstText = r.blocks.findIndex((b) => b.kind === 'text')
  const text = r.blocks.filter((b) => b.kind === 'text').map((b) => b.text).join('\n\n')
  const blocks: RenderedBlock[] = []
  const photo = photoIndex >= 0 ? r.blocks[photoIndex] : undefined
  const photoBelow = photo && firstText >= 0 && photoIndex > firstText
  if (photo && !photoBelow) blocks.push(photo)
  if (text) blocks.push({ kind: 'text', text })
  if (photo && photoBelow) blocks.push(photo)
  const bodyRows = r.blocks.filter((b) => b.kind === 'buttons').map((b) => b.buttons!.map((x) => ({ ...x, style: x.style === 'link' ? '' : x.style })))
  return { ...r, blocks, keyboard: [...bodyRows, ...r.keyboard] }
}

export const TgMessage = memo(function TgMessage({ r, mode, time = '12:00', editing, onPress, footer }: Props) {
  const view = mode === 'classic' ? toClassic(r) : r
  const hasKb = view.keyboard.length > 0
  const onlyMedia = view.blocks.length === 1 && view.blocks[0].kind === 'photo'
  return (
    <div className={`tg-msg${hasKb ? ' with-kb' : ''}`}>
      <div className={`tg-bubble${onlyMedia ? ' only-media' : ''}`}>
        <Body blocks={view.blocks} mode={mode} editing={editing} onPress={onPress} time={time} />
        {hasKb ? null : <svg className="tg-tail" viewBox="0 0 11 20" aria-hidden><path d="M11 0H6v11c0 4.5-2.2 7.6-6 9h11z" /></svg>}
      </div>
      {hasKb ? <Keyboard rows={view.keyboard} editing={editing} onPress={onPress} /> : null}
      {footer}
    </div>
  )
})
