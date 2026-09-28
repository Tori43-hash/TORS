// Package billing is the "billing" app: orders for plans and their payment.
// How people pay is a guest module in "billing.gateways" (Telegram Stars).
// A paid order becomes the durable event billing.order.paid.
package billing

import (
	"context"
	"embed"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/catalog"
	"github.com/tori43-hash/tors/modules/database"
	"github.com/tori43-hash/tors/modules/jobs"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed screens.json
var screens []byte

// OrderPaid is published when an order is paid. Meta carries what the buyer
// asked for, such as the subscription to extend.
type OrderPaid struct {
	Order  int64             `json:"order"`
	User   int64             `json:"user"`
	Plan   string            `json:"plan"`
	Amount int               `json:"amount"`
	Meta   map[string]string `json:"meta,omitzero"`
}

func (OrderPaid) EventName() string { return "billing.order.paid" }

// Invoice is what a gateway asks the user to pay.
type Invoice struct {
	Order       int64
	TelegramID  int64
	Lang        string
	Title       string
	Description string
	Amount      int // Telegram Stars
}

// Payment is a confirmed payment reported by a gateway.
type Payment struct {
	Order    int64
	Gateway  string
	ChargeID string
	Amount   int
}

// Gateway is implemented by payment modules (billing.gateways.*).
type Gateway interface {
	// Attach gives the gateway the orders it confirms payments of.
	Attach(o Orders)
	Invoice(ctx context.Context, inv Invoice) error
}

// Orders is what gateways use to check and confirm payments.
type Orders interface {
	Check(ctx context.Context, order int64, amount int) error
	Confirm(ctx context.Context, p Payment) error
}

// Checkout is a request to buy a plan.
type Checkout struct {
	User ui.User
	Plan catalog.Plan
	Meta map[string]string
}

// App keeps orders.
type App struct {
	Gateway jsontext.Value `json:"gateway" tors:"namespace=billing.gateways inline_key=gateway"`

	db      *database.DB
	q       *jobs.Queue
	catalog *catalog.App
	gateway Gateway
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "billing", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Оплата",
		Summary:  "Заказы и оплата тарифов. Способ оплаты подключается отдельно.",
		Category: "sales",
		Requires: []string{"catalog", "database", "jobs", "ui"},
		Needs:    []string{"billing.gateways"},
	}
}

func (*App) UI() ui.Contribution {
	s, err := theme.ParseScreens(screens)
	if err != nil {
		panic(err)
	}
	return ui.Contribution{
		Actions: map[string]theme.Action{
			"billing.checkout": {
				Title:  "Оплатить тариф",
				Label:  theme.Text{"ru": "Оплатить", "en": "Pay"},
				Params: []string{"plan"},
				Outcomes: []theme.Outcome{
					{Name: "invoice", Title: "Отправлен счёт", Terminal: true},
				},
			},
		},
		Screens: s,
	}
}

func (a *App) Provision(ctx tors.Context) error {
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
	if a.catalog, err = tors.AppAs[*catalog.App](ctx, "catalog"); err != nil {
		return err
	}
	if len(a.Gateway) == 0 {
		return errors.New("не выбран способ оплаты — добавьте, например, Telegram Stars")
	}
	m, err := ctx.LoadModule(a, "Gateway")
	if err != nil {
		return err
	}
	g, ok := m.(Gateway)
	if !ok {
		return errors.New("модуль оплаты не реализует billing.Gateway")
	}
	g.Attach(a)
	a.gateway = g
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	return reg.Register(a.UI(), ui.Bindings{Actions: map[string]ui.ActionFunc{
		"billing.checkout": func(ctx context.Context, c ui.Call) (ui.Result, error) {
			p, ok := a.catalog.Plan(c.Params["plan"])
			if !ok {
				return ui.Result{}, fmt.Errorf("тариф %q не найден", c.Params["plan"])
			}
			if _, err := a.Checkout(ctx, Checkout{User: c.User, Plan: p}); err != nil {
				return ui.Result{}, err
			}
			return ui.Result{Outcome: "invoice"}, nil
		},
	}})
}

