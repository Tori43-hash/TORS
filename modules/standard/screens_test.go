package standard_test

import (
	"testing"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

// TestStarterScreens checks that the screens modules offer in the builder
// are valid together with everything the modules declare.
func TestStarterScreens(t *testing.T) {
	var parts []ui.Contribution
	th := &theme.Theme{Version: 1, Screens: map[string]*theme.Screen{}}
	for _, info := range tors.Modules() {
		m, ok := ui.Describe(info)
		if !ok || m.UI == nil {
			continue
		}
		c := ui.Contribution{Data: m.UI.Data, Actions: m.UI.Actions, Conditions: m.UI.Conditions, Events: m.UI.Events}
		parts = append(parts, c)
		for id, s := range m.UI.Screens {
			if th.Screens[id] != nil {
				t.Errorf("экран %s есть у нескольких модулей", id)
			}
			th.Screens[id] = s
		}
	}
	th.Screens["menu"] = &theme.Screen{Blocks: []theme.Block{{Text: theme.Text{"ru": "Меню", "en": "Menu"}}}}
	th.Commands = map[string]theme.Entry{"start": {Default: "menu"}}
	m := ui.Merge([]string{"ru", "en"}, parts...)
	for _, is := range theme.Validate(th, m) {
		if is.Level == theme.LevelError {
			t.Errorf("%s %s: %s", is.Screen, is.Ref, is.Message)
		}
	}
}
