package tors

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// Instance is a running configuration: its apps in dependency order.
type Instance struct {
	cfg    *Config
	root   Context
	cancel context.CancelFunc
	logger *slog.Logger
	bus    *bus

	mu           sync.Mutex
	apps         map[string]any
	order        []string   // apps in the order their Provision finished
	provisioning []ModuleID // stack for cycle detection
	modules      []any      // every provisioned module, in order, for cleanup and health
	started      []string
}

// Load provisions every app of the config. On any error everything already
// provisioned is cleaned up and the error is returned.
func Load(cfg *Config) (*Instance, error) {
	if err := checkBuild(cfg.Build); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	inst := &Instance{
		cfg:    cfg,
		cancel: cancel,
		logger: newLogger(cfg.Logging),
		apps:   map[string]any{},
	}
	inst.bus = &bus{logger: inst.logger}
	inst.root = Context{Context: ctx, inst: inst}

	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if _, err := inst.app(name); err != nil {
			inst.cleanup()
			cancel()
			return nil, err
		}
	}
	return inst, nil
}

// Start starts the apps in dependency order; on error it stops those already
// started and cleans up.
func (i *Instance) Start() error {
	for _, name := range i.order {
		a, ok := i.apps[name].(App)
		if !ok {
			continue
		}
		if err := a.Start(); err != nil {
			_ = i.Stop()
			return fmt.Errorf("запуск %s: %w", name, err)
		}
		i.started = append(i.started, name)
	}
	i.logger.Info("бот запущен", "apps", strings.Join(i.order, ", "))
	return nil
}

// Stop stops started apps in reverse order and cleans everything up.
func (i *Instance) Stop() error {
	var errs []error
	for _, name := range slices.Backward(i.started) {
		if err := i.apps[name].(App).Stop(); err != nil {
			errs = append(errs, fmt.Errorf("остановка %s: %w", name, err))
		}
	}
	i.started = nil
	i.cancel()
	errs = append(errs, i.cleanup())
	return errors.Join(errs...)
}

// Run parses a JSON config, provisions and starts it.
func Run(configJSON []byte) (*Instance, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return nil, err
	}
	inst, err := Load(cfg)
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		return nil, err
	}
	return inst, nil
}

// Apps returns the provisioned apps in dependency order.
func (i *Instance) Apps() []string { return slices.Clone(i.order) }

// App returns a provisioned app by name.
func (i *Instance) App(name string) (any, bool) {
	v, ok := i.apps[name]
	return v, ok
}

// Health checks every module that implements HealthChecker.
func (i *Instance) Health(ctx context.Context) map[string]error {
	out := map[string]error{}
	for _, m := range i.modules {
		if h, ok := m.(HealthChecker); ok {
			out[string(m.(Module).TorsModule().ID)] = h.Health(ctx)
		}
	}
	return out
}

func (i *Instance) app(name string) (any, error) {
	i.mu.Lock()
	if v, ok := i.apps[name]; ok {
		i.mu.Unlock()
		return v, nil
	}
	i.mu.Unlock()
	info, err := GetModule(name)
	if err != nil {
		return nil, err
	}
	if info.ID.Namespace() != "" {
		return nil, fmt.Errorf("%s — не приложение верхнего уровня", name)
	}
	raw := i.cfg.Apps[name]
	v, err := i.load(i.root, info.ID, raw)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	i.apps[name] = v
	i.order = append(i.order, name)
	i.mu.Unlock()
	return v, nil
}

// load creates a module, decodes its config strictly, provisions and validates it.
func (i *Instance) load(parent Context, id ModuleID, raw jsontext.Value) (any, error) {
	if slices.Contains(i.provisioning, id) {
		chain := append(slices.Clone(i.provisioning), id)
		parts := make([]string, len(chain))
		for k, c := range chain {
			parts[k] = string(c)
		}
		return nil, fmt.Errorf("циклическая зависимость модулей: %s", strings.Join(parts, " → "))
	}
	info, err := GetModule(string(id))
	if err != nil {
		return nil, err
	}
	m := info.New()
	if !emptyObject(raw) {
		if err := json.Unmarshal(raw, m, json.RejectUnknownMembers(true)); err != nil {
			return nil, fmt.Errorf("конфигурация %s: %w", id, err)
		}
	}
	i.provisioning = append(i.provisioning, id)
	defer func() { i.provisioning = i.provisioning[:len(i.provisioning)-1] }()

	ctx := Context{Context: parent.Context, inst: i, module: id}
	if p, ok := m.(Provisioner); ok {
		if err := p.Provision(ctx); err != nil {
			if c, ok := m.(CleanerUpper); ok {
				_ = c.Cleanup()
			}
			return nil, fmt.Errorf("%s: %w", id, err)
		}
	}
	i.mu.Lock()
	i.modules = append(i.modules, m)
	i.mu.Unlock()
	if v, ok := m.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
	}
	return m, nil
}

func (i *Instance) cleanup() error {
	var errs []error
	for _, m := range slices.Backward(i.modules) {
		if c, ok := m.(CleanerUpper); ok {
			if err := c.Cleanup(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", m.(Module).TorsModule().ID, err))
			}
		}
	}
	i.modules = nil
	return errors.Join(errs...)
}

// checkBuild verifies that the modules the config was built for are compiled in.
func checkBuild(b *Build) error {
	if b == nil {
		return nil
	}
	have := map[string]bool{}
	for _, m := range Modules() {
		have[m.PackagePath()] = true
	}
	var missing []string
	for _, src := range b.Modules {
		found := false
		for p := range have {
			if p == src.Source || strings.HasPrefix(p, src.Source+"/") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, src.Source)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("бинарь собран без модулей %s — соберите его командой `tors build` с этим конфигом", strings.Join(missing, ", "))
	}
	return nil
}

// emptyObject reports whether raw is absent or {}: modules without settings
// accept an empty config even if they have no exported fields.
func emptyObject(raw jsontext.Value) bool {
	if len(raw) == 0 {
		return true
	}
	var m map[string]jsontext.Value
	return json.Unmarshal(raw, &m) == nil && len(m) == 0
}
