// Package kv is the "kv" app: short-lived key-value data such as dialog state
// and message navigation. Stores are guest modules in "kv.stores".
package kv

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/tori43-hash/tors"
)

func init() { tors.RegisterModule(App{}) }

// Store keeps values with an optional time to live.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// App selects the store; without one, values live in memory.
type App struct {
	Store jsontext.Value `json:"store,omitzero" tors:"namespace=kv.stores inline_key=store"`

	store Store
}

func (App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "kv", New: func() tors.Module { return new(App) }}
}

func (a *App) Provision(ctx tors.Context) error {
	raw := a.Store
	if len(raw) == 0 {
		raw = jsontext.Value(`{"store":"memory"}`)
		a.Store = raw
	}
	m, err := ctx.LoadModule(a, "Store")
	if err != nil {
		return err
	}
	s, ok := m.(Store)
	if !ok {
		return errors.New("модуль хранилища не реализует kv.Store")
	}
	a.store = s
	return nil
}

// Bucket is one module's key space.
type Bucket struct {
	store  Store
	prefix string
}

// Get returns the bucket of the module being provisioned.
func Get(ctx tors.Context) (*Bucket, error) {
	app, err := tors.AppAs[*App](ctx, "kv")
	if err != nil {
		return nil, err
	}
	return &Bucket{store: app.store, prefix: string(ctx.Module()) + ":"}, nil
}

func (b *Bucket) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return b.store.Get(ctx, b.prefix+key)
}

func (b *Bucket) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return b.store.Set(ctx, b.prefix+key, value, ttl)
}

func (b *Bucket) Delete(ctx context.Context, key string) error {
	return b.store.Delete(ctx, b.prefix+key)
}

// GetJSON decodes a JSON value; ok is false when the key is missing.
func GetJSON[T any](ctx context.Context, b *Bucket, key string) (v T, ok bool, err error) {
	raw, ok, err := b.Get(ctx, key)
	if err != nil || !ok {
		return v, ok, err
	}
	return v, true, json.Unmarshal(raw, &v)
}

// SetJSON stores a value as JSON.
func SetJSON(ctx context.Context, b *Bucket, key string, v any, ttl time.Duration) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.Set(ctx, key, raw, ttl)
}
