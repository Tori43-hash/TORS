// Package fake is a panel that keeps accounts in memory: for tests and for
// trying a bot out before a real panel is set up.
package fake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/panels"
)

func init() { tors.RegisterModule(new(Panel)) }

// Panel is an in-memory panel.
type Panel struct {
	// URL is the base of subscription links.
	URL string `json:"url,omitzero"`

	mu       sync.Mutex
	accounts map[string]*Stored
	next     int
}

// Stored is an account kept by the fake panel.
type Stored struct {
	panels.Account
	Token string
	Used  int64
}

func (*Panel) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "panels.providers.fake", New: func() tors.Module { return new(Panel) }}
}

func (*Panel) Market() market.Info {
	return market.Info{
		Name:     "Тестовая панель",
		Summary:  "Хранит подписки в памяти. Чтобы попробовать бота до подключения настоящей панели.",
		Category: "panel",
		Requires: []string{"panels"},
		Host:     &market.Host{App: "panels", Field: "providers", Key: "provider", Named: true},
	}
}

func (p *Panel) Provision(tors.Context) error {
	if p.URL == "" {
		p.URL = "https://sub.example.com/"
	}
	p.accounts = map[string]*Stored{}
	return nil
}

func (p *Panel) Ensure(_ context.Context, ref string, a panels.Account) (panels.Access, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ref == "" {
		for r, s := range p.accounts {
			if s.Name == a.Name {
				ref = r
			}
		}
	}
	s, ok := p.accounts[ref]
	if !ok {
		if ref != "" {
			return panels.Access{}, fmt.Errorf("аккаунт %s не найден", ref)
		}
		p.next++
		ref = fmt.Sprint(p.next)
		s = &Stored{Token: token()}
		p.accounts[ref] = s
	}
	s.Account = a
	return panels.Access{Ref: ref, URL: p.URL + s.Token}, nil
}

func (p *Panel) Revoke(_ context.Context, ref string) (panels.Access, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.accounts[ref]
	if !ok {
		return panels.Access{}, fmt.Errorf("аккаунт %s не найден", ref)
	}
	s.Token = token()
	return panels.Access{Ref: ref, URL: p.URL + s.Token}, nil
}

func (p *Panel) Usage(_ context.Context, ref string) (panels.Usage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.accounts[ref]
	if !ok {
		return panels.Usage{}, fmt.Errorf("аккаунт %s не найден", ref)
	}
	return panels.Usage{UsedBytes: s.Used}, nil
}

func (p *Panel) Delete(_ context.Context, ref string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.accounts, ref)
	return nil
}

// Accounts returns a copy of the accounts, for tests.
func (p *Panel) Accounts() map[string]Stored {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]Stored, len(p.accounts))
	for r, s := range p.accounts {
		out[r] = *s
	}
	return out
}

func token() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
