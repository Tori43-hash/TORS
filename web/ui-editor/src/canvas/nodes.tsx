import { memo } from 'react'
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import { TgMessage } from '../tg/Message'
import { useEditor } from '../store'
import type { ActionData, EntryData, ScreenData } from './graph'

type ScreenNodeT = Node<ScreenData & { selected: boolean; selectedRef?: string }, 'screen'>
type ActionNodeT = Node<ActionData & { selected: boolean }, 'action'>
type EntryNodeT = Node<EntryData & { selected: boolean }, 'entry'>

export const ScreenNode = memo(function ScreenNode({ data }: NodeProps<ScreenNodeT>) {
  const tgDark = useEditor((s) => s.tgDark)
  const renderer = useEditor((s) => s.renderer)
  const select = useEditor((s) => s.select)
  const { id, r, errors, warnings, input, list, selected, selectedRef } = data
  return (
    <div className={`node-screen${selected ? ' is-selected' : ''}`}>
      <Handle type="target" id="in" position={Position.Left} className="node-in" />
      <div className="node-head">
        <span className="node-title">{id}</span>
        <span className="node-badges">
          {errors > 0 ? <span className="badge badge-error">{errors}</span> : warnings > 0 ? <span className="badge badge-warn">{warnings}</span> : null}
        </span>
      </div>
      <div className={`tg tg-wall node-chat${tgDark ? ' is-dark' : ''}`}>
        {r ? (
          <TgMessage
            r={r}
            mode={renderer}
            editing={{ selectedRef, handles: true, onSelect: (ref) => select({ kind: 'button', screen: id, ref }) }}
          />
        ) : (
          <div className="node-skeleton" />
        )}
      </div>
      {input || list ? (
        <div className="node-foot">
          {input ? (
            <span className="node-port">
              Ждёт ввод текста
              <Handle type="source" id="input" position={Position.Right} className="node-port-handle" />
            </span>
          ) : null}
          {list ? (
            <span className="node-port">
              Если список пуст
              <Handle type="source" id="empty" position={Position.Right} className="node-port-handle" />
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  )
})

export const ActionNode = memo(function ActionNode({ data }: NodeProps<ActionNodeT>) {
  return (
    <div className={`node-action${data.selected ? ' is-selected' : ''}`}>
      <Handle type="target" id="in" position={Position.Left} className="node-in" />
      <div className="node-action-head">
        <svg viewBox="0 0 16 16" aria-hidden>
          <path d="M3 8h8M8 4.5 11.5 8 8 11.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span title={data.action}>{data.title}</span>
      </div>
      {data.outcomes.map((o) => (
        <div key={o.name} className={`node-outcome${o.overridden ? ' is-own' : ''}`}>
          <span>{o.title}</span>
          {o.terminal ? (
            <span className="node-terminal">без экрана</span>
          ) : (
            <Handle type="source" id={o.name} position={Position.Right} className="node-port-handle" />
          )}
        </div>
      ))}
    </div>
  )
})

export const EntryNode = memo(function EntryNode({ data }: NodeProps<EntryNodeT>) {
  return (
    <div className={`node-entry kind-${data.kind}${data.selected ? ' is-selected' : ''}`}>
      <div className="node-entry-label">{data.label}</div>
      <div className="node-entry-sub">{data.sub}</div>
      <Handle type="source" id="out" position={Position.Right} className="node-port-handle" />
    </div>
  )
})

export const nodeTypes = { screen: ScreenNode, action: ActionNode, entry: EntryNode }
