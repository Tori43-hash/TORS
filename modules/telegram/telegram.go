// Package telegram is the "telegram" app: it runs the bot described by a
// theme — shows screens, handles button presses and text input, and delivers
// event screens. Data and actions come from other modules through "ui".
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/httpserver"
	"github.com/tori43-hash/tors/modules/kv"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/modules/users"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(new(App)) }

// App is the bot.
type App struct {
	// Token from @BotFather, usually "{env.BOT_TOKEN}".
	Token string `json:"token"`
	// Transport: "polling" (default) or "webhook" (needs the http app with public_url).
	Transport string `json:"transport,omitzero"`
	// Theme is the bot's UI; ThemeFile loads it from a file instead.
	Theme     jsontext.Value `json:"theme,omitzero"`
	ThemeFile string         `json:"theme_file,omitzero"`
	// Assets is the folder for photos referenced as "assets/…" in the theme.
	Assets string `json:"assets,omitzero"`
	// APIServer points to a Bot API server other than api.telegram.org.
	APIServer string `json:"api_server,omitzero"`

	bot      *telego.Bot
	theme    *theme.Theme
	manifest *theme.Manifest
	ui       *ui.App
	users    *users.App
	kv       *kv.Bucket
	http     *httpserver.App
	log      *slog.Logger
	hookPath string
	secret   string

	mu          sync.RWMutex
	preCheckout []func(context.Context, telego.PreCheckoutQuery) error
	payments    []func(context.Context, users.User, telego.SuccessfulPayment) error

	qmu    sync.Mutex
	queues map[int64]*chatQueue
	sem    chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "telegram", New: func() tors.Module { return new(App) }}
}

func (*App) Market() market.Info {
	return market.Info{
		Name:     "Telegram-бот",
		Summary:  "Показывает экраны из холста, обрабатывает нажатия, ввод и события.",
		Category: "core",
		Hidden:   true,
		Requires: []string{"users", "ui", "kv"},
		Config: []market.Field{
			{Key: "token", Title: "Токен бота", Type: "secret", Default: "{env.BOT_TOKEN}", Required: true,
				Hint: "Выдаёт @BotFather. Лучше хранить в переменной окружения"},
			{Key: "transport", Title: "Получение обновлений", Type: "select", Default: "polling", Options: []market.Option{
				{Value: "polling", Title: "Опрос (проще, для одного сервера)"},
				{Value: "webhook", Title: "Вебхук (нужен публичный адрес)", Requires: []string{"http"}},
			}},
		},
	}
}

