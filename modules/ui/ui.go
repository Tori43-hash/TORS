// Package ui is the "ui" app: the registry through which modules offer data,
// actions, conditions and events to the bot's screens. Presentation modules
// (telegram, later a Mini App) read it; domain modules never import them.
package ui

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/jobs"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

// User is the person a screen is shown to.
type User struct {
	ID         int64 // internal user id
	TelegramID int64
	FirstName  string
	Username   string
	Lang       string
	New        bool // first contact with the bot
}

// Call is what data sources, actions and conditions receive.
type Call struct {
	User   User
	Params map[string]string
}

// Result of an action: the outcome name and params passed to the next screen.
type Result struct {
	Outcome string
	Params  map[string]string
}

type (
	DataFunc      func(ctx context.Context, c Call) (any, error)
	ActionFunc    func(ctx context.Context, c Call) (Result, error)
	ConditionFunc func(ctx context.Context, c Call) (bool, error)
)

// Contribution is the static description of what a module offers to screens.
// It needs no running bot, so the builder and the module market read it too.
type Contribution struct {
	Data       map[string]theme.DataSource
	Actions    map[string]theme.Action
	Conditions map[string]theme.Condition
	Events     map[string]theme.Event
	// Screens are starter screens the builder adds together with the module.
	Screens map[string]*theme.Screen
}

// Provider is implemented by modules that contribute to screens.
type Provider interface {
	UI() Contribution
}

// Bindings connect a contribution to code.
type Bindings struct {
	Data       map[string]DataFunc
	Actions    map[string]ActionFunc
	Conditions map[string]ConditionFunc
}

// UserEvent is an event shown to one user as a screen.
type UserEvent interface {
	tors.Event
	Recipient() int64 // internal user id
	EventData() map[string]any
}

// Presenter shows event screens; the telegram module is one.
type Presenter func(ctx context.Context, event string, userID int64, data map[string]any) error

// App is the registry.
type App struct {
	mu         sync.RWMutex
	queue      *jobs.Queue
	parts      map[tors.ModuleID]Contribution
	data       map[string]DataFunc
	actions    map[string]ActionFunc
	conditions map[string]ConditionFunc
	presenters []Presenter
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "ui", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name: "Экраны", Summary: "Связывает экраны бота с данными и действиями модулей.",
		Category: "core", Hidden: true, Requires: []string{"jobs"},
	}
}

func (a *App) Provision(ctx tors.Context) error {
	var err error
	if a.queue, err = jobs.Get(ctx); err != nil {
		return err
	}
	a.parts = map[tors.ModuleID]Contribution{}
	a.data = map[string]DataFunc{}
	a.actions = map[string]ActionFunc{}
	a.conditions = map[string]ConditionFunc{}
	return nil
}

// Registry is one module's access to the ui app.
type Registry struct {
	app    *App
	module tors.ModuleID
}

// Get returns the registry of the module being provisioned.
func Get(ctx tors.Context) (*Registry, error) {
	app, err := tors.AppAs[*App](ctx, "ui")
	if err != nil {
		return nil, err
	}
	return &Registry{app: app, module: ctx.Module()}, nil
}

// Register adds a module's contribution and its code. Every name must belong
// to the module ("subscriptions.list" for module "subscriptions") and every
// declared data source, action and condition must have code.
func (r *Registry) Register(c Contribution, b Bindings) error {
	check := func(kind string, declared, bound []string) error {
		for _, n := range declared {
			if !tors.Owns(r.module, n) {
				return fmt.Errorf("%s %s не принадлежит модулю %s", kind, n, r.module)
			}
			if !slices.Contains(bound, n) {
				return fmt.Errorf("%s %s объявлен, но не реализован", kind, n)
			}
		}
		for _, n := range bound {
			if !slices.Contains(declared, n) {
				return fmt.Errorf("%s %s реализован, но не объявлен", kind, n)
			}
		}
		return nil
	}
	if err := check("источник данных", sortedKeys(c.Data), sortedKeys(b.Data)); err != nil {
		return err
	}
	if err := check("действие", sortedKeys(c.Actions), sortedKeys(b.Actions)); err != nil {
		return err
	}
	if err := check("условие", sortedKeys(c.Conditions), sortedKeys(b.Conditions)); err != nil {
		return err
	}
	for n := range c.Events {
		if !tors.Owns(r.module, n) {
			return fmt.Errorf("событие %s не принадлежит модулю %s", n, r.module)
		}
	}
	a := r.app
	a.mu.Lock()
	defer a.mu.Unlock()
	a.parts[r.module] = c
	maps.Copy(a.data, b.Data)
	maps.Copy(a.actions, b.Actions)
	maps.Copy(a.conditions, b.Conditions)
	return nil
}

// Deliver makes a module's user event reach presenters: a durable
// subscription retries until the screen is sent.
func Deliver[E UserEvent](r *Registry) {
	name := tors.EventNameOf[E]()
	jobs.Subscribe(r.app.queue, "event."+name, func(ctx context.Context, e E) error {
		r.app.mu.RLock()
		ps := slices.Clone(r.app.presenters)
		r.app.mu.RUnlock()
		for _, p := range ps {
			if err := p(ctx, name, e.Recipient(), e.EventData()); err != nil {
				return err
			}
		}
		return nil
	})
}

// Present registers a presenter of event screens.
func (a *App) Present(p Presenter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.presenters = append(a.presenters, p)
}

func (a *App) Data(name string) (DataFunc, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	f, ok := a.data[name]
	return f, ok
}

func (a *App) Action(name string) (ActionFunc, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	f, ok := a.actions[name]
	return f, ok
}

func (a *App) Condition(name string) (ConditionFunc, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	f, ok := a.conditions[name]
	return f, ok
}

// UserFields are the template fields of .User.
var UserFields = []theme.Field{
	{Name: "FirstName", Type: "string", Title: "Имя", Sample: "Анна"},
	{Name: "Username", Type: "string", Title: "Username", Sample: "anna"},
	{Name: "ID", Type: "int", Title: "Telegram ID", Sample: 123456789},
}

// Manifest describes everything registered, for theme validation.
func (a *App) Manifest(languages []string) *theme.Manifest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	parts := make([]Contribution, 0, len(a.parts))
	for _, id := range sortedKeys(a.parts) {
		parts = append(parts, a.parts[tors.ModuleID(id)])
	}
	return Merge(languages, parts...)
}

// Merge combines contributions into a manifest.
func Merge(languages []string, parts ...Contribution) *theme.Manifest {
	m := &theme.Manifest{
		Version:    1,
		Languages:  languages,
		User:       UserFields,
		Data:       map[string]theme.DataSource{},
		Actions:    map[string]theme.Action{},
		Conditions: map[string]theme.Condition{},
		Events:     map[string]theme.Event{},
	}
	for _, c := range parts {
		maps.Copy(m.Data, c.Data)
		maps.Copy(m.Actions, c.Actions)
		maps.Copy(m.Conditions, c.Conditions)
		maps.Copy(m.Events, c.Events)
	}
	return m
}

// Record converts a struct or map to template data: exported fields become keys.
func Record(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	out := map[string]any{}
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		return out
	}
	for i := range rv.NumField() {
		f := rv.Type().Field(i)
		if f.IsExported() {
			out[f.Name] = rv.Field(i).Interface()
		}
	}
	return out
}

// List converts a slice of structs or maps to template data.
func List[T any](items []T) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = Record(it)
	}
	return out
}

func sortedKeys[M ~map[K]V, K ~string, V any](m M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// Owner returns the module that owns a name such as "subscriptions.list".
func Owner(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[:i]
}
