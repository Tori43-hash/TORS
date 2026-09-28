package tors_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tori43-hash/tors"
)

// trace records lifecycle calls across test modules.
var trace struct {
	sync.Mutex
	calls []string
}

func record(s string) {
	trace.Lock()
	trace.calls = append(trace.calls, s)
	trace.Unlock()
}

func calls() []string {
	trace.Lock()
	defer trace.Unlock()
	out := slices.Clone(trace.calls)
	trace.calls = nil
	return out
}

// testapp_a depends on testapp_b.
type appA struct {
	Name string `json:"name"`
}

func (appA) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testapp_a", New: func() tors.Module { return new(appA) }}
}
func (a *appA) Provision(ctx tors.Context) error {
	if _, err := ctx.App("testapp_b"); err != nil {
		return err
	}
	record("provision a")
	return nil
}
func (a *appA) Start() error   { record("start a"); return nil }
func (a *appA) Stop() error    { record("stop a"); return nil }
func (a *appA) Cleanup() error { record("cleanup a"); return nil }

type appB struct{}

func (appB) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testapp_b", New: func() tors.Module { return new(appB) }}
}
func (b *appB) Provision(tors.Context) error { record("provision b"); return nil }
func (b *appB) Start() error                 { record("start b"); return nil }
func (b *appB) Stop() error                  { record("stop b"); return nil }
func (b *appB) Cleanup() error               { record("cleanup b"); return nil }

// testapp_fail fails to provision after its dependency is up.
type appFail struct{}

func (appFail) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testapp_fail", New: func() tors.Module { return new(appFail) }}
}
func (f *appFail) Provision(ctx tors.Context) error {
	if _, err := ctx.App("testapp_b"); err != nil {
		return err
	}
	return errors.New("boom")
}

// testcycle_c ⇄ testcycle_d.
type cycC struct{}

func (cycC) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testcycle_c", New: func() tors.Module { return new(cycC) }}
}
func (c *cycC) Provision(ctx tors.Context) error { _, err := ctx.App("testcycle_d"); return err }

type cycD struct{}

func (cycD) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testcycle_d", New: func() tors.Module { return new(cycD) }}
}
func (d *cycD) Provision(ctx tors.Context) error { _, err := ctx.App("testcycle_c"); return err }

// testhost loads guests from "testhost.guests" in three forms.
type host struct {
	One    jsontext.Value            `json:"one,omitzero" tors:"namespace=testhost.guests inline_key=kind"`
	List   []jsontext.Value          `json:"list,omitzero" tors:"namespace=testhost.guests inline_key=kind"`
	Named  map[string]jsontext.Value `json:"named,omitzero" tors:"namespace=testhost.guests inline_key=kind"`
	one    *guest
	list   []any
	named  map[string]any
	events *tors.Events
}

func (host) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testhost", New: func() tors.Module { return new(host) }}
}
func (h *host) Provision(ctx tors.Context) error {
	one, err := ctx.LoadModule(h, "One")
	if err != nil {
		return err
	}
	h.one, _ = one.(*guest)
	if v, err := ctx.LoadModule(h, "List"); err != nil {
		return err
	} else {
		h.list = v.([]any)
	}
	if v, err := ctx.LoadModule(h, "Named"); err != nil {
		return err
	} else {
		h.named = v.(map[string]any)
	}
	h.events = ctx.Events()
	return nil
}

type guest struct {
	Word string `json:"word"`
}

func (guest) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "testhost.guests.echo", New: func() tors.Module { return new(guest) }}
}
func (g *guest) Validate() error {
	if g.Word == "" {
		return errors.New("word is required")
	}
	return nil
}

func init() {
	for _, m := range []tors.Module{appA{}, appB{}, appFail{}, cycC{}, cycD{}, host{}, guest{}} {
		tors.RegisterModule(m)
	}
}

func load(t *testing.T, cfg string) (*tors.Instance, error) {
	t.Helper()
	c, err := tors.ParseConfig([]byte(cfg))
	if err != nil {
		return nil, err
	}
	return tors.Load(c)
}

