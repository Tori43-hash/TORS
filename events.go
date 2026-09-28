package tors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Event is a fact that has happened. Names are namespaced by the module that
// owns them: "billing.order.paid" belongs to module "billing".
type Event interface {
	EventName() string
}

// Envelope wraps an event with where and when it came from.
type Envelope struct {
	Name   string
	Origin ModuleID
	Time   time.Time
	Event  Event
}

// ErrAbort returned by a handler cancels the emit; Emit returns it to the emitter.
var ErrAbort = errors.New("событие отменено")

// Handler reacts to an event.
type Handler func(context.Context, Envelope) error

type subscription struct {
	id      uint64
	pattern string
	fn      Handler
}

type bus struct {
	mu     sync.RWMutex
	seq    uint64
	subs   []subscription
	logger *slog.Logger
}

// Events is the bus as seen by one module.
type Events struct {
	bus    *bus
	origin ModuleID
}

// Emit delivers an event to subscribers synchronously, in subscription order.
// A module may only emit events in its own namespace. Handler panics and
// errors are logged; ErrAbort stops delivery and is returned.
func (e *Events) Emit(ctx context.Context, ev Event) error {
	name := ev.EventName()
	if !Owns(e.origin, name) {
		return fmt.Errorf("модуль %s не может публиковать событие %s", e.origin, name)
	}
	env := Envelope{Name: name, Origin: e.origin, Time: time.Now(), Event: ev}
	e.bus.mu.RLock()
	subs := make([]subscription, 0, len(e.bus.subs))
	for _, s := range e.bus.subs {
		if matches(s.pattern, name) {
			subs = append(subs, s)
		}
	}
	e.bus.mu.RUnlock()
	for _, s := range subs {
		if err := e.bus.call(ctx, s, env); errors.Is(err, ErrAbort) {
			return ErrAbort
		} else if err != nil {
			e.bus.logger.Error("обработчик события", "event", name, "error", err)
		}
	}
	return nil
}

func (b *bus) call(ctx context.Context, s subscription, env Envelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("паника: %v", r)
		}
	}()
	return s.fn(ctx, env)
}

// OnPattern subscribes to events by name: "billing.order.paid", "billing.*" or "*".
func (e *Events) OnPattern(pattern string, h Handler) (off func()) {
	e.bus.mu.Lock()
	defer e.bus.mu.Unlock()
	e.bus.seq++
	id := e.bus.seq
	e.bus.subs = append(e.bus.subs, subscription{id: id, pattern: pattern, fn: h})
	return func() {
		e.bus.mu.Lock()
		defer e.bus.mu.Unlock()
		for i, s := range e.bus.subs {
			if s.id == id {
				e.bus.subs = append(e.bus.subs[:i], e.bus.subs[i+1:]...)
				return
			}
		}
	}
}

// On subscribes to a typed event.
func On[E Event](e *Events, h func(context.Context, E) error) (off func()) {
	return e.OnPattern(EventNameOf[E](), func(ctx context.Context, env Envelope) error {
		ev, ok := env.Event.(E)
		if !ok {
			return fmt.Errorf("событие %s: ожидался тип %T", env.Name, *new(E))
		}
		return h(ctx, ev)
	})
}

// EventNameOf returns the name of an event type.
func EventNameOf[E Event]() string {
	var zero E
	t := reflect.TypeFor[E]()
	if t.Kind() == reflect.Pointer {
		return reflect.New(t.Elem()).Interface().(Event).EventName()
	}
	return zero.EventName()
}

// Owns reports whether a module may publish an event with this name.
func Owns(module ModuleID, name string) bool {
	return module != "" && strings.HasPrefix(name, string(module)+".")
}

func matches(pattern, name string) bool {
	switch {
	case pattern == "*" || pattern == name:
		return true
	case strings.HasSuffix(pattern, ".*"):
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
