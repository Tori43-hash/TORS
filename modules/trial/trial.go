// Package trial is the "trial" app: a free subscription, once per user.
package trial

import (
	"context"
	_ "embed"
	"strconv"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/catalog"
	"github.com/tori43-hash/tors/modules/subscriptions"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/modules/users"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

//go:embed screens.json
var screens []byte

// App gives trial subscriptions.
type App struct {
	Days      int      `json:"days,omitzero"`
	TrafficGB int      `json:"traffic_gb,omitzero"`
	Devices   int      `json:"devices,omitzero"`
	Panel     string   `json:"panel,omitzero"`
	Targets   []string `json:"targets,omitzero"`

	subs  *subscriptions.App
	users *users.App
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "trial", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Пробный период",
		Summary:  "Бесплатная подписка на несколько дней, один раз на человека.",
		Category: "feature",
		Requires: []string{"subscriptions"},
		Config: []market.Field{
			{Key: "days", Title: "Дней", Type: "number", Default: 3, Required: true},
			{Key: "traffic_gb", Title: "Трафик, ГБ", Type: "number", Default: 10, Hint: "0 — без ограничений"},
			{Key: "devices", Title: "Устройств", Type: "number", Default: 1, Hint: "0 — без ограничений"},
			{Key: "panel", Title: "Панель", Type: "select", OptionsFrom: "panels"},
		},
	}
}

func (*App) UI() ui.Contribution {
	s, err := theme.ParseScreens(screens)
	if err != nil {
		panic(err)
	}
	return ui.Contribution{
		Actions: map[string]theme.Action{
			"trial.start": {
				Title: "Начать пробный период", Label: theme.Text{"ru": "Попробовать бесплатно", "en": "Try for free"},
				Outcomes: []theme.Outcome{
					{Name: "ok", Title: "Выдан", Default: "trial_ok", Params: []string{"id"}},
					{Name: "used", Title: "Уже был", Default: "trial_used"},
				},
			},
		},
		Conditions: map[string]theme.Condition{
			"trial.available": {Title: "Пробный период ещё не брали", Sample: true},
		},
		Screens: s,
	}
}

func (a *App) Provision(ctx tors.Context) error {
	if a.Days <= 0 {
		a.Days = 3
	}
	var err error
	if a.subs, err = tors.AppAs[*subscriptions.App](ctx, "subscriptions"); err != nil {
		return err
	}
	if a.users, err = tors.AppAs[*users.App](ctx, "users"); err != nil {
		return err
	}
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	return reg.Register(a.UI(), ui.Bindings{
		Actions: map[string]ui.ActionFunc{"trial.start": a.start},
		Conditions: map[string]ui.ConditionFunc{
			"trial.available": func(ctx context.Context, c ui.Call) (bool, error) {
				used, err := a.subs.Granted(ctx, key(c.User.ID))
				return !used, err
			},
		},
	})
}

func key(user int64) string { return "trial:" + strconv.FormatInt(user, 10) }

func (a *App) start(ctx context.Context, c ui.Call) (ui.Result, error) {
	used, err := a.subs.Granted(ctx, key(c.User.ID))
	if err != nil || used {
		return ui.Result{Outcome: "used"}, err
	}
	u, err := a.users.Get(ctx, c.User.ID)
	if err != nil {
		return ui.Result{}, err
	}
	s, err := a.subs.Grant(ctx, subscriptions.Grant{User: u, Key: key(u.ID), Plan: catalog.Plan{
		ID: "trial", Title: map[string]string{"ru": "Пробный период", "en": "Free trial"},
		Days: a.Days, TrafficGB: a.TrafficGB, Devices: a.Devices, Panel: a.Panel, Targets: a.Targets,
	}})
	if err != nil {
		return ui.Result{}, err
	}
	return ui.Result{Outcome: "ok", Params: map[string]string{"id": strconv.FormatInt(s.ID, 10)}}, nil
}
