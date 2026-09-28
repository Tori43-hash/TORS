package kv_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/internal/pgtest"
	"github.com/tori43-hash/tors/modules/kv"
	_ "github.com/tori43-hash/tors/modules/kv/memory"
	_ "github.com/tori43-hash/tors/modules/kv/postgres"
)

type user struct{ bucket *kv.Bucket }

var mod = new(user)

func (*user) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testkv", New: func() tors.Module { return mod }}
}
func (u *user) Provision(ctx tors.Context) (err error) { u.bucket, err = kv.Get(ctx); return err }

func init() { tors.RegisterModule(mod) }

func exercise(t *testing.T, cfg string) {
	t.Helper()
	inst, err := tors.Run([]byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop()
	ctx := context.Background()
	b := mod.bucket
	if _, ok, _ := b.Get(ctx, "a"); ok {
		t.Fatal("empty store has a value")
	}
	if err := kv.SetJSON(ctx, b, "a", map[string]int{"n": 1}, 0); err != nil {
		t.Fatal(err)
	}
	v, ok, err := kv.GetJSON[map[string]int](ctx, b, "a")
	if err != nil || !ok || v["n"] != 1 {
		t.Fatalf("get: %v %v %v", v, ok, err)
	}
	_ = b.Set(ctx, "short", []byte("x"), 50*time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	if _, ok, _ := b.Get(ctx, "short"); ok {
		t.Fatal("expired value returned")
	}
	_ = b.Delete(ctx, "a")
	if _, ok, _ := b.Get(ctx, "a"); ok {
		t.Fatal("deleted value returned")
	}
}

func TestMemory(t *testing.T) { exercise(t, `{"apps": {"testkv": {}}}`) }

func TestPostgres(t *testing.T) {
	dsn := pgtest.DSN(t)
	exercise(t, fmt.Sprintf(`{"apps": {"database": {"dsn": %q}, "kv": {"store": {"store": "postgres"}}, "testkv": {}}}`, dsn))
}
