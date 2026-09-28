import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  ReactFlow,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type FinalConnectionState,
  type Node,
  type NodeChange,
} from '@xyflow/react'
import { useEditor } from '../store'
import { addScreen, locate, parseRef, removeButton, setGoto, setKind, toEntry } from '../ops'
import { buildGraph, layoutMissing } from './graph'
import { nodeTypes } from './nodes'

export function Canvas() {
  const theme = useEditor((s) => s.theme)
  const manifest = useEditor((s) => s.manifest)
  const rendered = useEditor((s) => s.rendered)
  const issues = useEditor((s) => s.issues)
  const positions = useEditor((s) => s.positions)
  const selection = useEditor((s) => s.selection)
  const layoutNonce = useEditor((s) => s.layoutNonce)
  const ready = Object.keys(rendered).length > 0 || Object.keys(theme.screens).length === 0
  const { edit, movePositions, select, notify } = useEditor.getState()
  const flow = useReactFlow()
  const [selectedEdges, setSelectedEdges] = useState<Set<string>>(new Set())
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({})

  const graph = useMemo(() => buildGraph(theme, manifest, rendered, issues), [theme, manifest, rendered, issues])

  useEffect(() => {
    if (!ready) return
    const missing = layoutMissing(graph.nodes, graph.edges, useEditor.getState().positions)
    if (Object.keys(missing).length) movePositions(missing, false)
  }, [graph, ready, layoutNonce, movePositions])

  // Start readable: entries and the first screens at a comfortable zoom,
  // rather than the whole theme shrunk to fit. Runs when a theme or layout arrives.
  useEffect(() => {
    if (!ready) return
    const id = requestAnimationFrame(() => {
      const ps = Object.values(useEditor.getState().positions)
      if (!ps.length) return
      const zoom = 0.72
      const minX = Math.min(...ps.map((p) => p.x))
      const minY = Math.min(...ps.map((p) => p.y))
      flow.setViewport({ x: 48 - minX * zoom, y: 40 - minY * zoom, zoom })
    })
    return () => cancelAnimationFrame(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutNonce, ready, Object.keys(positions).length > 0])

  const nodes: Node[] = useMemo(
    () =>
      graph.nodes.map((n) => {
        const selected =
          (selection?.kind === 'screen' && n.id === `s:${selection.id}`) ||
          (selection?.kind === 'entry' && n.id === `e:${selection.id}`) ||
          (selection?.kind === 'button' && n.type === 'action' && n.data.screen === selection.screen && n.data.ref === selection.ref)
        const selectedRef = selection?.kind === 'button' && n.type === 'screen' && n.data.id === selection.screen ? selection.ref : undefined
        return {
          id: n.id,
          type: n.type,
          position: positions[n.id] ?? { x: 0, y: 0 },
          data: { ...n.data, selected, selectedRef },
          // Only screens and entries delete with Backspace; a selected button is handled below.
          selected: selected && n.type !== 'action',
          measured: measured[n.id],
          style: positions[n.id] ? undefined : { visibility: 'hidden' as const },
        }
      }),
    [graph, positions, selection, measured],
  )

  const edges: Edge[] = useMemo(
    () =>
      graph.edges.map((e) => {
        const selected = selectedEdges.has(e.id)
        const color = selected ? 'var(--text)' : e.kind === 'entry' ? 'var(--text-2)' : 'var(--edge)'
        return {
          id: e.id,
          source: e.source,
          sourceHandle: e.sourceHandle,
          target: e.target,
          targetHandle: 'in',
          type: 'smoothstep',
          pathOptions: { borderRadius: 14 },
          selected,
          style: { stroke: color, strokeWidth: selected ? 2 : 1.4, strokeDasharray: e.dashed ? '5 5' : undefined },
          markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color },
        }
      }),
    [graph, selectedEdges],
  )

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const moved: Record<string, { x: number; y: number }> = {}
      const sized: Record<string, { width: number; height: number }> = {}
      for (const c of changes) {
        if (c.type === 'position' && c.position) moved[c.id] = c.position
        if (c.type === 'dimensions' && c.dimensions) sized[c.id] = c.dimensions
      }
      if (Object.keys(moved).length) movePositions(moved, false)
      if (Object.keys(sized).length) setMeasured((m) => ({ ...m, ...sized }))
    },
    [movePositions],
  )

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    setSelectedEdges((prev) => {
      const next = new Set(prev)
      for (const c of changes) {
        if (c.type === 'select') c.selected ? next.add(c.id) : next.delete(c.id)
        if (c.type === 'remove') next.delete(c.id)
      }
      return next
    })
  }, [])

  /** Wires a connection into the theme: which field changes depends on where it starts. */
  const connect = useCallback(
    (source: string, handle: string, target: string) => {
      if (!target.startsWith('s:')) return
      const to = target.slice(2)
      if (source.startsWith('s:')) {
        const screen = source.slice(2)
        edit((t) => {
          if (handle === 'empty') t.screens[screen].empty = to
          else if (handle !== 'input') {
            const loc = locate(t, screen, handle)
            if (loc) setGoto(loc.button, to)
          }
        })
      } else if (source.startsWith('a:')) {
        const [, screen, ref] = source.split(':')
        edit((t) => {
          const holder = ref === 'input' ? t.screens[screen].input : locate(t, screen, ref)?.button
          if (holder) holder.on = { ...holder.on, [handle]: to }
        })
      } else if (source.startsWith('e:cmd:')) {
        const name = source.slice(6)
        edit((t) => {
          t.commands ??= {}
          if (name === 'start:new') t.commands.start = { ...toEntry(t.commands.start), new_user: to }
          else t.commands[name] = { ...toEntry(t.commands[name]), default: to }
        })
      } else if (source.startsWith('e:ev:')) {
        const name = source.slice(5)
        edit((t) => void ((t.events ??= {})[name] = to))
      }
    },
    [edit],
  )

  const onConnect = useCallback((c: Connection) => connect(c.source, c.sourceHandle ?? '', c.target), [connect])

  /** Dropping a connection anywhere on a screen card connects to it;
   * dropping it on empty canvas creates a screen there. */
  const onConnectEnd = useCallback(
    (event: MouseEvent | TouchEvent, state: FinalConnectionState) => {
      if (state.isValid || !state.fromNode || state.fromHandle?.type !== 'source') return
      const point = 'changedTouches' in event ? event.changedTouches[0] : event
      const hit = document.elementFromPoint(point.clientX, point.clientY)?.closest('.react-flow__node')?.getAttribute('data-id')
      if (hit?.startsWith('s:')) {
        if (hit !== state.fromNode.id || !state.fromNode.id.startsWith('s:')) connect(state.fromNode.id, state.fromHandle.id ?? '', hit)
        return
      }
      if (hit) return
      const pos = flow.screenToFlowPosition({ x: point.clientX, y: point.clientY })
      let id = ''
      edit((t) => void (id = addScreen(t)))
      movePositions({ [`s:${id}`]: { x: pos.x, y: pos.y - 20 } }, false)
      connect(state.fromNode.id, state.fromHandle.id ?? '', `s:${id}`)
      select({ kind: 'screen', id })
    },
    [connect, edit, flow, movePositions, select],
  )

  const onEdgesDelete = useCallback(
    (removed: Edge[]) => {
      edit((t) => {
        for (const e of removed) {
          const [kind, ...rest] = e.id.split(':')
          if (kind === 'g' || kind === 'ga') {
            const loc = locate(t, rest[0], rest.slice(1).join(':'))
            if (loc) setKind(loc.button, '')
          } else if (kind === 'o') {
            const [screen, ref, outcome] = [rest[0], rest.slice(1, -1).join(':'), rest.at(-1)!]
            const holder = ref === 'input' ? t.screens[screen].input : locate(t, screen, ref)?.button
            if (holder?.on) delete holder.on[outcome]
          } else if (kind === 'em') delete t.screens[rest[0]].empty
          else if (kind === 'ev') ((t.events ??= {})[rest[0]] = '')
          else if (kind === 'en') {
            const name = rest.join(':')
            if (name === 'start:new') t.commands!.start = { default: toEntry(t.commands!.start).default }
            else t.commands![name] = { ...toEntry(t.commands![name]), default: '' }
          }
        }
      })
    },
    [edit],
  )

  const onNodesDelete = useCallback(
    (removed: Node[]) => {
      let screens = 0
      edit((t) => {
        for (const n of removed) {
          if (n.id.startsWith('s:')) {
            delete t.screens[n.id.slice(2)]
            screens++
          } else if (n.id.startsWith('a:')) {
            const [, screen, ref] = n.id.split(':')
            if (ref === 'input') delete t.screens[screen].input
            else {
              const loc = locate(t, screen, ref)
              if (loc) setKind(loc.button, '')
            }
          } else if (n.id.startsWith('e:cmd:')) {
            const name = n.id.slice(6)
            if (name === 'start:new') t.commands!.start = { default: toEntry(t.commands!.start).default }
            else if (name !== 'start') delete t.commands![name]
          }
        }
      })
      select(null)
      if (screens) notify(screens === 1 ? 'Экран удалён' : `Удалено экранов: ${screens}`, true)
    },
    [edit, notify, select],
  )

  /** Backspace on a selected button removes it (screens and edges are handled by React Flow). */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Backspace' && e.key !== 'Delete') return
      const el = e.target as HTMLElement
      if (el.closest('input, textarea, select, [contenteditable]')) return
      const sel = useEditor.getState().selection
      if (sel?.kind !== 'button') return
      const r = parseRef(sel.ref)
      if (!r || r.area === 'frag' || r.area === 'pager') return
      e.preventDefault()
      edit((t) => void removeButton(t, sel.screen, sel.ref))
      select({ kind: 'screen', id: sel.screen })
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [edit, select])

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      onConnectEnd={onConnectEnd}
      onEdgesDelete={onEdgesDelete}
      onNodesDelete={onNodesDelete}
      onNodeDragStart={() => movePositions({}, true)}
      onNodeClick={(_, n) => {
        if (n.id.startsWith('s:')) select({ kind: 'screen', id: n.id.slice(2) })
        else if (n.id.startsWith('e:')) select({ kind: 'entry', id: n.id.slice(2) })
        else if (n.id.startsWith('a:')) {
          const [, screen, ref] = n.id.split(':')
          select(ref === 'input' ? { kind: 'screen', id: screen } : { kind: 'button', screen, ref })
        }
      }}
      onPaneClick={() => select(null)}
      deleteKeyCode={['Backspace', 'Delete']}
      minZoom={0.15}
      maxZoom={1.6}
      onlyRenderVisibleElements
      connectionRadius={28}
      defaultEdgeOptions={{ type: 'smoothstep' }}
    >
      <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="var(--dots)" bgColor="var(--canvas)" />
      <Controls showInteractive={false} position="bottom-right" />
    </ReactFlow>
  )
}
