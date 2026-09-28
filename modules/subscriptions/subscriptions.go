// Package subscriptions is the "subscriptions" app: VPN subscriptions of
// users, several per user. A paid order grants or extends one on its panel;
// reminders go out before a subscription ends.
package subscriptions

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/billing"
	"github.com/tori43-hash/tors/modules/catalog"
	"github.com/tori43-hash/tors/modules/database"
	"github.com/tori43-hash/tors/modules/jobs"
	"github.com/tori43-hash/tors/modules/panels"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/modules/users"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed screens.json
var screens []byte

// Subscription is one VPN subscription.
type Subscription struct {
	ID        int64
	UserID    int64
	Plan      string
	Panel     string
	Ref       string
	URL       string
	Label     string
	Status    string // pending | active | expired
	ExpiresAt time.Time
	TrafficGB int
	Devices   int
}

// Grant asks for a subscription: a new one, or more days on an existing one.
type Grant struct {
	User users.User
	Plan catalog.Plan
	// Key makes the grant idempotent: "order:17", "trial:5".
	Key string
	// Extend is the subscription to extend; 0 creates a new one.
	Extend int64
}

// Events shown to users.
type (
	Paid struct {
		User      int64     `json:"user"`
		ID        int64     `json:"id"`
		Label     string    `json:"label"`
		ExpiresAt time.Time `json:"expires_at"`
		URL       string    `json:"url"`
	}
	Expiring struct {
		User      int64     `json:"user"`
		ID        int64     `json:"id"`
		Label     string    `json:"label"`
		ExpiresAt time.Time `json:"expires_at"`
		DaysLeft  int       `json:"days_left"`
	}
	Expired struct {
		User  int64  `json:"user"`
		ID    int64  `json:"id"`
		Label string `json:"label"`
	}
)

func (Paid) EventName() string      { return "subscriptions.paid" }
func (e Paid) Recipient() int64     { return e.User }
func (Expiring) EventName() string  { return "subscriptions.expiring" }
func (e Expiring) Recipient() int64 { return e.User }
func (Expired) EventName() string   { return "subscriptions.expired" }
func (e Expired) Recipient() int64  { return e.User }
func (e Paid) EventData() map[string]any {
	return map[string]any{"ID": e.ID, "Label": e.Label, "ExpiresAt": e.ExpiresAt, "URL": e.URL}
}
func (e Expiring) EventData() map[string]any {
	return map[string]any{"ID": e.ID, "Label": e.Label, "ExpiresAt": e.ExpiresAt, "DaysLeft": int64(e.DaysLeft)}
}
func (e Expired) EventData() map[string]any {
	return map[string]any{"ID": e.ID, "Label": e.Label}
}

// App keeps subscriptions.
type App struct {
	// RemindDays is how many days before the end to remind; 0 turns reminders off.
	RemindDays *int `json:"remind_days,omitzero"`

	db      *database.DB
	q       *jobs.Queue
	panels  *panels.App
	catalog *catalog.App
	billing *billing.App
	users   *users.App
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "subscriptions", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Подписки",
		Summary:  "Подписки пользователей: выдача после оплаты, продление, ссылка, напоминания.",
		Category: "sales",
		Requires: []string{"billing", "catalog", "panels"},
		Needs:    []string{"panels.providers"},
		Config: []market.Field{{
			Key: "remind_days", Title: "Напоминать за, дней", Type: "number", Default: 3,
			Hint: "0 — не напоминать",
		}},
	}
}

var subFields = []theme.Field{
	{Name: "ID", Type: "int", Title: "Номер"},
	{Name: "Label", Type: "string", Title: "Название"},
	{Name: "Status", Type: "string", Title: "Статус: active, expired"},
	{Name: "ExpiresAt", Type: "time", Title: "Действует до"},
	{Name: "Limit", Type: "bytes", Title: "Лимит трафика (0 — без ограничений)"},
	{Name: "Devices", Type: "int", Title: "Лимит устройств (0 — без ограничений)"},
	{Name: "URL", Type: "url", Title: "Ссылка подписки"},
}

var subSample = map[string]any{
	"ID": 17, "Label": "Телефон", "Status": "active", "ExpiresAt": "2026-10-28T12:00:00Z",
	"Limit": 214748364800, "Devices": 3, "URL": "https://sub.example.com/Xk2p9QaL", "Used": 13421772800,
}

