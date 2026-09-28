// Package postgres is kv.stores.postgres: values in a table of the bot's database.
package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/database"
)

func init() { tors.RegisterModule(Store{}) }

//go:embed migrations/*.sql
var migrations embed.FS

// Store keeps values in PostgreSQL; expired rows are removed lazily.
type Store struct {
	db *database.DB
}

func (Store) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "kv.stores.postgres", New: func() tors.Module { return new(Store) }}
}

func (Store) Market() market.Info {
	return market.Info{
		Name: "Хранение в PostgreSQL", Summary: "Состояние диалогов переживает перезапуск.", Category: "core", Hidden: true, Default: true,
		Requires: []string{"database"},
		Host:     &market.Host{App: "kv", Field: "store", Key: "store"},
	}
}

func (s *Store) Provision(ctx tors.Context) error {
	var err error
	if s.db, err = database.Get(ctx); err != nil {
		return err
	}
	return s.db.Migrate(ctx, migrations, "migrations")
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var v []byte
	err := s.db.Q(ctx).QueryRow(ctx,
		"SELECT value FROM kv_stores_postgres.kv WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())", key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return v, err == nil, err
}

func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	var exp *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		exp = &t
	}
	_, err := s.db.Q(ctx).Exec(ctx, `INSERT INTO kv_stores_postgres.kv (key, value, expires_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at`, key, value, exp)
	if err == nil && time.Now().UnixNano()%100 == 0 {
		_, _ = s.db.Q(ctx).Exec(ctx, "DELETE FROM kv_stores_postgres.kv WHERE expires_at < now()")
	}
	return err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.db.Q(ctx).Exec(ctx, "DELETE FROM kv_stores_postgres.kv WHERE key = $1", key)
	return err
}