func TestLifecycleOrder(t *testing.T) {
	calls()
	inst, err := load(t, `{"apps": {"testapp_a": {"name": "x"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	if err := inst.Stop(); err != nil {
		t.Fatal(err)
	}
	want := []string{"provision b", "provision a", "start b", "start a", "stop a", "stop b", "cleanup a", "cleanup b"}
	if got := calls(); !slices.Equal(got, want) {
		t.Fatalf("calls:\n got %v\nwant %v", got, want)
	}
	if got := inst.Apps(); !slices.Equal(got, []string{"testapp_b", "testapp_a"}) {
		t.Fatalf("apps order %v", got)
	}
}

func TestRollbackOnProvisionError(t *testing.T) {
	calls()
	_, err := load(t, `{"apps": {"testapp_fail": {}}}`)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if got := calls(); !slices.Equal(got, []string{"provision b", "cleanup b"}) {
		t.Fatalf("calls %v", got)
	}
}

func TestCycle(t *testing.T) {
	_, err := load(t, `{"apps": {"testcycle_c": {}}}`)
	if err == nil || !strings.Contains(err.Error(), "testcycle_c → testcycle_d → testcycle_c") {
		t.Fatalf("err = %v", err)
	}
}

func TestStrictConfig(t *testing.T) {
	if _, err := load(t, `{"apps": {"testapp_a": {"nmae": "x"}}}`); err == nil {
		t.Fatal("unknown app field accepted")
	}
	if _, err := load(t, `{"aps": {}}`); err == nil {
		t.Fatal("unknown top-level field accepted")
	}
	if _, err := load(t, `{"apps": {"nope": {}}}`); err == nil || !strings.Contains(err.Error(), "не вкомпилирован") {
		t.Fatalf("unknown module: %v", err)
	}
}

func TestLoadModuleForms(t *testing.T) {
	inst, err := load(t, `{"apps": {"testhost": {
		"one": {"kind": "echo", "word": "a"},
		"list": [{"kind": "echo", "word": "b"}, {"kind": "echo", "word": "c"}],
		"named": {"x": {"kind": "echo", "word": "d"}}
	}}}`)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := inst.App("testhost")
	h := v.(*host)
	if h.one.Word != "a" || len(h.list) != 2 || h.list[1].(*guest).Word != "c" || h.named["x"].(*guest).Word != "d" {
		t.Fatalf("guests: %+v %+v %+v", h.one, h.list, h.named)
	}
	for _, bad := range []string{
		`{"apps": {"testhost": {"one": {"word": "a"}}}}`,
		`{"apps": {"testhost": {"one": {"kind": "nope"}}}}`,
		`{"apps": {"testhost": {"one": {"kind": "echo"}}}}`,
		`{"apps": {"testhost": {"one": {"kind": "echo", "word": "a", "extra": 1}}}}`,
	} {
		if _, err := load(t, bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	t.Setenv("TORS_TEST_TOKEN", "secret-123")
	dir := t.TempDir()
	f := filepath.Join(dir, "pass")
	if err := os.WriteFile(f, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := `{"a": "{env.TORS_TEST_TOKEN}", "b": "x-{file.` + f + `}", "n": 9007199254740993, "t": "{{ .User.FirstName }}"}`
	out, err := tors.ReplacePlaceholders([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"secret-123"`, `"x-from-file"`, `9007199254740993`, `"{{ .User.FirstName }}"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if _, err := tors.ReplacePlaceholders([]byte(`{"a": "{env.TORS_TEST_MISSING}"}`)); err == nil {
		t.Error("missing env accepted")
	}
}

func TestDurationAndSize(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "1d12h": 36 * time.Hour, "2w": 14 * 24 * time.Hour, "90m": 90 * time.Minute} {
		if got, err := tors.ParseDuration(in); err != nil || got != want {
			t.Errorf("%s → %v, %v", in, got, err)
		}
	}
	for in, want := range map[string]int64{"100GiB": 100 << 30, "10GB": 10e9, "0": 0, "1.5 MiB": 3 << 19} {
		if got, err := tors.ParseSize(in); err != nil || got != want {
			t.Errorf("%s → %v, %v", in, got, err)
		}
	}
	if _, err := tors.ParseDuration("soon"); err == nil {
		t.Error("bad duration accepted")
	}
}

type paid struct{ Order int }

func (paid) EventName() string { return "testhost.order.paid" }

type foreign struct{}

func (foreign) EventName() string { return "billing.order.paid" }

func TestEvents(t *testing.T) {
	inst, err := load(t, `{"apps": {"testhost": {}}}`)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := inst.App("testhost")
	ev := v.(*host).events
	var got []string
	tors.On(ev, func(_ context.Context, e paid) error { got = append(got, "typed"); return nil })
	ev.OnPattern("testhost.*", func(context.Context, tors.Envelope) error { panic("handler bug") })
	off := ev.OnPattern("*", func(_ context.Context, e tors.Envelope) error { got = append(got, "all:"+string(e.Origin)); return nil })
	if err := ev.Emit(context.Background(), paid{Order: 1}); err != nil {
		t.Fatal(err)
	}
	off()
	_ = ev.Emit(context.Background(), paid{})
	if !slices.Equal(got, []string{"typed", "all:testhost", "typed"}) {
		t.Fatalf("delivered %v", got)
	}
	if err := ev.Emit(context.Background(), foreign{}); err == nil {
		t.Fatal("module emitted an event of another module")
	}
	ev.OnPattern("testhost.order.paid", func(context.Context, tors.Envelope) error { return tors.ErrAbort })
	if err := ev.Emit(context.Background(), paid{}); !errors.Is(err, tors.ErrAbort) {
		t.Fatalf("abort: %v", err)
	}
}

func TestBuildCheck(t *testing.T) {
	_, err := load(t, `{"build": {"modules": [{"source": "github.com/acme/tors-missing"}]}, "apps": {}}`)
	if err == nil || !strings.Contains(err.Error(), "github.com/acme/tors-missing") {
		t.Fatalf("err = %v", err)
	}
	if _, err := load(t, `{"build": {"modules": [{"source": "github.com/tori43-hash/tors_test"}]}, "apps": {}}`); err != nil {
		t.Fatalf("own module path rejected: %v", err)
	}
}

func TestRegisterRejectsBadIDs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	tors.RegisterModule(appB{})
}
