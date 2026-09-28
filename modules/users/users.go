// Package users is the "users" app: people who talk to the bot.
package users

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/market"
	"github.com/tori43-hash/tors/modules/database"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/theme"
)

func init() { tors.RegisterModule(App{}) }

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed screens.json
var screens []byte

// User is a person known to the bot.
type User struct {
	ID         int64
	TelegramID int64
	FirstName  string
	Username   string
	Lang       string
	CreatedAt  time.Time
}

// Profile is what Telegram tells about a user.
type Profile struct {
	TelegramID   int64
	FirstName    string
	Username     string
	LanguageCode string
}

// App stores users.
type App struct {
	// Languages the bot speaks; the first is the default.
	Languages []string `json:"languages,omitzero"`

	db *database.DB
}

func (App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "users", New: func() tors.Module { return new(App) }}
}

func (App) Market() market.Info {
	return market.Info{
		Name:     "Пользователи",
		Summary:  "Люди, которые пишут боту: имя, язык, первый визит.",
		Category: "core",
		Hidden:   true,
		Requires: []string{"database", "ui"},
		Config: []market.Field{{
			Key: "languages", Title: "Языки бота", Type: "strings", Default: []string{"ru", "en"},
			Hint: "Первый — язык по умолчанию",
		}},
	}
}

func (App) UI() ui.Contribution {
	return ui.Contribution{
		Actions: map[string]theme.Action{
			"users.set_language": {
				Title:  "Сменить язык",
				Params: []string{"lang"},
				Outcomes: []theme.Outcome{
					{Name: "ok", Title: "Готово", Default: "language"},
				},
			},
		},
		Screens: mustScreens(screens),
	}
}

func (a *App) Provision(ctx tors.Context) error {
	if len(a.Languages) == 0 {
		a.Languages = []string{"ru", "en"}
	}
	var err error
	if a.db, err = database.Get(ctx); err != nil {
		return err
	}
	if err := a.db.Migrate(ctx, migrations, "migrations"); err != nil {
		return err
	}
	reg, err := ui.Get(ctx)
	if err != nil {
		return err
	}
	return reg.Register(a.UI(), ui.Bindings{Actions: map[string]ui.ActionFunc{
		"users.set_language": func(ctx context.Context, c ui.Call) (ui.Result, error) {
			if err := a.SetLang(ctx, c.User.ID, c.Params["lang"]); err != nil {
				return ui.Result{}, err
			}
			return ui.Result{Outcome: "ok"}, nil
		},
	}})
}

// Touch records a contact from Telegram: creates the user on first contact
// and refreshes the name later. created is true for a new user.
func (a *App) Touch(ctx context.Context, p Profile) (u User, created bool, err error) {
	lang := a.Languages[0]
	if slices.Contains(a.Languages, p.LanguageCode) {
		lang = p.LanguageCode
	}
	err = a.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO users.users (telegram_id, first_name, username, lang) VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE SET first_name = excluded.first_name, username = excluded.username, last_seen_at = now()
		RETURNING id, telegram_id, first_name, username, lang, created_at, (xmax = 0)`,
		p.TelegramID, p.FirstName, p.Username, lang).Scan(&u.ID, &u.TelegramID, &u.FirstName, &u.Username, &u.Lang, &u.CreatedAt, &created)
	return u, created, err
}

// Get returns a user by internal id.
func (a *App) Get(ctx context.Context, id int64) (User, error) {
	var u User
	err := a.db.Q(ctx).QueryRow(ctx, `SELECT id, telegram_id, first_name, username, lang, created_at FROM users.users WHERE id = $1`, id).
		Scan(&u.ID, &u.TelegramID, &u.FirstName, &u.Username, &u.Lang, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, fmt.Errorf("пользователь %d не найден", id)
	}
	return u, err
}

// SetLang changes the user's language if the bot speaks it.
func (a *App) SetLang(ctx context.Context, id int64, lang string) error {
	if !slices.Contains(a.Languages, lang) {
		return fmt.Errorf("язык %q не поддерживается", lang)
	}
	_, err := a.db.Q(ctx).Exec(ctx, "UPDATE users.users SET lang = $2 WHERE id = $1", id, lang)
	return err
}

func mustScreens(b []byte) map[string]*theme.Screen {
	s, err := theme.ParseScreens(b)
	if err != nil {
		panic(err)
	}
	return s
}
