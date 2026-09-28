package telegram

import "testing"

func TestToHTML(t *testing.T) {
	cases := map[string]string{
		"**Главное меню**\n\nАктивных: 2":      "<b>Главное меню</b>\n\nАктивных: 2",
		"_курсив_ и *тоже* и __подчёркнуто__":  "<i>курсив</i> и <i>тоже</i> и <u>подчёркнуто</u>",
		"a < b & `x<y` ~~old~~ ||secret||":     "a &lt; b &amp; <code>x&lt;y</code> <s>old</s> <tg-spoiler>secret</tg-spoiler>",
		"[сайт](https://example.com/?a=1&b=2)": `<a href="https://example.com/?a=1&amp;b=2">сайт</a>`,
		"[плохо](javascript:alert(1))":         "плохо)",
		"## Заголовок\n- один\n- два":          "<b>Заголовок</b>\n• один\n• два",
		"> цитата\n> ещё\nтекст":               "<blockquote>цитата\nещё</blockquote>\nтекст",
		"```\n<b>не тег</b>\n```":              "<pre>&lt;b&gt;не тег&lt;/b&gt;</pre>",
		"snake_case_name и 2*3*4":              "snake_case_name и 2*3*4",
		"• Срок: 30 дней":                      "• Срок: 30 дней",
	}
	for in, want := range cases {
		if got := toHTML(in); got != want {
			t.Errorf("toHTML(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
