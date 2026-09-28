package theme

import (
	"encoding/json/v2"
	"slices"
)

// Manifest describes what the compiled bot offers to themes: data sources,
// actions with their outcomes, conditions and events. Modules contribute to it;
// `tors ui manifest` writes it out for the screen editor.
type Manifest struct {
	Version    int                   `json:"version"`
	Languages  []string              `json:"languages"`
	User       []Field               `json:"user"`
	Data       map[string]DataSource `json:"data"`
	Actions    map[string]Action     `json:"actions"`
	Conditions map[string]Condition  `json:"conditions"`
	Events     map[string]Event      `json:"events"`
}

// Field is a value available to templates. Sample feeds previews.
type Field struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // string | int | bool | time | bytes | url
	Title  string `json:"title,omitzero"`
	Sample any    `json:"sample,omitzero"`
}

// DataSource is a read-only provider a screen binds under an alias.
type DataSource struct {
	Title   string           `json:"title"`
	Params  []string         `json:"params,omitzero"`
	List    bool             `json:"list,omitzero"`
	Fields  []Field          `json:"fields"`
	Samples []map[string]any `json:"samples,omitzero"`
}

// Action is a module operation a button or an input can trigger.
type Action struct {
	Title    string    `json:"title"`
	Label    Text      `json:"label,omitzero"`
	Params   []string  `json:"params,omitzero"`
	Input    bool      `json:"input,omitzero"`
	Outcomes []Outcome `json:"outcomes"`
}

// Outcome is a named result of an action. Default is the screen shown unless
// the theme maps it elsewhere. Terminal outcomes show no screen (e.g. the bot
// sends a payment invoice instead).
type Outcome struct {
	Name     string `json:"name"`
	Title    string `json:"title,omitzero"`
	Default  string `json:"default,omitzero"`
	Terminal bool   `json:"terminal,omitzero"`
	// Params the action passes to the outcome screen, e.g. the new subscription.
	Params []string `json:"params,omitzero"`
}

// Condition is a named predicate for visible_if and disabled_if.
type Condition struct {
	Title  string `json:"title"`
	Sample bool   `json:"sample"`
}

// Event is something that happens outside a button press and can open a screen.
type Event struct {
	Title   string  `json:"title"`
	Default string  `json:"default,omitzero"`
	Fields  []Field `json:"fields,omitzero"`
}

// ParseManifest decodes a manifest.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	return &m, nil
}

func (a Action) outcome(name string) (Outcome, bool) {
	i := slices.IndexFunc(a.Outcomes, func(o Outcome) bool { return o.Name == name })
	if i < 0 {
		return Outcome{}, false
	}
	return a.Outcomes[i], true
}