func (*App) UI() ui.Contribution {
	s, err := theme.ParseScreens(screens)
	if err != nil {
		panic(err)
	}
	get := append(append([]theme.Field(nil), subFields...), theme.Field{Name: "Used", Type: "bytes", Title: "Израсходовано"})
	second := map[string]any{"ID": 21, "Label": "Ноутбук", "Status": "expired", "ExpiresAt": "2026-09-01T12:00:00Z",
		"Limit": 0, "Devices": 0, "URL": "https://sub.example.com/Pq81LmZe"}
	return ui.Contribution{
		Data: map[string]theme.DataSource{
			"subscriptions.list": {Title: "Мои подписки", List: true, Fields: subFields, Samples: []map[string]any{subSample, second}},
			"subscriptions.get":  {Title: "Подписка", Params: []string{"id"}, Fields: get, Samples: []map[string]any{subSample}},
		},
		Actions: map[string]theme.Action{
			"subscriptions.renew": {
				Title: "Продлить", Label: theme.Text{"ru": "Продлить", "en": "Renew"}, Params: []string{"id"},
				Outcomes: []theme.Outcome{{Name: "invoice", Title: "Отправлен счёт", Terminal: true}},
			},
			"subscriptions.rename": {
				Title: "Переименовать", Params: []string{"id", "label"}, Input: true,
				Outcomes: []theme.Outcome{
					{Name: "ok", Title: "Готово", Default: "sub"},
					{Name: "invalid", Title: "Не подходит", Default: "sub_rename"},
				},
			},
			"subscriptions.revoke": {
				Title: "Новая ссылка", Params: []string{"id"},
				Outcomes: []theme.Outcome{{Name: "ok", Title: "Готово", Default: "sub"}},
			},
		},
		Conditions: map[string]theme.Condition{
			"subscriptions.any": {Title: "Есть подписки", Sample: true},
		},
		Events: map[string]theme.Event{
			"subscriptions.paid": {Title: "Оплата прошла", Default: "sub_paid", Fields: []theme.Field{
				{Name: "ID", Type: "int", Title: "Номер", Sample: 17},
				{Name: "Label", Type: "string", Title: "Название", Sample: "Телефон"},
				{Name: "ExpiresAt", Type: "time", Title: "Действует до", Sample: "2026-10-28T12:00:00Z"},
				{Name: "URL", Type: "url", Title: "Ссылка подписки", Sample: "https://sub.example.com/Xk2p9QaL"},
			}},
			"subscriptions.expiring": {Title: "Скоро закончится", Default: "sub_expiring", Fields: []theme.Field{
				{Name: "ID", Type: "int", Title: "Номер", Sample: 17},
				{Name: "Label", Type: "string", Title: "Название", Sample: "Телефон"},
				{Name: "ExpiresAt", Type: "time", Title: "Действует до", Sample: "2026-10-01T12:00:00Z"},
				{Name: "DaysLeft", Type: "int", Title: "Осталось дней", Sample: 3},
			}},
			"subscriptions.expired": {Title: "Закончилась", Default: "sub_expired", Fields: []theme.Field{
				{Name: "ID", Type: "int", Title: "Номер", Sample: 21},
				{Name: "Label", Type: "string", Title: "Название", Sample: "Ноутбук"},
			}},
		},
		Screens: s,
	}
}