// Checkout creates an order and sends the invoice.
func (a *App) Checkout(ctx context.Context, c Checkout) (int64, error) {
	var id int64
	err := a.db.Q(ctx).QueryRow(ctx,
		`INSERT INTO billing.orders (user_id, plan_id, amount, meta) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.User.ID, c.Plan.ID, c.Plan.Price, meta(c.Meta)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, a.gateway.Invoice(ctx, Invoice{
		Order: id, TelegramID: c.User.TelegramID, Lang: c.User.Lang, Amount: c.Plan.Price,
		Title: c.Plan.Name(c.User.Lang), Description: describe(c.Plan, c.User.Lang),
	})
}

// Check tells whether an order can be paid with this amount.
func (a *App) Check(ctx context.Context, order int64, amount int) error {
	var status string
	var want int
	err := a.db.Q(ctx).QueryRow(ctx, `SELECT status, amount FROM billing.orders WHERE id = $1`, order).Scan(&status, &want)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return errors.New("заказ не найден")
	case err != nil:
		return err
	case status != "pending":
		return errors.New("заказ уже оплачен")
	case amount != want:
		return errors.New("сумма не совпадает с заказом")
	}
	return nil
}

// Confirm marks an order paid and publishes billing.order.paid in the same
// transaction. Confirming the same payment again does nothing.
func (a *App) Confirm(ctx context.Context, p Payment) error {
	return a.db.InTx(ctx, func(ctx context.Context) error {
		var ev OrderPaid
		var status string
		var m map[string]string
		err := a.db.Q(ctx).QueryRow(ctx,
			`SELECT user_id, plan_id, amount, status, meta FROM billing.orders WHERE id = $1 FOR UPDATE`, p.Order).
			Scan(&ev.User, &ev.Plan, &ev.Amount, &status, &m)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("заказ %d не найден", p.Order)
		}
		if err != nil {
			return err
		}
		if status == "paid" {
			return nil
		}
		if p.Amount != ev.Amount {
			return fmt.Errorf("заказ %d: оплачено %d вместо %d", p.Order, p.Amount, ev.Amount)
		}
		if _, err := a.db.Q(ctx).Exec(ctx,
			`INSERT INTO billing.payments (order_id, gateway, charge_id, amount) VALUES ($1, $2, $3, $4)`,
			p.Order, p.Gateway, p.ChargeID, p.Amount); err != nil {
			return err
		}
		if _, err := a.db.Q(ctx).Exec(ctx,
			`UPDATE billing.orders SET status = 'paid', paid_at = now() WHERE id = $1`, p.Order); err != nil {
			return err
		}
		ev.Order, ev.Meta = p.Order, m
		return a.q.Publish(ctx, ev)
	})
}

func meta(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func describe(p catalog.Plan, lang string) string {
	en := lang == "en"
	parts := []string{fmt.Sprintf("%d %s", p.Days, word(en, p.Days, "day", "days", "день", "дня", "дней"))}
	switch {
	case p.TrafficGB > 0 && en:
		parts = append(parts, fmt.Sprintf("%d GB", p.TrafficGB))
	case p.TrafficGB > 0:
		parts = append(parts, fmt.Sprintf("%d ГБ", p.TrafficGB))
	case en:
		parts = append(parts, "unlimited traffic")
	default:
		parts = append(parts, "трафик без ограничений")
	}
	if p.Devices > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", p.Devices, word(en, p.Devices, "device", "devices", "устройство", "устройства", "устройств")))
	}
	return strings.Join(parts, ", ")
}

func word(en bool, n int, one, many, ru1, ru2, ru5 string) string {
	if en {
		if n == 1 {
			return one
		}
		return many
	}
	switch n10, n100 := n%10, n%100; {
	case n10 == 1 && n100 != 11:
		return ru1
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return ru2
	}
	return ru5
}