func (a *App) Provision(ctx tors.Context) error {
	a.log = ctx.Logger()
	if a.Token == "" {
		return errors.New("не указан token — токен бота от @BotFather")
	}
	if err := a.loadTheme(); err != nil {
		return err
	}
	opts := []telego.BotOption{telego.WithHTTPClient(&http.Client{Timeout: 70 * time.Second}), telego.WithDiscardLogger()}
	if a.APIServer != "" {
		opts = append(opts, telego.WithAPIServer(strings.TrimRight(a.APIServer, "/")))
	}
	var err error
	if a.bot, err = telego.NewBot(a.Token, opts...); err != nil {
		return fmt.Errorf("токен бота: %w", err)
	}
	if a.ui, err = tors.AppAs[*ui.App](ctx, "ui"); err != nil {
		return err
	}
	if a.users, err = tors.AppAs[*users.App](ctx, "users"); err != nil {
		return err
	}
	if a.kv, err = kv.Get(ctx); err != nil {
		return err
	}
	a.ui.Present(a.presentEvent)
	a.sem = make(chan struct{}, 64)
	a.queues = map[int64]*chatQueue{}

	switch a.Transport {
	case "", "polling":
		a.Transport = "polling"
	case "webhook":
		if a.http, err = tors.AppAs[*httpserver.App](ctx, "http"); err != nil {
			return err
		}
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		a.secret = hex.EncodeToString(b)
		a.hookPath = "/telegram/" + a.secret[:12]
		if err := a.http.Mount(ctx.Module(), "POST "+a.hookPath, http.HandlerFunc(a.webhook)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("transport: %q — ожидается polling или webhook", a.Transport)
	}
	return nil
}

func (a *App) loadTheme() error {
	raw := []byte(a.Theme)
	if a.ThemeFile != "" {
		b, err := os.ReadFile(a.ThemeFile)
		if err != nil {
			return fmt.Errorf("theme_file: %w", err)
		}
		raw = b
		if a.Assets == "" {
			a.Assets = filepath.Dir(a.ThemeFile)
		}
	}
	if len(raw) == 0 {
		return errors.New("нет темы: задайте theme или theme_file — экраны бота из редактора")
	}
	t, err := theme.Parse(raw)
	if err != nil {
		return fmt.Errorf("тема: %w", err)
	}
	a.theme = t
	return nil
}

// Start checks the theme against everything modules registered, then starts
// receiving updates.
func (a *App) Start() error {
	a.manifest = a.ui.Manifest(a.users.Languages)
	var problems []string
	for _, is := range theme.Validate(a.theme, a.manifest) {
		if is.Level == theme.LevelError {
			where := strings.Trim(is.Screen+" "+is.Ref, " ")
			problems = append(problems, strings.TrimSpace(where+": "+is.Message))
		}
	}
	if len(problems) > 0 {
		if len(problems) > 12 {
			problems = append(problems[:12], fmt.Sprintf("… и ещё %d", len(problems)-12))
		}
		return fmt.Errorf("в теме ошибки:\n  %s", strings.Join(problems, "\n  "))
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	allowed := []string{"message", "callback_query", "pre_checkout_query"}
	if a.Transport == "webhook" {
		url, err := a.http.URL(a.hookPath)
		if err != nil {
			return err
		}
		return a.bot.SetWebhook(ctx, &telego.SetWebhookParams{URL: url, SecretToken: a.secret, AllowedUpdates: allowed})
	}
	_ = a.bot.DeleteWebhook(ctx, &telego.DeleteWebhookParams{})
	updates, err := a.bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{Timeout: 30, AllowedUpdates: allowed})
	if err != nil {
		return err
	}
	a.wg.Go(func() {
		for upd := range updates {
			a.dispatch(upd)
		}
	})
	return nil
}

func (a *App) Stop() error {
	if a.cancel != nil {
		a.cancel()
		a.wg.Wait()
		a.cancel = nil
	}
	return nil
}

func (a *App) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != a.secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var upd telego.Update
	if err := decodeUpdate(r, &upd); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	a.dispatch(upd)
	w.WriteHeader(http.StatusOK)
}

// dispatch handles an update in the background. Updates of one chat are
// handled one at a time and in the order they came; chats run in parallel.
func (a *App) dispatch(upd telego.Update) {
	chat := chatOf(upd)
	a.qmu.Lock()
	defer a.qmu.Unlock()
	q := a.queues[chat]
	if q == nil {
		q = &chatQueue{}
		a.queues[chat] = q
	}
	q.pending = append(q.pending, upd)
	if !q.running {
		q.running = true
		a.wg.Go(func() { a.drain(chat, q) })
	}
}

type chatQueue struct {
	pending []telego.Update
	running bool
}

func (a *App) drain(chat int64, q *chatQueue) {
	for {
		a.qmu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			delete(a.queues, chat)
			a.qmu.Unlock()
			return
		}
		upd := q.pending[0]
		q.pending = q.pending[1:]
		a.qmu.Unlock()
		a.sem <- struct{}{}
		a.handleOne(chat, upd)
		<-a.sem
	}
}

func (a *App) handleOne(chat int64, upd telego.Update) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("паника при обработке обновления", "panic", r)
		}
	}()
	if err := a.handle(ctx, upd); err != nil {
		a.log.Error("обновление", "chat", chat, "error", err)
	}
}

func chatOf(upd telego.Update) int64 {
	switch {
	case upd.Message != nil:
		return upd.Message.Chat.ID
	case upd.CallbackQuery != nil:
		return upd.CallbackQuery.From.ID
	case upd.PreCheckoutQuery != nil:
		return upd.PreCheckoutQuery.From.ID
	}
	return 0
}

// Bot gives integration modules (payments, channel checks) the Bot API client.
func (a *App) Bot() *telego.Bot { return a.bot }

// OnPreCheckout registers a check of Telegram Stars payments before they
// happen; an error declines the payment with its text.
func (a *App) OnPreCheckout(fn func(context.Context, telego.PreCheckoutQuery) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.preCheckout = append(a.preCheckout, fn)
}

// OnPayment registers a handler of successful payments.
func (a *App) OnPayment(fn func(context.Context, users.User, telego.SuccessfulPayment) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.payments = append(a.payments, fn)
}

// ChatOf returns the Telegram chat of a user.
func (a *App) ChatOf(ctx context.Context, userID int64) (int64, error) {
	u, err := a.users.Get(ctx, userID)
	if err != nil {
		return 0, err
	}
	return u.TelegramID, nil
}
