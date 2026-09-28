package theme

import (
	"fmt"
	"strconv"
	"strings"
)

// RenderOptions controls how a screen is rendered.
type RenderOptions struct {
	Lang       string
	Params     map[string]string
	Conditions map[string]bool // missing conditions use manifest samples
	Pages      map[string]int  // repeat row ref → zero-based page
	ShowHidden bool            // keep hidden buttons and mark them (editor canvas)
	Data       map[string]any  // template data; nil means manifest samples
}

// Rendered is a screen ready to be drawn: templates executed, conditions
// applied, repeats expanded and action outcomes resolved.
type Rendered struct {
	Screen   string             `json:"screen"`
	Blocks   []RenderedBlock    `json:"blocks"`
	Keyboard [][]RenderedButton `json:"keyboard"`
	Input    *Input             `json:"input,omitzero"`
	Redirect string             `json:"redirect,omitzero"`
	Errors   []Issue            `json:"errors,omitzero"`
}

type RenderedBlock struct {
	Kind    string           `json:"kind"` // photo | text | buttons
	Photo   string           `json:"photo,omitzero"`
	Text    string           `json:"text,omitzero"`
	Align   string           `json:"align,omitzero"`
	Buttons []RenderedButton `json:"buttons,omitzero"`
}

type RenderedButton struct {
	Ref      string            `json:"ref"`
	Label    string            `json:"label"`
	Icon     string            `json:"icon,omitzero"`
	Style    string            `json:"style,omitzero"`
	Kind     string            `json:"kind"`
	Target   string            `json:"target,omitzero"`
	Params   map[string]string `json:"params,omitzero"`
	Outcomes map[string]string `json:"outcomes,omitzero"`
	Disabled bool              `json:"disabled,omitzero"`
	Hidden   bool              `json:"hidden,omitzero"`
	Repeat   bool              `json:"repeat,omitzero"`
}

// Kinds of generated pager buttons.
const KindNoop = "noop"

// Render renders one screen of the theme.
func Render(t *Theme, m *Manifest, id string, opt RenderOptions) (*Rendered, error) {
	s := t.Screens[id]
	if s == nil {
		return nil, fmt.Errorf("screen %q not found", id)
	}
	if opt.Data == nil {
		opt.Data = SampleData(t, m, id, opt.Params)
	}
	r := &renderer{t: t, m: m, opt: opt, out: &Rendered{Screen: id, Input: s.Input, Keyboard: [][]RenderedButton{}, Blocks: []RenderedBlock{}}}
	for i, b := range s.Blocks {
		ref := "b." + strconv.Itoa(i)
		switch {
		case b.Photo != "":
			r.out.Blocks = append(r.out.Blocks, RenderedBlock{Kind: "photo", Photo: b.Photo})
		case len(b.Text) > 0:
			r.out.Blocks = append(r.out.Blocks, RenderedBlock{Kind: "text", Text: r.text(b.Text, opt.Data, ref)})
		case b.Buttons != nil:
			var items []RenderedButton
			for j := range b.Buttons.Items {
				if rb, ok := r.button(&b.Buttons.Items[j], opt.Data, ref+"."+strconv.Itoa(j)); ok {
					items = append(items, rb)
				}
			}
			if len(items) > 0 {
				r.out.Blocks = append(r.out.Blocks, RenderedBlock{Kind: "buttons", Align: b.Buttons.Align, Buttons: items})
			}
		}
	}
	for i, row := range s.Keyboard {
		ref := "k." + strconv.Itoa(i)
		switch {
		case row.Repeat != nil:
			if redirect := r.repeat(row.Repeat, ref, s.Empty); redirect != "" {
				r.out.Redirect = redirect
				return r.out, nil
			}
		case row.Use != "":
			for fi, frow := range t.Fragments[row.Use] {
				r.row(frow.Buttons, fmt.Sprintf("%s.f%d", ref, fi))
			}
		default:
			r.row(row.Buttons, ref)
		}
	}
	return r.out, nil
}

type renderer struct {
	t   *Theme
	m   *Manifest
	opt RenderOptions
	out *Rendered
}

func (r *renderer) row(buttons []Button, ref string) {
	var out []RenderedButton
	for j := range buttons {
		if rb, ok := r.button(&buttons[j], r.opt.Data, ref+"."+strconv.Itoa(j)); ok {
			out = append(out, rb)
		}
	}
	if len(out) > 0 {
		r.out.Keyboard = append(r.out.Keyboard, out)
	}
}

