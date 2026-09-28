// Package jobs is the "jobs" app: a job queue in PostgreSQL with retries,
// periodic tasks and durable events. A job enqueued inside a database
// transaction exists only if that transaction commits (transactional outbox).
package jobs

import (
	"context"
	"embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/database"
)

func init() { tors.RegisterModule(new(App)) }

//go:embed migrations/*.sql
var migrations embed.FS

// App runs workers that execute queued jobs.
type App struct {
	Workers      int           `json:"workers,omitzero"`       // default 4
	PollInterval tors.Duration `json:"poll_interval,omitzero"` // default 1s

	db       *database.DB
	log      interface{ Error(string, ...any) }
	mu       sync.RWMutex
	handlers map[string]func(context.Context, []byte) error
	subs     map[string][]string // event name → subscriber job kinds
	periodic []periodic
	wake     chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type periodic struct {
	name  string
	every time.Duration
	fn    func(context.Context) error
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "jobs", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name: "Фоновые задачи", Summary: "Очередь задач и события, которые не теряются при перезапуске.",
		Category: "core", Hidden: true, Requires: []string{"database"},
	}
}

func (a *App) Provision(ctx tors.Context) error {
	var err error
	if a.db, err = database.Get(ctx); err != nil {
		return err
	}
	if err := a.db.Migrate(ctx, migrations, "migrations"); err != nil {
		return err
	}
	if a.Workers <= 0 {
		a.Workers = 4
	}
	if a.PollInterval <= 0 {
		a.PollInterval = tors.Duration(time.Second)
	}
	a.log = ctx.Logger()
	a.handlers = map[string]func(context.Context, []byte) error{}
	a.subs = map[string][]string{}
	a.wake = make(chan struct{}, 1)
	return nil
}

// Start launches the workers and the scheduler of periodic tasks.
func (a *App) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	for range a.Workers {
		a.wg.Go(func() { a.work(ctx) })
	}
	a.wg.Go(func() { a.schedule(ctx) })
	return nil
}

func (a *App) Stop() error {
	if a.cancel != nil {
		a.cancel()
		a.wg.Wait()
		a.cancel = nil
	}
	return nil
}

// Queue is one module's access to the queue: its job kinds and events are
// prefixed with the module ID.
type Queue struct {
	app    *App
	module tors.ModuleID
}

// Get returns the queue of the module being provisioned.
func Get(ctx tors.Context) (*Queue, error) {
	app, err := tors.AppAs[*App](ctx, "jobs")
	if err != nil {
		return nil, err
	}
	return &Queue{app: app, module: ctx.Module()}, nil
}

func (q *Queue) kind(name string) string { return string(q.module) + "." + name }

// Handle registers the handler of a job kind. Handlers must be idempotent:
// a job may run more than once.
func (q *Queue) Handle(name string, fn func(ctx context.Context, payload []byte) error) {
	q.app.mu.Lock()
	defer q.app.mu.Unlock()
	q.app.handlers[q.kind(name)] = fn
}

// Enqueue adds a job. Inside database.InTx the job commits with the transaction.
func (q *Queue) Enqueue(ctx context.Context, name string, payload any) error {
	return q.app.enqueue(ctx, q.kind(name), payload, time.Time{})
}

// EnqueueAt adds a job that runs no earlier than at.
func (q *Queue) EnqueueAt(ctx context.Context, name string, payload any, at time.Time) error {
	return q.app.enqueue(ctx, q.kind(name), payload, at)
}

// Every runs fn periodically. The next run time is kept in the database, so
// restarts and several instances do not multiply runs.
func (q *Queue) Every(name string, every time.Duration, fn func(context.Context) error) {
	q.app.mu.Lock()
	defer q.app.mu.Unlock()
	q.app.periodic = append(q.app.periodic, periodic{name: q.kind(name), every: every, fn: fn})
}

