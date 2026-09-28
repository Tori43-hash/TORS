//go:build js && wasm

// Command ui-wasm exposes the theme package to the screen editor: the editor
// validates and renders screens with exactly the code the bot runs.
package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"syscall/js"

	"github.com/tori43-hash/tors/modules/telegram/theme"
	"github.com/tori43-hash/tors/modules/telegram/theme/standard"
)

var (
	current  *theme.Theme
	manifest *theme.Manifest
)

type loadResult struct {
	OK       bool                `json:"ok"`
	Error    string              `json:"error,omitzero"`
	Issues   []theme.Issue       `json:"issues"`
	Incoming map[string][]string `json:"incoming"`
}

// load parses a theme and a manifest, keeps them for render and validates.
func load(themeJSON, manifestJSON string) any {
	m, err := theme.ParseManifest([]byte(manifestJSON))
	if err != nil {
		return result(loadResult{Error: "Манифест: " + err.Error(), Issues: []theme.Issue{}})
	}
	t, err := theme.Parse([]byte(themeJSON))
	if err != nil {
		return result(loadResult{Error: "Тема: " + err.Error(), Issues: []theme.Issue{}})
	}
	current, manifest = t, m
	issues := theme.Validate(t, m)
	if issues == nil {
		issues = []theme.Issue{}
	}
	return result(loadResult{OK: true, Issues: issues, Incoming: theme.IncomingParams(t, m)})
}

func render(id, optionsJSON string) any {
	if current == nil {
		return result(map[string]string{"error": "тема не загружена"})
	}
	var opt theme.RenderOptions
	if err := json.Unmarshal([]byte(optionsJSON), &opt); err != nil {
		return result(map[string]string{"error": err.Error()})
	}
	r, err := theme.Render(current, manifest, id, opt)
	if err != nil {
		return result(map[string]string{"error": err.Error()})
	}
	return result(r)
}

// format returns the theme as canonical, indented JSON for export.
func format(themeJSON string) any {
	t, err := theme.Parse([]byte(themeJSON))
	if err != nil {
		return result(map[string]string{"error": err.Error()})
	}
	b, err := json.Marshal(t, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return result(map[string]string{"error": err.Error()})
	}
	return result(map[string]string{"json": string(b)})
}

func result(v any) any {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return `{"error":` + jsontext.Value(`"`+err.Error()+`"`).String() + `}`
	}
	return string(b)
}

func fn(f func(args []js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return f(args) })
}

func main() {
	js.Global().Set("torsTheme", js.ValueOf(map[string]any{
		"load":   fn(func(a []js.Value) any { return load(a[0].String(), a[1].String()) }),
		"render": fn(func(a []js.Value) any { return render(a[0].String(), a[1].String()) }),
		"format": fn(func(a []js.Value) any { return format(a[0].String()) }),
		"standard": fn(func([]js.Value) any {
			return result(map[string]jsontext.Value{"theme": standard.Theme, "manifest": standard.Manifest})
		}),
	}))
	if cb := js.Global().Get("onTorsThemeReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {}
}