func (r *renderer) repeat(rp *Repeat, ref, empty string) (redirect string) {
	items, _ := r.opt.Data[strings.TrimPrefix(rp.Source, ".")].([]any)
	if len(items) == 0 {
		return empty
	}
	cols := max(rp.Columns, 1)
	page, pages := 0, 1
	if rp.PageSize > 0 && len(items) > rp.PageSize {
		pages = (len(items) + rp.PageSize - 1) / rp.PageSize
		page = min(max(r.opt.Pages[ref], 0), pages-1)
		items = items[page*rp.PageSize : min((page+1)*rp.PageSize, len(items))]
	}
	var line []RenderedButton
	for _, item := range items {
		rb, ok := r.button(&rp.Button, item, ref+".r")
		if !ok {
			continue
		}
		rb.Repeat = true
		line = append(line, rb)
		if len(line) == cols {
			r.out.Keyboard = append(r.out.Keyboard, line)
			line = nil
		}
	}
	if len(line) > 0 {
		r.out.Keyboard = append(r.out.Keyboard, line)
	}
	if pages > 1 {
		pager := ref + ".p"
		r.out.Keyboard = append(r.out.Keyboard, []RenderedButton{
			{Ref: pager, Label: "‹", Kind: KindPage, Target: strconv.Itoa((page - 1 + pages) % pages)},
			{Ref: pager, Label: fmt.Sprintf("%d/%d", page+1, pages), Kind: KindNoop},
			{Ref: pager, Label: "›", Kind: KindPage, Target: strconv.Itoa((page + 1) % pages)},
		})
	}
	return ""
}

func (r *renderer) button(b *Button, data any, ref string) (RenderedButton, bool) {
	hidden := b.VisibleIf != "" && !r.cond(b.VisibleIf)
	if hidden && !r.opt.ShowHidden {
		return RenderedButton{}, false
	}
	rb := RenderedButton{
		Ref:      ref,
		Label:    r.label(b, data, ref),
		Icon:     b.Icon,
		Style:    b.Style,
		Kind:     b.Kind(),
		Disabled: b.DisabledIf != "" && r.cond(b.DisabledIf),
		Hidden:   hidden,
	}
	switch rb.Kind {
	case KindGoto:
		rb.Target = b.Goto
	case KindAction:
		rb.Target = b.Action
		rb.Outcomes = r.t.ResolveOutcomes(r.m, b.Action, b.On)
	case KindURL:
		rb.Target = r.exec(b.URL, data, ref)
	case KindWebApp:
		rb.Target = r.exec(b.WebApp, data, ref)
	case KindCopy:
		rb.Target = r.exec(b.CopyText, data, ref)
	}
	if len(b.Params) > 0 {
		rb.Params = make(map[string]string, len(b.Params))
		for k, v := range b.Params {
			rb.Params[k] = r.exec(v, data, ref)
		}
	}
	return rb, true
}

func (r *renderer) label(b *Button, data any, ref string) string {
	text := r.pick(b.Text)
	if text == "" && b.Action != "" {
		text = r.pick(r.m.Actions[b.Action].Label)
	}
	text = r.exec(text, data, ref)
	switch {
	case b.Emoji == "":
		return text
	case text == "":
		return b.Emoji
	default:
		return b.Emoji + " " + text
	}
}

func (r *renderer) text(t Text, data any, ref string) string {
	return r.exec(r.pick(t), data, ref)
}

// pick returns the text for the render language, falling back to the
// manifest language order and then to any translation.
func (r *renderer) pick(t Text) string {
	if s, ok := t[r.opt.Lang]; ok {
		return s
	}
	for _, l := range r.m.Languages {
		if s, ok := t[l]; ok {
			return s
		}
	}
	for _, s := range t {
		return s
	}
	return ""
}

func (r *renderer) exec(src string, data any, ref string) string {
	out, err := Execute(src, data, r.opt.Lang)
	if err != nil {
		r.out.Errors = append(r.out.Errors, Issue{Level: LevelError, Screen: r.out.Screen, Ref: ref, Message: templateMessage(err)})
		return src
	}
	return out
}

func (r *renderer) cond(expr string) bool {
	name, neg := strings.CutPrefix(expr, "!")
	v, ok := r.opt.Conditions[name]
	if !ok {
		v = r.m.Conditions[name].Sample
	}
	return v != neg
}

// ResolveOutcomes maps every outcome of an action to a screen: the button's
// own mapping wins, then theme routes, then the manifest default. Terminal
// outcomes map to "".
func (t *Theme) ResolveOutcomes(m *Manifest, action string, on map[string]string) map[string]string {
	a, ok := m.Actions[action]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(a.Outcomes))
	for _, o := range a.Outcomes {
		switch {
		case on[o.Name] != "":
			out[o.Name] = on[o.Name]
		case t.Routes[action][o.Name] != "":
			out[o.Name] = t.Routes[action][o.Name]
		case o.Terminal:
			out[o.Name] = ""
		default:
			out[o.Name] = o.Default
		}
	}
	return out
}

// CommandTarget returns the screen a command opens for a new or returning user.
func (t *Theme) CommandTarget(name string, newUser bool) string {
	e := t.Commands[name]
	if newUser && e.NewUser != "" {
		return e.NewUser
	}
	return e.Default
}

func (t *Theme) eventTarget(m *Manifest, name string) string {
	if s, ok := t.Events[name]; ok {
		return s
	}
	return m.Events[name].Default
}

// EventTarget returns the screen an event opens, or "" if none.
func (t *Theme) EventTarget(m *Manifest, name string) string { return t.eventTarget(m, name) }
