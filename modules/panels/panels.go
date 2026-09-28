// Package panels is the "panels" app: the VPN panels subscriptions live on.
// Each panel is a named instance of a provider module (panels.providers.*),
// so one bot can sell access on several panels, even of different kinds.
package panels

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
)

func init() { tors.RegisterModule(new(App)) }

// Account is what a subscription needs on a panel.
type Account struct {
	// Name is unique per subscription and stable: providers use it to find
	// an account again after a retry.
	Name        string
	TelegramID  int64
	ExpiresAt   time.Time
	TrafficGB   int // 0 — unlimited
	Devices     int // 0 — unlimited
	Targets     []string
	Description string
}

// Access is the account as the panel knows it.
type Access struct {
	Ref string // provider's id of the account
	URL string // subscription link for VPN apps
}

// Usage is the account's traffic.
type Usage struct {
	UsedBytes int64
}

// Provider is implemented by panel modules (panels.providers.*).
type Provider interface {
	// Ensure creates the account or updates it to match, returning its access.
	Ensure(ctx context.Context, ref string, a Account) (Access, error)
	// Revoke issues a new subscription link; the old one stops working.
	Revoke(ctx context.Context, ref string) (Access, error)
	Usage(ctx context.Context, ref string) (Usage, error)
	Delete(ctx context.Context, ref string) error
}

// App holds the configured panels.
type App struct {
	Providers map[string]jsontext.Value `json:"providers" tors:"namespace=panels.providers inline_key=provider"`

	panels map[string]Provider
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "panels", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name: "Панели", Summary: "Панели, на которых создаются подписки.", Category: "core", Hidden: true,
		Needs: []string{"panels.providers"},
	}
}

func (a *App) Provision(ctx tors.Context) error {
	if len(a.Providers) == 0 {
		return errors.New("не подключена ни одна панель — добавьте, например, Remnawave")
	}
	mods, err := ctx.LoadModule(a, "Providers")
	if err != nil {
		return err
	}
	a.panels = map[string]Provider{}
	for name, m := range mods.(map[string]any) {
		p, ok := m.(Provider)
		if !ok {
			return fmt.Errorf("панель %s: модуль не реализует panels.Provider", name)
		}
		a.panels[name] = p
	}
	return nil
}

// Panel returns a panel by name.
func (a *App) Panel(name string) (Provider, error) {
	if name == "" {
		name = a.Default()
	}
	p, ok := a.panels[name]
	if !ok {
		return nil, fmt.Errorf("панель %q не настроена", name)
	}
	return p, nil
}

// Default is the panel used when a plan names none: "main" or the only one.
func (a *App) Default() string {
	if _, ok := a.panels["main"]; ok || len(a.panels) != 1 {
		return "main"
	}
	for name := range a.panels {
		return name
	}
	return "main"
}

// Names lists the configured panels.
func (a *App) Names() []string {
	out := make([]string, 0, len(a.panels))
	for n := range a.panels {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
