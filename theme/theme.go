// Package theme describes the bot UI as data: screens, blocks, keyboards and
// the transitions between them. The same package validates and renders themes
// in the bot and, compiled to WebAssembly, in the screen editor.
package theme

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// FormatVersion is the theme format understood by this package.
const FormatVersion = 1

// Telegram limits checked by Validate. The Bot API does not document them;
// the values follow what Telegram clients accept in practice.
const (
	MaxButtonsPerRow = 8
	MaxButtons       = 100
)

// Theme is the whole UI of a bot.
type Theme struct {
	Version   int                          `json:"version"`
	Commands  map[string]Entry             `json:"commands,omitzero"`
	Events    map[string]string            `json:"events,omitzero"`
	Routes    map[string]map[string]string `json:"routes,omitzero"`
	Fragments map[string][]Row             `json:"fragments,omitzero"`
	Screens   map[string]*Screen           `json:"screens"`
	// Editor holds editor-only state such as node positions; the bot ignores it.
	Editor jsontext.Value `json:"editor,omitzero"`
}

// Entry is the screen a command opens. A plain string sets Default.
type Entry struct {
	Default string `json:"default"`
	NewUser string `json:"new_user,omitzero"`
}

func (e *Entry) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	if v.Kind() == '"' {
		*e = Entry{}
		return json.Unmarshal(v, &e.Default)
	}
	type plain Entry
	return json.Unmarshal(v, (*plain)(e), dec.Options())
}

// Screen is one message of the bot: body blocks and the keyboard under it.
type Screen struct {
	Data     map[string]string `json:"data,omitzero"`
	Blocks   []Block           `json:"blocks,omitzero"`
	Keyboard []Row             `json:"keyboard,omitzero"`
	Empty    string            `json:"empty,omitzero"`
	Input    *Input            `json:"input,omitzero"`
}

// Block is exactly one of a photo, a text or a row of buttons inside the message.
type Block struct {
	Photo   string  `json:"photo,omitzero"`
	Text    Text    `json:"text,omitzero"`
	Buttons *Inline `json:"buttons,omitzero"`
}

// Text maps a language code to a Markdown template.
type Text map[string]string

// Inline is a row of buttons inside the message body.
type Inline struct {
	Align string   `json:"align,omitzero"`
	Items []Button `json:"items"`
}

// Row is a keyboard row: explicit buttons, a repeat over a list, or a fragment.
type Row struct {
	Buttons []Button
	Repeat  *Repeat
	Use     string
}

// Repeat produces one button per item of a list data source.
type Repeat struct {
	Source   string `json:"repeat"`
	Columns  int    `json:"columns,omitzero"`
	PageSize int    `json:"page_size,omitzero"`
	Button   Button `json:"button"`
}

func (r Row) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch {
	case r.Repeat != nil:
		return json.MarshalEncode(enc, r.Repeat)
	case r.Use != "":
		return json.MarshalEncode(enc, map[string]string{"use": r.Use})
	case r.Buttons == nil:
		return json.MarshalEncode(enc, []Button{})
	default:
		return json.MarshalEncode(enc, r.Buttons)
	}
}

func (r *Row) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	*r = Row{}
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	switch v.Kind() {
	case '[':
		return json.Unmarshal(v, &r.Buttons, dec.Options())
	case '{':
		var probe map[string]jsontext.Value
		if err := json.Unmarshal(v, &probe); err != nil {
			return err
		}
		if _, ok := probe["repeat"]; ok {
			r.Repeat = new(Repeat)
			return json.Unmarshal(v, r.Repeat, dec.Options())
		}
		var use struct {
			Use string `json:"use"`
		}
		if err := json.Unmarshal(v, &use, dec.Options()); err != nil {
			return err
		}
		if use.Use == "" {
			return fmt.Errorf("keyboard row object needs \"repeat\" or \"use\"")
		}
		r.Use = use.Use
		return nil
	default:
		return fmt.Errorf("keyboard row must be an array or an object")
	}
}

// Button is a keyboard or body button. Exactly one target field is set.
type Button struct {
	Text       Text              `json:"text,omitzero"`
	Emoji      string            `json:"emoji,omitzero"`
	Icon       string            `json:"icon,omitzero"`
	Style      string            `json:"style,omitzero"`
	Goto       string            `json:"goto,omitzero"`
	Action     string            `json:"action,omitzero"`
	On         map[string]string `json:"on,omitzero"`
	Params     map[string]string `json:"params,omitzero"`
	URL        string            `json:"url,omitzero"`
	WebApp     string            `json:"web_app,omitzero"`
	CopyText   string            `json:"copy_text,omitzero"`
	Back       bool              `json:"back,omitzero"`
	Home       bool              `json:"home,omitzero"`
	VisibleIf  string            `json:"visible_if,omitzero"`
	DisabledIf string            `json:"disabled_if,omitzero"`
}

// Kind names what pressing the button does.
func (b *Button) Kind() string {
	switch {
	case b.Goto != "":
		return KindGoto
	case b.Action != "":
		return KindAction
	case b.URL != "":
		return KindURL
	case b.WebApp != "":
		return KindWebApp
	case b.CopyText != "":
		return KindCopy
	case b.Back:
		return KindBack
	case b.Home:
		return KindHome
	}
	return ""
}

func (b *Button) targetCount() int {
	n := 0
	for _, set := range []bool{b.Goto != "", b.Action != "", b.URL != "", b.WebApp != "", b.CopyText != "", b.Back, b.Home} {
		if set {
			n++
		}
	}
	return n
}

// Button kinds.
const (
	KindGoto   = "goto"
	KindAction = "action"
	KindURL    = "url"
	KindWebApp = "web_app"
	KindCopy   = "copy_text"
	KindBack   = "back"
	KindHome   = "home"
	KindPage   = "page"
)

// Input makes a screen wait for a text message and pass it to an action.
type Input struct {
	Action string            `json:"action"`
	Param  string            `json:"param"`
	On     map[string]string `json:"on,omitzero"`
}

// Parse decodes a theme. Unknown fields are errors so typos surface early.
func Parse(b []byte) (*Theme, error) {
	var t Theme
	if err := json.Unmarshal(b, &t, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	if t.Screens == nil {
		t.Screens = map[string]*Screen{}
	}
	return &t, nil
}

// ParseScreens decodes a map of screens (a module's starter screens).
func ParseScreens(b []byte) (map[string]*Screen, error) {
	var s map[string]*Screen
	if err := json.Unmarshal(b, &s, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	return s, nil
}