// Publish delivers an event to its durable subscribers: one job per
// subscriber, retried until it succeeds. Call it inside the transaction that
// makes the event true. A module may publish only its own events.
func (q *Queue) Publish(ctx context.Context, ev tors.Event) error {
	name := ev.EventName()
	if !tors.Owns(q.module, name) {
		return fmt.Errorf("модуль %s не может публиковать событие %s", q.module, name)
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	q.app.mu.RLock()
	kinds := q.app.subs[name]
	q.app.mu.RUnlock()
	for _, k := range kinds {
		if err := q.app.enqueue(ctx, k, jsontext.Value(body), time.Time{}); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers a durable subscriber. The name must stay stable: it is
// the job kind stored in the queue.
func Subscribe[E tors.Event](q *Queue, name string, fn func(context.Context, E) error) {
	kind := q.kind("on." + name)
	event := tors.EventNameOf[E]()
	q.app.mu.Lock()
	q.app.subs[event] = append(q.app.subs[event], kind)
	q.app.handlers[kind] = func(ctx context.Context, payload []byte) error {
		var e E
		if err := json.Unmarshal(payload, &e); err != nil {
			return fmt.Errorf("событие %s: %w", event, err)
		}
		return fn(ctx, e)
	}
	q.app.mu.Unlock()
}

func (a *App) enqueue(ctx context.Context, kind string, payload any, at time.Time) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if at.IsZero() {
		_, err = a.db.Q(ctx).Exec(ctx, "INSERT INTO jobs.jobs (kind, payload) VALUES ($1, $2)", kind, body)
	} else {
		_, err = a.db.Q(ctx).Exec(ctx, "INSERT INTO jobs.jobs (kind, payload, run_at) VALUES ($1, $2, $3)", kind, body, at)
	}
	if err != nil {
		return err
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}

func (a *App) work(ctx context.Context) {
	for {
		ran, err := a.runOne(ctx)
		if err != nil && ctx.Err() == nil {
			a.log.Error("очередь задач", "error", err)
		}
		if ran {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-time.After(time.Duration(a.PollInterval)):
		}
	}
}

// runOne claims the next due job, runs it and records the result. A claimed
// job is leased for five minutes; if the process dies, it runs again later.
func (a *App) runOne(ctx context.Context) (bool, error) {
	var (
		id       int64
		kind     string
		payload  []byte
		attempts int
		max      int
	)
	err := a.db.Q(ctx).QueryRow(ctx, `
		UPDATE jobs.jobs SET attempts = attempts + 1, run_at = now() + interval '5 minutes'
		WHERE id = (
			SELECT id FROM jobs.jobs
			WHERE done_at IS NULL AND NOT dead AND run_at <= now()
			ORDER BY run_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, payload, attempts, max_attempts`).Scan(&id, &kind, &payload, &attempts, &max)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	a.mu.RLock()
	fn := a.handlers[kind]
	a.mu.RUnlock()
	runErr := errors.New("нет обработчика задачи " + kind)
	if fn != nil {
		runErr = safeRun(ctx, fn, payload)
	}
	if runErr == nil {
		_, err = a.db.Q(ctx).Exec(ctx, "UPDATE jobs.jobs SET done_at = now(), last_error = NULL WHERE id = $1", id)
		return true, err
	}
	a.log.Error("задача не выполнена", "kind", kind, "attempt", attempts, "error", runErr)
	backoff := time.Duration(math.Min(math.Pow(2, float64(attempts)), 3600)) * time.Second
	_, err = a.db.Q(ctx).Exec(ctx, `UPDATE jobs.jobs SET last_error = $2, dead = attempts >= max_attempts,
		run_at = now() + $3::interval WHERE id = $1`, id, runErr.Error(), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
	return true, err
}

func safeRun(ctx context.Context, fn func(context.Context, []byte) error, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("паника: %v", r)
		}
	}()
	return fn(ctx, payload)
}

func (a *App) schedule(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		a.mu.RLock()
		tasks := append([]periodic(nil), a.periodic...)
		a.mu.RUnlock()
		for _, t := range tasks {
			a.runPeriodic(ctx, t)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// runPeriodic runs a task if it is due; the conditional update makes sure only
// one instance takes each run.
func (a *App) runPeriodic(ctx context.Context, t periodic) {
	q := a.db.Q(ctx)
	if _, err := q.Exec(ctx, "INSERT INTO jobs.periodic (name, next_run) VALUES ($1, now()) ON CONFLICT DO NOTHING", t.name); err != nil {
		return
	}
	tag, err := q.Exec(ctx, "UPDATE jobs.periodic SET next_run = now() + $2::interval WHERE name = $1 AND next_run <= now()",
		t.name, fmt.Sprintf("%d milliseconds", t.every.Milliseconds()))
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	if err := safeRun(ctx, func(c context.Context, _ []byte) error { return t.fn(c) }, nil); err != nil {
		a.log.Error("периодическая задача", "name", t.name, "error", err)
	}
}
