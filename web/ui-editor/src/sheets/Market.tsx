import { useRef, useState } from 'react'
import { useEditor } from '../store'
import { CATEGORY, TRUST } from '../market'
import type { MarketModule, Pack } from '../types'
import { Segmented } from '../inspector/fields'
import { Sheet } from './Sheet'

type Tab = 'official' | 'verified' | 'own'

const ORDER = ['sales', 'panel', 'payment', 'feature']

function Row({ m }: { m: MarketModule }) {
  const all = useEditor((s) => s.all)
  const inBot = useEditor((s) => s.resolved.set.has(m.id))
  const added = useEditor((s) => !!s.modules[m.id]?.added)
  const st = useEditor.getState()
  const requires = (m.requires ?? []).map((r) => all.get(r)).filter((r) => r && !r.hidden).map((r) => r!.name)
  return (
    <div className="mrow">
      <div className="mrow-main">
        <div className="mrow-title">
          {m.name}
          {m.trust === 'own' ? <span className="tag">не проверен</span> : m.trust === 'verified' ? <span className="tag">проверен</span> : null}
        </div>
        <div className="mrow-summary">{m.summary}</div>
        {requires.length ? <div className="mrow-meta">Нужны: {requires.join(', ')}</div> : null}
        {m.version ? <div className="mrow-meta mono">{m.package}@{m.version}</div> : null}
      </div>
      <div className="mrow-side">
        {added || inBot ? (
          <button type="button" className="btn" onClick={() => { st.select({ kind: 'module', id: m.id }); st.setSheet('') }}>
            {added ? 'Настроить' : 'Уже в боте'}
          </button>
        ) : (
          <button type="button" className="btn btn-primary" onClick={() => { st.addModule(m.id); st.select({ kind: 'module', id: m.id }) }}>
            Добавить
          </button>
        )}
        {m.trust === 'own' ? (
          <button type="button" className="link-btn" onClick={() => st.removeOwn(m.id)}>Убрать из списка</button>
        ) : null}
      </div>
    </div>
  )
}

// Modules that bring others with them come first: they are the starting points.
const weight = (m: MarketModule) => -(m.requires?.length ?? 0)

function List({ modules }: { modules: MarketModule[] }) {
  const sorted = [...modules].sort((a, b) => weight(a) - weight(b) || a.name.localeCompare(b.name))
  const groups = ORDER.map((c) => [c, sorted.filter((m) => m.category === c)] as const)
  const other = modules.filter((m) => !ORDER.includes(m.category))
  return (
    <>
      {[...groups, ['other', other] as const].filter(([, ms]) => ms.length).map(([c, ms]) => (
        <section key={c} className="mgroup">
          <h3>{CATEGORY[c] ?? 'Другое'}</h3>
          {ms.map((m) => <Row key={m.id} m={m} />)}
        </section>
      ))}
    </>
  )
}

export function MarketSheet() {
  const all = useEditor((s) => s.all)
  const [tab, setTab] = useState<Tab>('official')
  const file = useRef<HTMLInputElement>(null)
  const visible = [...all.values()].filter((m) => !m.hidden && (m.trust ?? 'official') === tab)

  return (
    <Sheet
      title="Модули"
      aside={
        <Segmented<Tab>
          value={tab}
          onChange={setTab}
          options={[
            { value: 'official', label: 'Официальные' },
            { value: 'verified', label: 'Проверенные' },
            { value: 'own', label: 'Свои' },
          ]}
        />
      }
    >
      <p className="sheet-note">{TRUST[tab]}: {tab === 'official'
        ? 'модули из репозитория TORS, собираются вместе с ядром.'
        : tab === 'verified'
          ? 'модули сторонних авторов, код которых прочитан на конкретной версии.'
          : 'модули, описание которых вы открыли сами. Их код никто не проверял.'}</p>

      {tab === 'own' ? (
        <div className="own-import">
          <p>
            Соберите бинарь со своим модулем и выполните <code>tors describe --package ваш/модуль -o tors-module.json</code>.
            Откройте получившийся файл здесь.
          </p>
          <button type="button" className="btn" onClick={() => file.current?.click()}>Открыть tors-module.json</button>
          <input
            ref={file}
            type="file"
            accept="application/json,.json"
            hidden
            onChange={async (e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (!f) return
              const st = useEditor.getState()
              try {
                const pack = JSON.parse(await f.text()) as Pack
                if (!Array.isArray(pack.modules)) throw new Error()
                st.notify(st.addOwn(pack.modules))
              } catch {
                st.notify('Это не tors-module.json')
              }
            }}
          />
        </div>
      ) : null}

      {visible.length ? (
        <List modules={visible} />
      ) : tab === 'verified' ? (
        <p className="sheet-empty">Проверенных модулей сторонних авторов пока нет. Как предложить свой — в registry/README.md.</p>
      ) : tab === 'own' ? null : (
        <p className="sheet-empty">Модулей нет.</p>
      )}
    </Sheet>
  )
}
