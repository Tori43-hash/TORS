// Package catalog is the "catalog" app: the plans the bot sells, set in the
// config.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

// Plan is a subscription on sale.
type Plan struct {
	ID    string            `json:"id"`
	Title map[string]string `json:"title"`
	Days  int               `json:"days"`
	// TrafficGB and Devices limit the subscription; 0 means no limit.
	TrafficGB int `json:"traffic_gb,omitzero"`
	Devices   int `json:"devices,omitzero"`
	// Price in Telegram Stars.
	Price int `json:"price"`
	// Panel is the panel to create the subscription on; empty means the default.
	Panel string `json:"panel,omitzero"`
	// Targets are panel-specific groups, such as Remnawave internal squads.
	Targets []string `json:"targets,omitzero"`
}

// Name returns the plan title in a language.
func (p Plan) Name(lang string) string {
	if s := p.Title[lang]; s != "" {
		return s
	}
	for _, l := range []string{"ru", "en"} {
		if s := p.Title[l]; s != "" {
			return s
		}
	}
	for _, s := range p.Title {
		return s
	}
	return p.ID
}

// App holds the plans.
type App struct {
	Plans []Plan `json:"plans"`

	byID map[string]Plan
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "catalog", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Тарифы",
		Summary:  "Список тарифов: срок, трафик, устройства и цена в звёздах.",
		Category: "sales",
		Config: []market.Field{{
			Key: "plans", Title: "Тарифы", Type: "list", Required: true,
			Default: []any{
				map[string]any{"id": "month", "title": map[string]any{"ru": "1 месяц", "en": "1 month"}, "days": 30, "traffic_gb": 200, "devices": 3, "price": 150},
				map[string]any{"id": "year", "title": map[string]any{"ru": "12 месяцев", "en": "12 months"}, "days": 365, "devices": 5, "price": 1400},
			},
			Item: []market.Field{
				{Key: "id", Title: "Код", Type: "text", Required: true, Hint: "Латиница, не меняется после запуска"},
				{Key: "title", Title: "Название", Type: "i18n", Required: true},
				{Key: "days", Title: "Дней", Type: "number", Required: true},
				{Key: "traffic_gb", Title: "Трафик, ГБ", Type: "number", Hint: "0 — без ограничений"},
				{Key: "devices", Title: "Устройств", Type: "number", Hint: "0 — без ограничений"},
				{Key: "price", Title: "Цена, звёзд", Type: "number", Required: true},
				{Key: "panel", Title: "Панель", Type: "select", OptionsFrom: "panels"},
			},
		}},
	}
}

var planFields = []theme.Field{
	{Name: "ID", Type: "string", Title: "Код"},
	{Name: "Title", Type: "string", Title: "Название"},
	{Name: "Days", Type: "int", Title: "Дней"},
	{Name: "Traffic", Type: "bytes", Title: "Трафик (0 — без ограничений)"},
	{Name: "Devices", Type: "int", Title: "Устройств (0 — без ограничений)"},
	{Name: "Price", Type: "int", Title: "Цена, звёзд"},
}

var planSamples = []map[string]any{
	{"ID": "month", "Title": "1 месяц", "Days": 30, "Traffic": 214748364800, "Devices": 3, "Price": 150},
	{"ID": "year", "Title": "12 месяцев", "Days": 365, "Traffic": 0, "Devices": 5, "Price": 1400},
}

func (*App) UI() ui.Contribution {
	return ui.Contribution{Data: map[string]theme.DataSource{
		"catalog.plans": {Title: "Тарифы", List: true, Fields: planFields, Samples: planSamples},
		"catalog.plan":  {Title: "Тариф", Params: []string{"plan"}, Fields: planFields, Samples: planSamples[:1]},
	}}
}

var idRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func (a *App) Provision(ctx tors.Context) error {
	a.byID = map[string]Plan{}
	for i, p := range a.Plans {
		switch {
		case !idRe.MatchString(p.ID):
			return fmt.Errorf("тариф %d: id %q — латиница в нижнем регистре, цифры, _ и -", i+1, p.ID)
		case a.byID[p.ID].ID != "":
			return fmt.Errorf("тариф %s указан дважды", p.ID)
		case len(p.Title) == 0:
			return fmt.Errorf("тариф %s: нет названия", p.ID)
		case p.Days <= 0:
			return fmt.Errorf("тариф %s: days должно быть больше нуля", p.ID)
		case p.Price <= 0:
			return fmt.Errorf("тариф %s: price должна быть больше нуля", p.ID)
		case p.TrafficGB < 0 || p.Devices < 0:
			return fmt.Errorf("тариф %s: лимиты не могут быть отрицательными", p.ID)
		}
		a.byID[p.ID] = p
	}
	if len(a.Plans) == 0 {
		return errors.New("нет ни одного тарифа")
	}
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	return reg.Register(a.UI(), ui.Bindings{Data: map[string]ui.DataFunc{
		"catalog.plans": func(_ context.Context, c ui.Call) (any, error) {
			out := make([]any, len(a.Plans))
			for i, p := range a.Plans {
				out[i] = view(p, c.User.Lang)
			}
			return out, nil
		},
		"catalog.plan": func(_ context.Context, c ui.Call) (any, error) {
			p, ok := a.Plan(c.Params["plan"])
			if !ok {
				return nil, fmt.Errorf("тариф %q не найден", c.Params["plan"])
			}
			return view(p, c.User.Lang), nil
		},
	}})
}

func view(p Plan, lang string) map[string]any {
	return map[string]any{
		"ID": p.ID, "Title": p.Name(lang), "Days": p.Days, "Traffic": int64(p.TrafficGB) << 30,
		"Devices": p.Devices, "Price": p.Price,
	}
}

// Plan returns a plan by id.
func (a *App) Plan(id string) (Plan, bool) {
	p, ok := a.byID[id]
	return p, ok
}
