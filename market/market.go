// Package market describes modules for the bot builder and the module market:
// what a module is called, what it needs, how it is configured and what it
// offers to screens.
package market

import "github.com/tori43-hash/tors/theme"

// Info is implemented by modules through Describer.
type Info struct {
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Category string `json:"category"` // core | panel | payment | feature
	// Hidden modules are infrastructure: the builder adds them when needed.
	Hidden bool `json:"hidden,omitzero"`
	// Default modules are picked automatically when their namespace is needed.
	Default  bool     `json:"default,omitzero"`
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

// Option of a select field. Requires lists modules the option needs, such as
// the http app for webhooks.
type Option struct {
	Value    string   `json:"value"`
	Title    string   `json:"title"`
	Requires []string `json:"requires,omitzero"`
}

// Describer is implemented by modules that appear in the builder.
type Describer interface {
	Market() Info
}

// Trust levels of modules in the market.
const (
	Official = "official" // maintained in the TORS repository
	Verified = "verified" // third-party, code reviewed at a pinned version
)

// Module is the descriptor of one module: everything the builder needs to
// offer, configure and build it without its code.
type Module struct {
	ID      string `json:"id"`
	Package string `json:"package"`
	// Version is the Go module version to build with; empty for official
	// modules, which come with the core.
	Version string `json:"version,omitzero"`
	Trust   string `json:"trust,omitzero"`
	Info    `json:",inline"`
	UI      *UI `json:"ui,omitzero"`
}

// UI is what a module offers to screens, and its starter screens.
type UI struct {
	Data       map[string]theme.DataSource `json:"data,omitzero"`
	Actions    map[string]theme.Action     `json:"actions,omitzero"`
	Conditions map[string]theme.Condition  `json:"conditions,omitzero"`
	Events     map[string]theme.Event      `json:"events,omitzero"`
	Screens    map[string]*theme.Screen    `json:"screens,omitzero"`
}

// Pack is a file of descriptors: the market index, or a tors-module.json a
// module author publishes.
type Pack struct {
	Version int `json:"version"`
	// Core is the core version official modules come with.
	Core    string   `json:"core,omitzero"`
	Modules []Module `json:"modules"`
}
