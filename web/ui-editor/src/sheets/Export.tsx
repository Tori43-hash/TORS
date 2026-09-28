import { useMemo } from 'react'
import { exportable, useEditor } from '../store'
import { envVars, exportBot, missing } from '../market'
import { Sheet } from './Sheet'

export function ExportSheet() {
  const theme = useEditor((s) => s.theme)
  const positions = useEditor((s) => s.positions)
  const modules = useEditor((s) => s.modules)
  const all = useEditor((s) => s.all)
  const resolved = useEditor((s) => s.resolved)
  const issues = useEditor((s) => s.issues)
  const st = useEditor.getState()

  const bot = useMemo(() => exportBot(exportable({ theme, positions }), modules, all), [theme, positions, modules, all])
  const json = useMemo(() => JSON.stringify(bot, null, 2) + '\n', [bot])
  const env = envVars(bot)

  const problems: { text: string; go?: () => void }[] = []
  const errors = issues.filter((i) => i.level === 'error').length
  if (errors) problems.push({ text: `В экранах ошибок: ${errors}. Бот не запустится, пока их не исправить.` })
  for (const n of resolved.needs) {
    problems.push({ text: `${all.get(n.by)?.name ?? n.by}: выберите ${n.namespace === 'panels.providers' ? 'панель' : n.namespace === 'billing.gateways' ? 'способ оплаты' : 'модуль'} — ${n.options.map((o) => o.name).join(' или ')}.` })
  }
  for (const id of resolved.set) {
    const m = all.get(id)!
    const miss = missing(m, modules[id]?.config)
    if (miss.length) problems.push({ text: `${m.name}: заполните ${miss.join(', ')}.`, go: () => { st.select({ kind: 'module', id }); st.setSheet('') } })
  }

  const download = () => {
    const url = URL.createObjectURL(new Blob([json], { type: 'application/json' }))
    const a = Object.assign(document.createElement('a'), { href: url, download: 'bot.json' })
    a.click()
    URL.revokeObjectURL(url)
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(json)
      st.notify('bot.json скопирован')
    } catch {
      st.notify('Браузер не дал доступ к буферу обмена')
    }
  }

  return (
    <Sheet
      title="Экспорт"
      wide
      aside={
        <>
          <button type="button" className="btn" onClick={copy}>Скопировать</button>
          <button type="button" className="btn btn-primary" onClick={download}>Скачать bot.json</button>
        </>
      }
    >
      {problems.length ? (
        <section className="export-block">
          <h3>Перед запуском</h3>
          <ul className="problems">
            {problems.map((p, i) => (
              <li key={i}>{p.go ? <button type="button" className="link-btn" onClick={p.go}>{p.text}</button> : p.text}</li>
            ))}
          </ul>
        </section>
      ) : (
        <section className="export-block">
          <h3>Готово к запуску</h3>
          <p className="sheet-note">Ошибок нет, все модули настроены.</p>
        </section>
      )}

      <section className="export-block">
        <h3>Как запустить</h3>
        <ol className="steps">
          <li>
            Соберите бинарь с модулями этого бота:
            <pre>tors build --config bot.json -o bot</pre>
          </li>
          <li>
            Задайте переменные окружения{env.length ? ':' : '.'}
            {env.length ? <pre>{env.map((e) => `export ${e}=…`).join('\n')}</pre> : null}
          </li>
          <li>
            Запустите:
            <pre>./bot run --config bot.json</pre>
          </li>
        </ol>
      </section>

      <section className="export-block">
        <h3>Модули в сборке · {bot.build.modules.length}</h3>
        <p className="mono small">{bot.build.modules.map((m) => m.source.replace('github.com/tori43-hash/tors/modules/', '') + (m.version ? `@${m.version}` : '')).join(' · ')}</p>
      </section>

      <section className="export-block">
        <h3>bot.json</h3>
        <pre className="json">{json}</pre>
      </section>
    </Sheet>
  )
}
