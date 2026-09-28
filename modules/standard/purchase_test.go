package standard_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/internal/pgtest"
	"github.com/tori43-hash/tors/internal/tgtest"
	"github.com/tori43-hash/tors/modules/panels"
	"github.com/tori43-hash/tors/modules/panels/fake"
	_ "github.com/tori43-hash/tors/modules/standard"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

// botTheme is what a person builds in the builder: a menu of their own plus
// the starter screens of the modules they added.
func botTheme(t *testing.T) json.RawMessage {
	screens := map[string]*theme.Screen{}
	for _, id := range []string{"users", "billing", "subscriptions", "trial", "support"} {
		info, err := tors.GetModule(id)
		if err != nil {
			t.Fatal(err)
		}
		for name, s := range info.New().(ui.Provider).UI().Screens {
			screens[name] = s
		}
	}
	menu, err := theme.ParseScreens([]byte(`{"menu": {
		"blocks": [{"text": {"ru": "Привет, {{ .User.FirstName }}!", "en": "Hi, {{ .User.FirstName }}!"}}],
		"keyboard": [
			[{"text": {"ru": "Тарифы"}, "goto": "plans"}, {"text": {"ru": "Мои подписки"}, "goto": "my_subs"}],
			[{"action": "trial.start", "visible_if": "trial.available"}],
			[{"text": {"ru": "Поддержка"}, "goto": "support"}, {"text": {"ru": "Язык"}, "goto": "language"}]
		]}}`))
	if err != nil {
		t.Fatal(err)
	}
	screens["menu"] = menu["menu"]
	b, err := json.Marshal(map[string]any{"version": 1, "commands": map[string]string{"start": "menu"}, "screens": screens})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPurchase(t *testing.T) {
	dsn := pgtest.DSN(t)
	tg := tgtest.New(t)
	cfg, _ := json.Marshal(map[string]any{"apps": map[string]any{
		"database": map[string]any{"dsn": dsn},
		"jobs":     map[string]any{"poll_interval": "50ms"},
		"kv":       map[string]any{"store": map[string]any{"store": "postgres"}},
		"telegram": map[string]any{"token": tgtest.Token, "api_server": tg.URL, "theme": botTheme(t)},
		"catalog": map[string]any{"plans": []any{
			map[string]any{"id": "month", "title": map[string]any{"ru": "1 месяц", "en": "1 month"}, "days": 30, "traffic_gb": 200, "devices": 3, "price": 150},
		}},
		"billing":       map[string]any{"gateway": map[string]any{"gateway": "stars"}},
		"panels":        map[string]any{"providers": map[string]any{"main": map[string]any{"provider": "fake"}}},
		"subscriptions": map[string]any{},
		"trial":         map[string]any{"days": 3},
		"support":       map[string]any{"contact": "@help"},
	}})
	inst, err := tors.Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop()
	panel := findFake(t, inst)
	const me = 9001

	// Menu, then the free trial.
	tg.Text(me, "/start")
	menu := tg.Last(1, "sendMessage")
	kb := menu.Keyboard()
	if menu.Str("text") != "Привет, Анна!" || len(kb) != 3 || kb[1][0].Text != "Попробовать бесплатно" {
		t.Fatalf("menu %q %+v", menu.Str("text"), kb)
	}
	msg := 101
	tg.Press(me, msg, kb[1][0].Data)
	if got := tg.Last(1, "editMessageText").Str("text"); !strings.Contains(got, "Пробный период включён") {
		t.Fatalf("trial %q", got)
	}
	if n := len(panel.Accounts()); n != 1 {
		t.Fatalf("trial accounts %d", n)
	}
	tg.Press(me, msg, "b:0") // open the subscription
	card := tg.Last(2, "editMessageText")
	if !strings.Contains(card.Str("text"), "Пробный период") || !strings.Contains(card.Str("text"), "https://sub.example.com/") {
		t.Fatalf("trial card %q", card.Str("text"))
	}

	// The trial button is gone from the menu now.
	tg.Text(me, "/start")
	kb = tg.Last(2, "sendMessage").Keyboard()
	if len(kb) != 2 {
		t.Fatalf("menu after trial %+v", kb)
	}
	msg = 102

	// Plans → plan → pay.
	tg.Press(me, msg, kb[0][0].Data)
	plans := tg.Last(3, "editMessageText")
	if plans.Keyboard()[0][0].Text != "1 месяц · 150 ⭐" {
		t.Fatalf("plans %+v", plans.Keyboard())
	}
	tg.Press(me, msg, plans.Keyboard()[0][0].Data)
	plan := tg.Last(4, "editMessageText")
	if !strings.Contains(plan.Str("text"), "Трафик: 200 ГБ") || !strings.Contains(plan.Str("text"), "Срок: 30 дней") {
		t.Fatalf("plan %q", plan.Str("text"))
	}
	tg.Press(me, msg, plan.Keyboard()[0][0].Data)
	inv := tg.Last(1, "sendInvoice")
	prices := inv.Params["prices"].([]any)[0].(map[string]any)
	if inv.Str("currency") != "XTR" || inv.Str("payload") != "order:1" || prices["amount"].(float64) != 150 ||
		inv.Str("description") != "30 дней, 200 ГБ, 3 устройства" {
		t.Fatalf("invoice %+v", inv.Params)
	}

	// Telegram asks before charging: a wrong amount is refused.
	preCheckout(tg, me, "order:1", 100)
	if a := tg.Last(1, "answerPreCheckoutQuery"); a.Params["ok"] != false || a.Str("error_message") == "" {
		t.Fatalf("wrong amount accepted %+v", a.Params)
	}
	preCheckout(tg, me, "order:1", 150)
	if a := tg.Last(2, "answerPreCheckoutQuery"); a.Params["ok"] != true {
		t.Fatalf("pre-checkout %+v", a.Params)
	}
	// The payment arrives twice (Telegram retries); the subscription is granted once.
	paid(tg, me, "order:1", 150, "charge-1")
	paid(tg, me, "order:1", 150, "charge-1")
	done := tg.Last(3, "sendMessage")
	if !strings.Contains(done.Str("text"), "Оплата прошла") || !strings.Contains(done.Str("text"), "1 месяц") {
		t.Fatalf("paid screen %q", done.Str("text"))
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(tg.Calls("sendMessage")); n != 3 {
		t.Fatalf("paid screen sent %d times", n-2)
	}
	if n := len(panel.Accounts()); n != 2 {
		t.Fatalf("accounts after purchase %d", n)
	}

	// Renew the bought subscription from its card.
	msg = 103 + 1                                 // the invoice took 103, the paid screen 104
	tg.Press(me, msg, done.Keyboard()[1][0].Data) // my subscriptions
	subs := tg.Last(5, "editMessageText").Keyboard()
	if !strings.HasPrefix(subs[0][0].Text, "1 месяц · до ") || !strings.HasPrefix(subs[1][0].Text, "Пробный период · до ") {
		t.Fatalf("my subs %+v", subs)
	}
	tg.Press(me, msg, subs[0][0].Data)
	sub := tg.Last(6, "editMessageText")
	if sub.Keyboard()[1][0].Text != "Продлить" {
		t.Fatalf("sub card %+v", sub.Keyboard())
	}
	before := expiry(t, dsn, 2)
	tg.Press(me, msg, sub.Keyboard()[1][0].Data)
	if tg.Last(2, "sendInvoice").Str("payload") != "order:2" {
		t.Fatal("renew invoice")
	}
	preCheckout(tg, me, "order:2", 150)
	paid(tg, me, "order:2", 150, "charge-2")
	tg.Wait(4, "sendMessage")
	if got := expiry(t, dsn, 2).Sub(before); got < 29*24*time.Hour || got > 31*24*time.Hour {
		t.Fatalf("renewal added %v", got)
	}
	if n := len(panel.Accounts()); n != 2 {
		t.Fatalf("renewal created an account: %d", n)
	}

	// Rename through text input.
	tg.Press(me, msg, sub.Keyboard()[2][0].Data)
	tg.Wait(7, "editMessageText")
	tg.Text(me, "Телефон")
	if got := tg.Last(5, "sendMessage").Str("text"); !strings.HasPrefix(got, "<b>Телефон</b>") {
		t.Fatalf("renamed card %q", got)
	}

	// Reminders: the trial ends in two days, then ends.
	exec(t, dsn, `UPDATE subscriptions.subscriptions SET expires_at = now() + interval '2 days' WHERE id = 1`)
	exec(t, dsn, `UPDATE jobs.periodic SET next_run = now()`)
	if got := tg.Last(6, "sendMessage").Str("text"); got != "Подписка «Пробный период» закончится через 2 дня." {
		t.Fatalf("reminder %q", got)
	}
	exec(t, dsn, `UPDATE subscriptions.subscriptions SET expires_at = now() - interval '1 minute' WHERE id = 1`)
	exec(t, dsn, `UPDATE jobs.periodic SET next_run = now()`)
	if got := tg.Last(7, "sendMessage").Str("text"); got != "Подписка «Пробный период» закончилась." {
		t.Fatalf("expired %q", got)
	}
}

func findFake(t *testing.T, inst *tors.Instance) *fake.Panel {
	t.Helper()
	v, _ := inst.App("panels")
	p, err := v.(*panels.App).Panel("main")
	if err != nil {
		t.Fatal(err)
	}
	return p.(*fake.Panel)
}

func preCheckout(tg *tgtest.Server, from int64, payload string, amount int) {
	tg.Push(map[string]any{"pre_checkout_query": map[string]any{
		"id": "pc" + payload, "from": tgtest.User(from), "currency": "XTR", "total_amount": amount, "invoice_payload": payload,
	}})
}

func paid(tg *tgtest.Server, from int64, payload string, amount int, charge string) {
	tg.Push(map[string]any{"message": map[string]any{
		"message_id": 1, "date": time.Now().Unix(), "from": tgtest.User(from), "chat": map[string]any{"id": from, "type": "private"},
		"successful_payment": map[string]any{
			"currency": "XTR", "total_amount": amount, "invoice_payload": payload,
			"telegram_payment_charge_id": charge, "provider_payment_charge_id": "",
		},
	}})
}

func exec(t *testing.T, dsn, sql string) {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	if _, err := c.Exec(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
}

func expiry(t *testing.T, dsn string, id int) time.Time {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	var at time.Time
	if err := c.QueryRow(context.Background(), `SELECT expires_at FROM subscriptions.subscriptions WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}