func (a *App) Provision(ctx tors.Context) error {
	if a.RemindDays == nil {
		a.RemindDays = new(int)
		*a.RemindDays = 3
	}
	var err error
	if a.db, err = database.Get(ctx); err != nil {
		return err
	}
	if err := a.db.Migrate(ctx, migrations, "migrations"); err != nil {
		return err
	}
	if a.q, err = jobs.Get(ctx); err != nil {
		return err
	}
	if a.panels, err = tors.AppAs[*panels.App](ctx, "panels"); err != nil {
		return err
	}
	if a.catalog, err = tors.AppAs[*catalog.App](ctx, "catalog"); err != nil {
		return err
	}
	if a.billing, err = tors.AppAs[*billing.App](ctx, "billing"); err != nil {
		return err
	}
	if a.users, err = tors.AppAs[*users.App](ctx, "users"); err != nil {
		return err
	}
	jobs.Subscribe(a.q, "grant", a.onPaid)
	a.q.Every("remind", time.Hour, a.remind)
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	ui.Deliver[Paid](reg)
	ui.Deliver[Expiring](reg)
	ui.Deliver[Expired](reg)
	return reg.Register(a.UI(), ui.Bindings{
		Data: map[string]ui.DataFunc{
			"subscriptions.list": func(ctx context.Context, c ui.Call) (any, error) {
				subs, err := a.List(ctx, c.User.ID)
				if err != nil {
					return nil, err
				}
				out := make([]any, len(subs))
				for i, s := range subs {
					out[i] = view(s)
				}
				return out, nil
			},
			"subscriptions.get": func(ctx context.Context, c ui.Call) (any, error) {
				s, err := a.own(ctx, c)
				if err != nil {
					return nil, err
				}
				v := view(s)
				v["Used"] = int64(0)
				if p, err := a.panels.Panel(s.Panel); err == nil && s.Ref != "" {
					if u, err := p.Usage(ctx, s.Ref); err == nil {
						v["Used"] = u.UsedBytes
					}
				}
				return v, nil
			},
		},
		Actions: map[string]ui.ActionFunc{
			"subscriptions.renew": func(ctx context.Context, c ui.Call) (ui.Result, error) {
				s, err := a.own(ctx, c)
				if err != nil {
					return ui.Result{}, err
				}
				p, ok := a.catalog.Plan(s.Plan)
				if !ok {
					return ui.Result{}, fmt.Errorf("тариф %s подписки %d больше не продаётся", s.Plan, s.ID)
				}
				_, err = a.billing.Checkout(ctx, billing.Checkout{User: c.User, Plan: p, Meta: map[string]string{"extend": strconv.FormatInt(s.ID, 10)}})
				return ui.Result{Outcome: "invoice"}, err
			},
			"subscriptions.rename": func(ctx context.Context, c ui.Call) (ui.Result, error) {
				s, err := a.own(ctx, c)
				if err != nil {
					return ui.Result{}, err
				}
				label := strings.TrimSpace(c.Params["label"])
				if label == "" || utf8.RuneCountInString(label) > 32 || strings.ContainsAny(label, "\n<>") {
					return ui.Result{Outcome: "invalid"}, nil
				}
				_, err = a.db.Q(ctx).Exec(ctx, `UPDATE subscriptions.subscriptions SET label = $2 WHERE id = $1`, s.ID, label)
				return ui.Result{Outcome: "ok"}, err
			},
			"subscriptions.revoke": func(ctx context.Context, c ui.Call) (ui.Result, error) {
				s, err := a.own(ctx, c)
				if err != nil {
					return ui.Result{}, err
				}
				p, err := a.panels.Panel(s.Panel)
				if err != nil {
					return ui.Result{}, err
				}
				acc, err := p.Revoke(ctx, s.Ref)
				if err != nil {
					return ui.Result{}, err
				}
				_, err = a.db.Q(ctx).Exec(ctx, `UPDATE subscriptions.subscriptions SET url = $2 WHERE id = $1`, s.ID, acc.URL)
				return ui.Result{Outcome: "ok"}, err
			},
		},
		Conditions: map[string]ui.ConditionFunc{
			"subscriptions.any": func(ctx context.Context, c ui.Call) (bool, error) {
				var has bool
				err := a.db.Q(ctx).QueryRow(ctx,
					`SELECT EXISTS (SELECT 1 FROM subscriptions.subscriptions WHERE user_id = $1 AND status <> 'pending')`, c.User.ID).Scan(&has)
				return has, err
			},
		},
	})
}

func view(s Subscription) map[string]any {
	return map[string]any{
		"ID": s.ID, "Label": s.Label, "Status": s.Status, "ExpiresAt": s.ExpiresAt,
		"Limit": int64(s.TrafficGB) << 30, "Devices": int64(s.Devices), "URL": s.URL,
	}
}

// own returns the subscription named by params["id"] if it is the user's.
func (a *App) own(ctx context.Context, c ui.Call) (Subscription, error) {
	id, err := strconv.ParseInt(c.Params["id"], 10, 64)
	if err != nil {
		return Subscription{}, fmt.Errorf("номер подписки %q", c.Params["id"])
	}
	s, err := a.Get(ctx, id)
	if err != nil {
		return s, err
	}
	if s.UserID != c.User.ID {
		return Subscription{}, fmt.Errorf("подписка %d принадлежит другому пользователю", id)
	}
	return s, nil
}

