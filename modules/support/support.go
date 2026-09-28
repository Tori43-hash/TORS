// Package support is the "support" app: how to reach the people behind the bot.
package support

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

//go:embed screens.json
var screens []byte

// App holds the support contact.
type App struct {
	// Contact is a Telegram username (@support) or a link.
	Contact string `json:"contact"`

	url string
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "support", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Поддержка",
		Summary:  "Контакт поддержки для экранов бота.",
		Category: "feature",
		Config: []market.Field{
			{Key: "contact", Title: "Контакт", Type: "text", Placeholder: "@support", Required: true,
				Hint: "Username в Telegram или ссылка"},
		},
	}
}

func (*App) UI() ui.Contribution {
	s, err := theme.ParseScreens(screens)
	if err != nil {
		panic(err)
	}
	return ui.Contribution{
		Data: map[string]theme.DataSource{
			"support.info": {Title: "Поддержка", Fields: []theme.Field{
				{Name: "Contact", Type: "string", Title: "Контакт", Sample: "@support"},
				{Name: "URL", Type: "url", Title: "Ссылка", Sample: "https://t.me/support"},
			}},
		},
		Screens: s,
	}
}

func (a *App) Provision(ctx tors.Context) error {
	c := strings.TrimSpace(a.Contact)
	switch {
	case c == "":
		return errors.New("не указан contact")
	case strings.HasPrefix(c, "https://") || strings.HasPrefix(c, "http://"):
		a.url = c
	default:
		a.Contact = "@" + strings.TrimPrefix(c, "@")
		a.url = "https://t.me/" + strings.TrimPrefix(c, "@")
	}
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	return reg.Register(a.UI(), ui.Bindings{Data: map[string]ui.DataFunc{
		"support.info": func(context.Context, ui.Call) (any, error) {
			return map[string]any{"Contact": a.Contact, "URL": a.url}, nil
		},
	}})
}
