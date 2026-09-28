// A small, safe Markdown renderer for previews. Everything is HTML-escaped
// first, then a fixed set of Telegram-style markup is applied — themes are
// shared between people, so no raw HTML ever reaches the page.

const escapeHtml = (s: string) =>
  s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')

const safeUrl = (u: string) => (/^(https?:|tg:)/i.test(u) ? u : '#')

function inline(src: string): string {
  const slots: string[] = []
  const keep = (html: string) => `\u0000${slots.push(html) - 1}\u0000`
  let s = escapeHtml(src)
  s = s.replace(/`([^`\n]+)`/g, (_, c) => keep(`<code>${c}</code>`))
  s = s.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (_, t, u) =>
    keep(`<a href="${safeUrl(u.replace(/&amp;/g, '&'))}" target="_blank" rel="noopener noreferrer">${t}</a>`),
  )
  s = s.replace(/\*\*([^*\n]+)\*\*/g, '<b>$1</b>')
  s = s.replace(/__([^_\n]+)__/g, '<u>$1</u>')
  s = s.replace(/(^|[^\w*])\*([^*\n]+)\*(?!\w)/g, '$1<i>$2</i>')
  s = s.replace(/(^|[^\w_])_([^_\n]+)_(?!\w)/g, '$1<i>$2</i>')
  s = s.replace(/~~([^~\n]+)~~/g, '<s>$1</s>')
  s = s.replace(/\|\|([^|\n]+)\|\|/g, '<span class="tg-spoiler">$1</span>')
  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => slots[Number(i)])
}

export interface MdOptions {
  /** Classic messages have no headings: they degrade to bold lines. */
  headings: boolean
}

export function markdown(src: string, opt: MdOptions = { headings: true }): string {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const out: string[] = []
  let para: string[] = []
  let list: { ordered: boolean; items: string[] } | null = null
  let quote: string[] = []

  const flushPara = () => {
    if (para.length) out.push(`<p>${para.map(inline).join('<br>')}</p>`)
    para = []
  }
  const flushList = () => {
    if (list) {
      const tag = list.ordered ? 'ol' : 'ul'
      out.push(`<${tag}>${list.items.map((i) => `<li>${inline(i)}</li>`).join('')}</${tag}>`)
    }
    list = null
  }
  const flushQuote = () => {
    if (quote.length) out.push(`<blockquote>${quote.map(inline).join('<br>')}</blockquote>`)
    quote = []
  }
  const flush = () => (flushPara(), flushList(), flushQuote())

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.startsWith('```')) {
      flush()
      const code: string[] = []
      while (++i < lines.length && !lines[i].startsWith('```')) code.push(lines[i])
      out.push(`<pre>${escapeHtml(code.join('\n'))}</pre>`)
      continue
    }
    const h = /^(#{1,3})\s+(.*)$/.exec(line)
    if (h) {
      flush()
      out.push(opt.headings ? `<h${h[1].length + 2}>${inline(h[2])}</h${h[1].length + 2}>` : `<p><b>${inline(h[2])}</b></p>`)
      continue
    }
    const q = /^>\s?(.*)$/.exec(line)
    if (q) {
      flushPara(), flushList()
      quote.push(q[1])
      continue
    }
    const li = /^\s*(?:([-*•])|(\d+)[.)])\s+(.*)$/.exec(line)
    if (li && !(li[1] === '•')) {
      flushPara(), flushQuote()
      const ordered = !!li[2]
      if (!list || list.ordered !== ordered) flushList(), (list = { ordered, items: [] })
      list.items.push(li[3])
      continue
    }
    if (line.trim() === '') {
      flush()
      continue
    }
    flushList(), flushQuote()
    para.push(line)
  }
  flush()
  return out.join('')
}
