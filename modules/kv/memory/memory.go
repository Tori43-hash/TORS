// Package memory is kv.stores.memory: values in process memory, lost on restart.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
)

func init() { tors.RegisterModule(new(Store)) }

type item struct {
	value   []byte
	expires time.Time
}

// Store is an in-memory store for development and tests.
type Store struct {
	mu    sync.Mutex
	items map[string]item
}

func (*Store) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "kv.stores.memory", New: func() tors.Module { return new(Store) }}
}

func (*Store) Market() market.Info {
	return market.Info{
		Name: "Хранение в памяти", Summary: "Состояние диалогов теряется при перезапуске. Для проверки бота.",
		Category: "core", Hidden: true, Host: &market.Host{App: "kv", Field: "store", Key: "store"},
	}
}

func (s *Store) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok || (!it.expires.IsZero() && time.Now().After(it.expires)) {
		delete(s.items, key)
		return nil, false, nil
	}
	return it.value, true, nil
}

func (s *Store) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]item{}
	}
	it := item{value: value}
	if ttl > 0 {
		it.expires = time.Now().Add(ttl)
	}
	s.items[key] = it
	return nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}
