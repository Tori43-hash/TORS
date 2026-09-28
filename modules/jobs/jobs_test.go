package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/internal/pgtest"
	"github.com/tori43-hash/tors/modules/database"
	"github.com/tori43-hash/tors/modules/jobs"
)

type orderPaid struct {
	Order int `json:"order"`
}

func (orderPaid) EventName() string { return "testshop.order.paid" }

// testshop is a module that enqueues jobs and publishes events.
type shop struct {
	db    *database.DB
	q     *jobs.Queue
	fails atomic.Int32
	mu    sync.Mutex
	seen  []string
}

func (*shop) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testshop", New: func() tors.Module { return current }}
}

var current = new(shop)

func (s *shop) Provision(ctx tors.Context) error {
	var err error
	if s.db, err = database.Get(ctx); err != nil {
		return err
	}
	if s.q, err = jobs.Get(ctx); err != nil {
		return err
	}
	s.q.Handle("flaky", func(_ context.Context, p []byte) error {
		if s.fails.Add(-1) >= 0 {
			return errors.New("temporary")
		}
		s.note("flaky:" + string(p))
		return nil
	})
	s.q.Handle("plain", func(_ context.Context, p []byte) error { s.note("plain:" + string(p)); return nil })
	jobs.Subscribe(s.q, "grant", func(_ context.Context, e orderPaid) error { s.note(fmt.Sprintf("grant:%d", e.Order)); return nil })
	jobs.Subscribe(s.q, "notify", func(_ context.Context, e orderPaid) error { s.note(fmt.Sprintf("notify:%d", e.Order)); return nil })
	s.q.Every("tick", time.Hour, func(context.Context) error { s.note("tick"); return nil })
	return nil
}

func (s *shop) note(v string) {
	s.mu.Lock()
	s.seen = append(s.seen, v)
	s.mu.Unlock()
}

func (s *shop) saw(v string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.seen {
		if x == v {
			return true
		}
	}
	return false
}

func init() { tors.RegisterModule(current) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestQueue(t *testing.T) {
	dsn := pgtest.DSN(t)
	inst, err := tors.Run([]byte(fmt.Sprintf(`{"apps": {"database": {"dsn": %q}, "jobs": {"poll_interval": "100ms"}, "testshop": {}}}`, dsn)))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop()
	s := current
	ctx := context.Background()

	if err := s.q.Enqueue(ctx, "plain", "hello"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.saw(`plain:"hello"`) })

	// Rolled-back transactions leave no jobs behind; committed ones run.
	_ = s.db.InTx(ctx, func(ctx context.Context) error {
		_ = s.q.Enqueue(ctx, "plain", "rolled back")
		return errors.New("abort")
	})
	if err := s.db.InTx(ctx, func(ctx context.Context) error {
		return s.q.Publish(ctx, orderPaid{Order: 7})
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.saw("grant:7") && s.saw("notify:7") })
	if s.saw(`plain:"rolled back"`) {
		t.Fatal("job from a rolled back transaction ran")
	}

	// A failing job is retried until it succeeds.
	s.fails.Store(1)
	if err := s.q.Enqueue(ctx, "flaky", 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.saw("flaky:1") })

	waitFor(t, func() bool { return s.saw("tick") })

	if err := s.q.Publish(ctx, foreignEvent{}); err == nil {
		t.Fatal("module published another module's event")
	}
}

type foreignEvent struct{}

func (foreignEvent) EventName() string { return "billing.order.paid" }
