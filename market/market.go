// Package market describes modules for the bot builder and the module market:
// what a module is called, what it needs and how it is configured.
package market

// Info is implemented by modules through Describer.
type Info struct {
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Category string `json:"category"` // core | panel | payment | feature
	// Hidden modules are infrastructure: the builder adds them when needed.
	Hidden   bool     `json:"hidden,omitzero"`
	Requires []string `json:"requires,omitzero"`
	// Needs lists namespaces the bot needs at least one module from, such as
	// "billing.gateways" for a way to pay.
	Needs []string `json:"needs,omitzero"`
	// Host says where a guest module's config goes inside its app.
	Host   *Host   `json:"host,omitzero"`
	Config []Field `json:"config,omitzero"`
}

// Host places a guest module into its app's config: apps[App][Field] is a map
// of named instances (Named) or a single object, selected by Key.
type Host struct {
	App   string `json:"app"`
	Field string `json:"field"`
	Key   string `json:"key"`
	Named bool   `json:"named,omitzero"`
}

// Field is one setting in the builder's form.
type Field struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitzero"`
	// Type: text | secret | number | bool | select | i18n | list | strings
	Type        string   `json:"type"`
	Default     any      `json:"default,omitzero"`
	Placeholder string   `json:"placeholder,omitzero"`
	Required    bool     `json:"required,omitzero"`
	Options     []Option `json:"options,omitzero"`
	// OptionsFrom fills options at edit time: "panels" lists panel instances.
	OptionsFrom string  `json:"options_from,omitzero"`
	Item        []Field `json:"item,omitzero"` // fields of each item of a list
}

// Option of a select field.
type Option struct {
	Value string `json:"value"`
	Title string `json:"title"`
}

// Describer is implemented by modules that appear in the builder.
type Describer interface {
	Market() Info
}
