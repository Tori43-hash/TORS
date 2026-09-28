package tors

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
)

// Context is passed to Provision. It carries the running instance, so a
// module can load its guest modules, reach other apps, log and emit events.
type Context struct {
	context.Context
	inst   *Instance
	module ModuleID
}

// Module is the ID of the module being provisioned ("" at the root).
func (ctx Context) Module() ModuleID { return ctx.module }

// Logger returns a logger tagged with the module ID.
func (ctx Context) Logger() *slog.Logger {
	if ctx.module == "" {
		return ctx.inst.logger
	}
	return ctx.inst.logger.With("module", string(ctx.module))
}

// Events returns the event bus bound to this module: it may only emit
// events in its own namespace ("billing" emits "billing.*").
func (ctx Context) Events() *Events {
	return &Events{bus: ctx.inst.bus, origin: ctx.module}
}

// OnCancel runs f when the instance stops.
func (ctx Context) OnCancel(f func()) {
	context.AfterFunc(ctx.Context, f)
}

// App returns a top-level module, provisioning it first if needed. An app
// missing from the config is provisioned with an empty config.
func (ctx Context) App(name string) (any, error) {
	return ctx.inst.app(name)
}

// AppIfConfigured returns an app only if it is present in the config.
func (ctx Context) AppIfConfigured(name string) (any, error) {
	if _, ok := ctx.inst.cfg.Apps[name]; !ok {
		if v, ok := ctx.inst.apps[name]; ok {
			return v, nil
		}
		return nil, nil
	}
	return ctx.inst.app(name)
}

// AppAs is App with a type assertion; T is usually the app's service interface.
func AppAs[T any](ctx Context, name string) (T, error) {
	var zero T
	v, err := ctx.App(name)
	if err != nil {
		return zero, err
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("приложение %s (%T) не реализует %s", name, v, reflect.TypeFor[T]())
	}
	return t, nil
}

// Health checks every provisioned module that implements HealthChecker.
func (ctx Context) Health(c context.Context) map[string]error {
	return ctx.inst.Health(c)
}

// LoadModule loads the guest modules described by a field of structPtr.
// The field carries a tag `tors:"namespace=<ns> inline_key=<key>"` and is one of:
//
//	jsontext.Value              one module; returns any
//	[]jsontext.Value            an ordered list; returns []any
//	map[string]jsontext.Value   named instances; returns map[string]any
//
// The module ID is namespace + "." + the value of the inline key.
func (ctx Context) LoadModule(structPtr any, field string) (any, error) {
	rv := reflect.ValueOf(structPtr)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("LoadModule: нужен указатель на структуру, получен %T", structPtr)
	}
	sf, ok := rv.Elem().Type().FieldByName(field)
	if !ok {
		return nil, fmt.Errorf("LoadModule: у %T нет поля %s", structPtr, field)
	}
	ns, key := parseTag(sf.Tag.Get("tors"))
	if key == "" {
		return nil, fmt.Errorf("LoadModule: у поля %s нет inline_key в теге tors", field)
	}
	fv := rv.Elem().FieldByName(field).Interface()
	switch raw := fv.(type) {
	case jsontext.Value:
		if len(raw) == 0 {
			return nil, nil
		}
		return ctx.loadInline(ns, key, raw)
	case []jsontext.Value:
		out := make([]any, 0, len(raw))
		for i, r := range raw {
			m, err := ctx.loadInline(ns, key, r)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", field, i, err)
			}
			out = append(out, m)
		}
		return out, nil
	case map[string]jsontext.Value:
		out := make(map[string]any, len(raw))
		for name, r := range raw {
			m, err := ctx.loadInline(ns, key, r)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", field, name, err)
			}
			out[name] = m
		}
		return out, nil
	default:
		return nil, fmt.Errorf("LoadModule: поле %s имеет неподдерживаемый тип %T", field, fv)
	}
}

// LoadModuleByID provisions a module by its full ID from raw JSON config.
func (ctx Context) LoadModuleByID(id string, raw jsontext.Value) (any, error) {
	return ctx.inst.load(ctx, ModuleID(id), raw)
}

func (ctx Context) loadInline(ns, key string, raw jsontext.Value) (any, error) {
	var obj map[string]jsontext.Value
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("ожидается объект: %w", err)
	}
	nameRaw, ok := obj[key]
	if !ok {
		return nil, fmt.Errorf("не указан %q — какой модуль из %s использовать", key, ns)
	}
	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil || name == "" {
		return nil, fmt.Errorf("%q должен быть непустой строкой", key)
	}
	delete(obj, key)
	rest, err := json.Marshal(obj, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return ctx.inst.load(ctx, ModuleID(ns+"."+name), rest)
}

func parseTag(tag string) (namespace, inlineKey string) {
	for _, part := range strings.Fields(tag) {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "namespace":
			namespace = v
		case "inline_key":
			inlineKey = v
		}
	}
	return namespace, inlineKey
}
