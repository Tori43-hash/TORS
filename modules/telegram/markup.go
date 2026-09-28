package telegram

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Markdown in themes follows the editor preview: **bold**, _italic_ or *italic*,
// __underline__, ~~strike~~, `code`, ```blocks```, [links](https://…),
// ||spoilers||, "> quotes", "# headings" and "- lists". toHTML turns it into
// the HTML subset Telegram accepts.

var (
	reCode      = regexp.MustCompile("`([^`\n]+)`")
	reLink      = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	reBold      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reUnderline = regexp.MustCompile(`__([^_\n]+)__`)
	reItalicA   = regexp.MustCompile(`(^|[^\p{L}\p{N}_*])\*([^*\n]+)\*($|[^\p{L}\p{N}_*])`)
	reItalicU   = regexp.MustCompile(`(^|[^\p{L}\p{N}_])_([^_\n]+)_($|[^\p{L}\p{N}_])`)
	reStrike    = regexp.MustCompile(`~~([^~\n]+)~~`)
	reSpoiler   = regexp.MustCompile(`\|\|([^|\n]+)\|\|`)
	reSlot      = regexp.MustCompile("\x00(\\d+)\x00")
	reHeading   = regexp.MustCompile(`^#{1,3}\s+(.*)$`)
	reQuote     = regexp.MustCompile(`^>\s?(.*)$`)
	reBullet    = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)
)

func inline(src string) string {
	var slots []string
	keep := func(s string) string {
		slots = append(slots, s)
		return fmt.Sprintf("\x00%d\x00", len(slots)-1)
	}
	s := html.EscapeString(src)
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		return keep("<code>" + reCode.FindStringSubmatch(m)[1] + "</code>")
	})
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		p := reLink.FindStringSubmatch(m)
		u := html.UnescapeString(p[2])
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "tg://") {
			return p[1]
		}
		return keep(`<a href="` + html.EscapeString(u) + `">` + p[1] + "</a>")
	})
	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	s = reUnderline.ReplaceAllString(s, "<u>$1</u>")
	s = reItalicA.ReplaceAllString(s, "$1<i>$2</i>$3")
	s = reItalicU.ReplaceAllString(s, "$1<i>$2</i>$3")
	s = reStrike.ReplaceAllString(s, "<s>$1</s>")
	s = reSpoiler.ReplaceAllString(s, "<tg-spoiler>$1</tg-spoiler>")
	return reSlot.ReplaceAllStringFunc(s, func(m string) string {
		var i int
		fmt.Sscanf(reSlot.FindStringSubmatch(m)[1], "%d", &i)
		return slots[i]
	})
}

// toHTML converts theme Markdown to Telegram HTML.
func toHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []string
	var quote []string
	flushQuote := func() {
		if len(quote) > 0 {
			out = append(out, "<blockquote>"+strings.Join(quote, "\n")+"</blockquote>")
			quote = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			flushQuote()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
				code = append(code, lines[i])
			}
			out = append(out, "<pre>"+html.EscapeString(strings.Join(code, "\n"))+"</pre>")
			continue
		}
		if m := reQuote.FindStringSubmatch(line); m != nil {
			quote = append(quote, inline(m[1]))
			continue
		}
		flushQuote()
		switch {
		case reHeading.MatchString(line):
			out = append(out, "<b>"+inline(reHeading.FindStringSubmatch(line)[1])+"</b>")
		case reBullet.MatchString(line):
			out = append(out, "• "+inline(reBullet.FindStringSubmatch(line)[1]))
		default:
			out = append(out, inline(line))
		}
	}
	flushQuote()
	return strings.TrimSpace(strings.Join(out, "\n"))
}