const columns = `id, user_id, plan_id, panel, ref, url, label, status, expires_at, traffic_gb, devices`

func scan(row pgx.Row) (Subscription, error) {
	var s Subscription
	err := row.Scan(&s.ID, &s.UserID, &s.Plan, &s.Panel, &s.Ref, &s.URL, &s.Label, &s.Status, &s.ExpiresAt, &s.TrafficGB, &s.Devices)
	return s, err
}

// Get returns a subscription by id.
func (a *App) Get(ctx context.Context, id int64) (Subscription, error) {
	s, err := scan(a.db.Q(ctx).QueryRow(ctx, `SELECT `+columns+` FROM subscriptions.subscriptions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return s, fmt.Errorf("подписка %d не найдена", id)
	}
	return s, err
}

// List returns the user's subscriptions, active first.
func (a *App) List(ctx context.Context, user int64) ([]Subscription, error) {
	rows, err := a.db.Q(ctx).Query(ctx, `SELECT `+columns+` FROM subscriptions.subscriptions
		WHERE user_id = $1 AND status <> 'pending' ORDER BY status = 'active' DESC, expires_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Granted reports whether a grant with this key was made.
func (a *App) Granted(ctx context.Context, key string) (bool, error) {
	var ok bool
	err := a.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subscriptions.grants WHERE key = $1)`, key).Scan(&ok)
	return ok, err
}

// Grant creates or extends a subscription and puts it on its panel. It is
// safe to repeat: a grant with a known key only re-syncs the panel.
//
// The row is committed before the panel is called, so a crash between the
// two is healed by the retry: the panel finds the account by its name.
func (a *App) Grant(ctx context.Context, g Grant) (Subscription, error) {
	var id int64
	err := a.db.InTx(ctx, func(ctx context.Context) error {
		err := a.db.Q(ctx).QueryRow(ctx, `SELECT subscription_id FROM subscriptions.grants WHERE key = $1`, g.Key).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		days := time.Duration(g.Plan.Days) * 24 * time.Hour
		if g.Extend != 0 {
			s, err := scan(a.db.Q(ctx).QueryRow(ctx, `SELECT `+columns+` FROM subscriptions.subscriptions WHERE id = $1 FOR UPDATE`, g.Extend))
			if err != nil {
				return fmt.Errorf("подписка %d: %w", g.Extend, err)
			}
			if s.UserID != g.User.ID {
				return fmt.Errorf("подписка %d принадлежит другому пользователю", s.ID)
			}
			from := later(s.ExpiresAt, time.Now())
			id = s.ID
			_, err = a.db.Q(ctx).Exec(ctx, `UPDATE subscriptions.subscriptions
				SET expires_at = $2, plan_id = $3, traffic_gb = $4, devices = $5, reminded_at = NULL,
				    status = CASE WHEN status = 'expired' THEN 'active' ELSE status END
				WHERE id = $1`, s.ID, from.Add(days), g.Plan.ID, g.Plan.TrafficGB, g.Plan.Devices)
			if err != nil {
				return err
			}
		} else {
			var n int
			if err := a.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM subscriptions.subscriptions WHERE user_id = $1 AND plan_id = $2`, g.User.ID, g.Plan.ID).Scan(&n); err != nil {
				return err
			}
			label := g.Plan.Name(g.User.Lang)
			if n > 0 {
				label = fmt.Sprintf("%s · %d", label, n+1)
			}
			panel := g.Plan.Panel
			if panel == "" {
				panel = a.panels.Default()
			}
			err := a.db.Q(ctx).QueryRow(ctx, `INSERT INTO subscriptions.subscriptions
				(user_id, plan_id, panel, label, expires_at, traffic_gb, devices) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
				g.User.ID, g.Plan.ID, panel, label, time.Now().Add(days), g.Plan.TrafficGB, g.Plan.Devices).Scan(&id)
			if err != nil {
				return err
			}
		}
		_, err = a.db.Q(ctx).Exec(ctx, `INSERT INTO subscriptions.grants (key, subscription_id) VALUES ($1, $2)`, g.Key, id)
		return err
	})
	if err != nil {
		return Subscription{}, err
	}
	s, err := a.Get(ctx, id)
	if err != nil {
		return s, err
	}
	return s, a.sync(ctx, &s, g.User.TelegramID, g.Plan.Targets)
}

// sync puts the subscription on its panel and stores the link.
func (a *App) sync(ctx context.Context, s *Subscription, telegramID int64, targets []string) error {
	p, err := a.panels.Panel(s.Panel)
	if err != nil {
		return err
	}
	acc, err := p.Ensure(ctx, s.Ref, panels.Account{
		Name: strconv.FormatInt(s.ID, 10), TelegramID: telegramID, ExpiresAt: s.ExpiresAt,
		TrafficGB: s.TrafficGB, Devices: s.Devices, Targets: targets,
		Description: fmt.Sprintf("tors: user %d, plan %s", s.UserID, s.Plan),
	})
	if err != nil {
		return fmt.Errorf("панель %s: %w", s.Panel, err)
	}
	status := s.Status
	if status == "pending" {
		status = "active"
	}
	_, err = a.db.Q(ctx).Exec(ctx, `UPDATE subscriptions.subscriptions SET ref = $2, url = $3, status = $4 WHERE id = $1`,
		s.ID, acc.Ref, acc.URL, status)
	s.Ref, s.URL, s.Status = acc.Ref, acc.URL, status
	return err
}

func (a *App) onPaid(ctx context.Context, e billing.OrderPaid) error {
	p, ok := a.catalog.Plan(e.Plan)
	if !ok {
		return fmt.Errorf("тариф %s заказа %d не найден в конфигурации", e.Plan, e.Order)
	}
	u, err := a.users.Get(ctx, e.User)
	if err != nil {
		return err
	}
	var extend int64
	if v := e.Meta["extend"]; v != "" {
		if extend, err = strconv.ParseInt(v, 10, 64); err != nil {
			return fmt.Errorf("заказ %d: extend %q", e.Order, v)
		}
	}
	s, err := a.Grant(ctx, Grant{User: u, Plan: p, Key: "order:" + strconv.FormatInt(e.Order, 10), Extend: extend})
	if err != nil {
		return err
	}
	return a.q.Publish(ctx, Paid{User: u.ID, ID: s.ID, Label: s.Label, ExpiresAt: s.ExpiresAt, URL: s.URL})
}

// remind marks ended subscriptions expired and warns about ending ones.
func (a *App) remind(ctx context.Context) error {
	return a.db.InTx(ctx, func(ctx context.Context) error {
		rows, err := a.db.Q(ctx).Query(ctx, `UPDATE subscriptions.subscriptions SET status = 'expired'
			WHERE status = 'active' AND expires_at <= now() RETURNING id, user_id, label`)
		if err != nil {
			return err
		}
		var expired []Expired
		for rows.Next() {
			var e Expired
			if err := rows.Scan(&e.ID, &e.User, &e.Label); err != nil {
				rows.Close()
				return err
			}
			expired = append(expired, e)
		}
		rows.Close()
		for _, e := range expired {
			if err := a.q.Publish(ctx, e); err != nil {
				return err
			}
		}
		if *a.RemindDays <= 0 {
			return nil
		}
		rows, err = a.db.Q(ctx).Query(ctx, `UPDATE subscriptions.subscriptions SET reminded_at = now()
			WHERE status = 'active' AND reminded_at IS NULL AND expires_at <= now() + make_interval(days => $1)
			RETURNING id, user_id, label, expires_at`, *a.RemindDays)
		if err != nil {
			return err
		}
		var soon []Expiring
		for rows.Next() {
			var e Expiring
			if err := rows.Scan(&e.ID, &e.User, &e.Label, &e.ExpiresAt); err != nil {
				rows.Close()
				return err
			}
			e.DaysLeft = max(1, int(time.Until(e.ExpiresAt).Hours()/24+0.5))
			soon = append(soon, e)
		}
		rows.Close()
		for _, e := range soon {
			if err := a.q.Publish(ctx, e); err != nil {
				return err
			}
		}
		return nil
	})
}

func later(t, u time.Time) time.Time {
	if t.After(u) {
		return t
	}
	return u
}
