// Package stars takes payments in Telegram Stars: the bot sends an invoice,
// confirms the pre-checkout query and records the successful payment.
package stars

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/billing"
	"github.com/tori43-hash/tors/modules/telegram"
	"github.com/tori43-hash/tors/modules/users"
)

func init() { tors.RegisterModule(new(Gateway)) }

// Gateway is the Telegram Stars payment method.
type Gateway struct {
	tg     *telegram.App
	orders billing.Orders
}

func (*Gateway) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "billing.gateways.stars", New: func() tors.Module { return new(Gateway) }}
}

func (*Gateway) Market() market.Info {
	return market.Info{
		Name:     "Telegram Stars",
		Summary:  "Оплата звёздами прямо в Telegram, без платёжного провайдера.",
		Category: "payment",
		Default:  true,
		Requires: []string{"billing", "telegram"},
		Host:     &market.Host{App: "billing", Field: "gateway", Key: "gateway"},
	}
}

const prefix = "order:"

func (g *Gateway) Provision(ctx tors.Context) error {
	var err error
	g.tg, err = tors.AppAs[*telegram.App](ctx, "telegram")
	return err
}

func (g *Gateway) Attach(o billing.Orders) {
	g.orders = o
	g.tg.OnPreCheckout(func(ctx context.Context, q telego.PreCheckoutQuery) error {
		id, ok := order(q.InvoicePayload)
		if !ok {
			return nil // someone else's invoice
		}
		if q.Currency != "XTR" {
			return fmt.Errorf("оплата только звёздами")
		}
		return g.orders.Check(ctx, id, q.TotalAmount)
	})
	g.tg.OnPayment(func(ctx context.Context, _ users.User, p telego.SuccessfulPayment) error {
		id, ok := order(p.InvoicePayload)
		if !ok || p.Currency != "XTR" {
			return nil
		}
		return g.orders.Confirm(ctx, billing.Payment{
			Order: id, Gateway: "stars", ChargeID: p.TelegramPaymentChargeID, Amount: p.TotalAmount,
		})
	})
}

func (g *Gateway) Invoice(ctx context.Context, inv billing.Invoice) error {
	_, err := g.tg.Bot().SendInvoice(ctx, &telego.SendInvoiceParams{
		ChatID:      tu.ID(inv.TelegramID),
		Title:       clip(inv.Title, 32),
		Description: clip(inv.Description, 255),
		Payload:     prefix + strconv.FormatInt(inv.Order, 10),
		Currency:    "XTR",
		Prices:      []telego.LabeledPrice{{Label: clip(inv.Title, 32), Amount: inv.Amount}},
	})
	return err
}

func order(payload string) (int64, bool) {
	s, ok := strings.CutPrefix(payload, prefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
