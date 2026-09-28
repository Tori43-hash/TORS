// Package database is the "database" app: a PostgreSQL pool shared by modules.
// Every module gets its own schema (named after the module) and runs its own
// migrations in it; queries name tables with the schema: billing.orders.
package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
)

func init() { tors.RegisterModule(App{}) }

// App holds the connection pool.
type App struct {
	// DSN is a PostgreSQL connection string, usually "{env.DATABASE_URL}".
	DSN      string `json:"dsn"`
	MaxConns int32  `json:"max_conns,omitzero"`

	pool *pgxpool.Pool
}

func (App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "database", New: func() tors.Module { return new(App) }}
}

func (App) Market() market.Info {
	return market.Info{
		Name: "PostgreSQL", Summary: "База данных бота.", Category: "core", Hidden: true,
		Config: []market.Field{{Key: "dsn", Title: "Строка подключения", Type: "secret", Default: "{env.DATABASE_URL}", Required: true}},
	}
}

func (a *App) Provision(ctx tors.Context) error {
	if a.DSN == "" {
		return errors.New("не указан dsn — строка подключения к PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(a.DSN)
	if err != nil {
		return fmt.Errorf("dsn: %w", err)
	}
	if a.MaxConns > 0 {
		cfg.MaxConns = a.MaxConns
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a.pool, err = pgxpool.NewWithConfig(c, cfg)
	if err != nil {
		return err
	}
	if err := a.pool.Ping(c); err != nil {
		a.pool.Close()
		return fmt.Errorf("нет соединения с PostgreSQL: %w", err)
	}
	return nil
}

func (a *App) Cleanup() error {
	if a.pool != nil {
		a.pool.Close()
	}
	return nil
}

func (a *App) Health(ctx context.Context) error { return a.pool.Ping(ctx) }

// For returns the database handle of the module being provisioned and
// creates its schema.
func (a *App) For(ctx tors.Context) (*DB, error) {
	schema := strings.ReplaceAll(string(ctx.Module()), ".", "_")
	if schema == "" {
		return nil, errors.New("database.For вызывается из Provision модуля")
	}
	if _, err := a.pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return nil, fmt.Errorf("схема %s: %w", schema, err)
	}
	return &DB{pool: a.pool, schema: schema}, nil
}

// Get returns the database app from a module's context.
func Get(ctx tors.Context) (*DB, error) {
	app, err := tors.AppAs[*App](ctx, "database")
	if err != nil {
		return nil, err
	}
	return app.For(ctx)
}

// Querier is implemented by the pool and by transactions.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is one module's view of the database.
type DB struct {
	pool   *pgxpool.Pool
	schema string
}

// Schema is the module's schema name.
func (d *DB) Schema() string { return d.schema }

type txKey struct{}

// Q returns the transaction carried by ctx, or the pool.
func (d *DB) Q(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return d.pool
}

// InTx runs fn in a transaction. Nested calls join the outer transaction, so
// several modules can write atomically — for example an order and the job
// that fulfils it.
func (d *DB) InTx(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Migrate applies the *.sql files of dir in name order, once each, inside the
// module's schema. Unqualified names in migrations land in that schema.
func (d *DB) Migrate(ctx context.Context, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)
	schema := pgx.Identifier{d.schema}.Sanitize()
	return d.InTx(ctx, func(ctx context.Context) error {
		q := d.Q(ctx)
		if _, err := q.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "tors.migrate."+d.schema); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+schema+".schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
			return err
		}
		for _, f := range files {
			var done bool
			if err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+schema+".schema_migrations WHERE version = $1)", f).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			body, err := fs.ReadFile(fsys, path.Join(dir, f))
			if err != nil {
				return err
			}
			if _, err := q.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("миграция %s/%s: %w", d.schema, f, err)
			}
			if _, err := q.Exec(ctx, "INSERT INTO "+schema+".schema_migrations (version) VALUES ($1)", f); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, "SET LOCAL search_path TO DEFAULT")
		return err
	})
}

// IsUniqueViolation reports whether err is a unique constraint violation.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
