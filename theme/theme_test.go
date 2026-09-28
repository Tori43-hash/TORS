package theme_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/tori43-hash/tors/theme"
	"github.com/tori43-hash/tors/theme/standard"
)

func load(t *testing.T) (*theme.Theme, *theme.Manifest) {
	t.Helper()
	th, err := theme.Parse(standard.Theme)
	if err != nil {
		t.Fatalf("parse theme: %v", err)
	}
	m, err := theme.ParseManifest(standard.Manifest)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return th, m
}

func TestStandardThemeIsValid(t *testing.T) {
	th, m := load(t)
	for _, is := range theme.Validate(th, m) {
		t.Errorf("%s %s %s: %s", is.Level, is.Screen, is.Ref, is.Message)
	}
}

func TestRoundTrip(t *testing.T) {
	th, _ := load(t)
	b, err := json.Marshal(th, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	again, err := theme.Parse(b)
	if err != nil {
		t.Fatalf("reparse: %v\n%s", err, b)
	}
	b2, _ := json.Marshal(again, json.Deterministic(true))
	if string(b) != string(b2) {
		t.Fatalf("round trip changed the theme")
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	cases := map[string]string{
		"screen":      `{"version":1,"screens":{"a":{"blok":[]}}}`,
		"button":      `{"version":1,"screens":{"a":{"keyboard":[[{"txt":{"ru":"x"},"back":true}]]}}}`,
		"repeat":      `{"version":1,"screens":{"a":{"keyboard":[{"repeat":".X","colums":2,"button":{"back":true}}]}}}`,
		"row object":  `{"version":1,"screens":{"a":{"keyboard":[{"foo":1}]}}}`,
		"row literal": `{"version":1,"screens":{"a":{"keyboard":[1]}}}`,
	}
	for name, src := range cases {
		if _, err := theme.Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestValidateFindsProblems(t *testing.T) {
	_, m := load(t)
	src := `{
	  "version": 1,
	  "commands": {"start": "home", "Bad": "home"},
	  "screens": {
	    "home": {
	      "blocks": [{"text": {"ru": "{{ .User.Nope }}"}}],
	      "keyboard": [
	        [{"text": {"ru": "a"}, "goto": "missing"}],
	        [{"text": {"ru": "b"}}],
	        [{"text": {"ru": "c"}, "goto": "card"}],
	        [{"text": {"ru": "d"}, "action": "trial.start", "on": {"ok": "home", "nope": "home"}}],
	        [{"text": {"ru": "e"}, "back": true, "style": "link"}],
	        [{"text": {"ru": "f"}, "url": "ftp://x"}]
	      ]
	    },
	    "card": {"data": {"Sub": "subscriptions.get"}, "blocks": [{"text": {"ru": "x", "en": "x"}}], "keyboard": [[{"text": {"ru": "x", "en": "x"}, "back": true}]]},
	    "lonely": {"blocks": [{"text": {"ru": "x", "en": "x"}}]}
	  }
	}`
	th, err := theme.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, is := range theme.Validate(th, m) {
		msgs = append(msgs, is.Level+": "+is.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{
		"Нет такой переменной: .User.Nope",
		"Кнопка ведёт на экран «missing», которого нет",
		"Кнопка никуда не ведёт",
		"нужен параметр id",
		"У действия trial.start нет исхода nope",
		"Недопустимый цвет кнопки: link",
		"Ссылка должна начинаться",
		"Команда /Bad",
		"warning: Экран недостижим",
		"warning: Нет перевода: en",
		"Исход «Уже был» действия trial.start ведёт на экран «trial_used», которого нет",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing issue %q in:\n%s", want, all)
		}
	}
}

func TestRender(t *testing.T) {
	th, m := load(t)
	r, err := theme.Render(th, m, "my_subs", theme.RenderOptions{Lang: "ru"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Errors) > 0 {
		t.Fatalf("render errors: %v", r.Errors)
	}
	if got := r.Keyboard[0][0].Label; got != "Телефон · до 28.10.2026" {
		t.Errorf("first repeat label = %q", got)
	}
	if got := r.Keyboard[0][0].Params["id"]; got != "17" {
		t.Errorf("first repeat param id = %q", got)
	}
	if r.Keyboard[3][0].Label != "➕ Новая подписка" || r.Keyboard[4][0].Label != "‹ Назад" {
		t.Errorf("unexpected keyboard: %+v", r.Keyboard)
	}

	r, _ = theme.Render(th, m, "sub_card", theme.RenderOptions{Lang: "ru", Params: map[string]string{"id": "17"}})
	if !strings.Contains(r.Blocks[0].Text, "12,5 ГБ из 200 ГБ") {
		t.Errorf("bytes formatting: %q", r.Blocks[0].Text)
	}

	r, _ = theme.Render(th, m, "welcome", theme.RenderOptions{Lang: "en", Conditions: map[string]bool{"trial.available": false}})
	if r.Keyboard[0][0].Label != "🛒 Plans" {
		t.Errorf("hidden trial button still shown: %+v", r.Keyboard[0])
	}

	r, _ = theme.Render(th, m, "expiring_notice", theme.RenderOptions{Lang: "ru"})
	if !strings.Contains(r.Blocks[0].Text, "через 3 дня") {
		t.Errorf("plural: %q", r.Blocks[0].Text)
	}
}

func TestRenderPagesAndEmpty(t *testing.T) {
	th, m := load(t)
	th.Screens["my_subs"].Keyboard[0].Repeat.PageSize = 2
	r, _ := theme.Render(th, m, "my_subs", theme.RenderOptions{Lang: "ru", Pages: map[string]int{"k.0": 1}})
	labels := func(row []theme.RenderedButton) (out []string) {
		for _, b := range row {
			out = append(out, b.Label)
		}
		return out
	}
	if got := labels(r.Keyboard[0]); !slices.Equal(got, []string{"Роутер · до 01.09.2026"}) {
		t.Errorf("page 2 items = %v", got)
	}
	if got := labels(r.Keyboard[1]); !slices.Equal(got, []string{"‹", "2/2", "›"}) {
		t.Errorf("pager = %v", got)
	}

	data := theme.SampleData(th, m, "my_subs", nil)
	data["Subs"] = []any{}
	r, _ = theme.Render(th, m, "my_subs", theme.RenderOptions{Lang: "ru", Data: data})
	if r.Redirect != "no_subs" {
		t.Errorf("empty list redirect = %q", r.Redirect)
	}
}

func TestResolveOutcomes(t *testing.T) {
	th, m := load(t)
	th.Routes = map[string]map[string]string{"trial.start": {"already_used": "plans"}}
	got := th.ResolveOutcomes(m, "trial.start", map[string]string{"ok": "my_subs"})
	want := map[string]string{"ok": "my_subs", "already_used": "plans", "need_channel": "join_channel"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s → %q, want %q", k, got[k], v)
		}
	}
	if got := th.ResolveOutcomes(m, "billing.checkout", nil)["invoice"]; got != "" {
		t.Errorf("terminal outcome → %q", got)
	}
}
