package theme

import (
	"maps"
	"slices"
	"strconv"
)

// IncomingParams returns, for every screen, the param names other screens may
// pass to it: button params on goto and on action outcomes, and the params of
// input screens plus the entered text on their outcomes.
func IncomingParams(t *Theme, m *Manifest) map[string][]string {
	in := map[string]map[string]bool{}
	add := func(target string, keys ...string) {
		if target == "" {
			return
		}
		if in[target] == nil {
			in[target] = map[string]bool{}
		}
		for _, k := range keys {
			in[target][k] = true
		}
	}
	for _, id := range sortedKeys(t.Screens) {
		t.EachButton(id, func(_ string, b *Button) {
			keys := slices.Sorted(maps.Keys(b.Params))
			switch b.Kind() {
			case KindGoto:
				add(b.Goto, keys...)
			case KindAction:
				for name, target := range t.ResolveOutcomes(m, b.Action, b.On) {
					add(target, keys...)
					add(target, outcomeParams(m, b.Action, name)...)
				}
			}
		})
	}
	// Input screens forward what they received; a few passes cover chains.
	for range 3 {
		for _, id := range sortedKeys(t.Screens) {
			s := t.Screens[id]
			if s.Input == nil {
				continue
			}
			keys := append(slices.Sorted(maps.Keys(in[id])), s.Input.Param)
			for _, name := range s.Data {
				keys = append(keys, m.Data[name].Params...)
			}
			for name, target := range t.ResolveOutcomes(m, s.Input.Action, s.Input.On) {
				add(target, keys...)
				add(target, outcomeParams(m, s.Input.Action, name)...)
			}
		}
	}
	out := make(map[string][]string, len(in))
	for id, keys := range in {
		out[id] = slices.Sorted(maps.Keys(keys))
	}
	return out
}

// outcomeParams are the params an action adds for the screen of an outcome.
func outcomeParams(m *Manifest, action, outcome string) []string {
	o, _ := m.Actions[action].outcome(outcome)
	return o.Params
}

// EachButton calls fn for every button a screen defines — body, keyboard,
// fragments and repeat templates — with its ref.
func (t *Theme) EachButton(id string, fn func(ref string, b *Button)) {
	s := t.Screens[id]
	if s == nil {
		return
	}
	for i, blk := range s.Blocks {
		if blk.Buttons == nil {
			continue
		}
		for j := range blk.Buttons.Items {
			fn("b."+strconv.Itoa(i)+"."+strconv.Itoa(j), &blk.Buttons.Items[j])
		}
	}
	for i, row := range s.Keyboard {
		ref := "k." + strconv.Itoa(i)
		switch {
		case row.Repeat != nil:
			fn(ref+".r", &row.Repeat.Button)
		case row.Use != "":
			for fi, frow := range t.Fragments[row.Use] {
				for j := range frow.Buttons {
					fn(ref+".f"+strconv.Itoa(fi)+"."+strconv.Itoa(j), &frow.Buttons[j])
				}
			}
		default:
			for j := range row.Buttons {
				fn(ref+"."+strconv.Itoa(j), &row.Buttons[j])
			}
		}
	}
}
