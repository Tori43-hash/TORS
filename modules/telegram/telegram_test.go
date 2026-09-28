package telegram_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/internal/pgtest"
	"github.com/tori43-hash/tors/internal/tgtest"
	_ "github.com/tori43-hash/tors/modules/database"
	"github.com/tori43-hash/tors/modules/jobs"
	_ "github.com/tori43-hash/tors/modules/kv"
	_ "github.com/tori43-hash/tors/modules/kv/memory"
	_ "github.com/tori43-hash/tors/modules/telegram"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/modules/users"
	"github.com/tori43-hash/tors/theme"
)

// notes is a test module with a list, an input action and an event.
type notes struct {
	names map[int64]string
	reg   *ui.Registry
	q     *jobs.Queue
}

func (*notes) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "notes", New: func() tors.Module { return &notes{names: map[int64]string{}} }}
}

type noteReady struct {
	User int64  `json:"user"`
	Name string `json:"name"`
}

func (noteReady) EventName() string           { return "notes.ready" }
func (e noteReady) Recipient() int64          { return e.User }
func (e noteReady) EventData() map[string]any { return map[string]any{"Name": e.Name} }

var sent *notes

func (n *notes) Provision(ctx tors.Context) error {
	sent = n
	var err error
	if n.reg, err = ui.Get(ctx); err != nil {
		return err
	}
	if n.q, err = jobs.Get(ctx); err != nil {
		return err
	}
	type item struct {
		ID   int
		Name string
	}
	if err := n.reg.Register(ui.Contribution{
		Data: map[string]theme.DataSource{
			"notes.list": {Title: "Заметки", List: true, Fields: []theme.Field{{Name: "ID", Type: "int"}, {Name: "Name", Type: "string"}}},
			"notes.me":   {Title: "Имя", Fields: []theme.Field{{Name: "Name", Type: "string"}}},
		},
		Actions: map[string]theme.Action{
			"notes.rename": {Title: "Переименовать", Params: []string{"name"}, Input: true, Outcomes: []theme.Outcome{{Name: "ok", Default: "main"}, {Name: "bad", Default: "rename"}}},
		},
		Conditions: map[string]theme.Condition{"notes.named": {Title: "Есть имя"}},
		Events:     map[string]theme.Event{"notes.ready": {Title: "Готово", Default: "ready", Fields: []theme.Field{{Name: "Name", Type: "string"}}}},
	}, ui.Bindings{
		Data: map[string]ui.DataFunc{
			"notes.list": func(context.Context, ui.Call) (any, error) {
				var out []item
				for i := 1; i <= 7; i++ {
					out = append(out, item{ID: i, Name: fmt.Sprintf("n%d", i)})
				}
				return out, nil
			},
			"notes.me": func(_ context.Context, c ui.Call) (any, error) {
				return map[string]any{"Name": n.names[c.User.ID]}, nil
			},
		},
		Actions: map[string]ui.ActionFunc{
			"notes.rename": func(_ context.Context, c ui.Call) (ui.Result, error) {
				if len(c.Params["name"]) < 2 {
					return ui.Result{Outcome: "bad"}, nil
				}
				n.names[c.User.ID] = c.Params["name"]
				return ui.Result{Outcome: "ok"}, nil
			},
		},
		Conditions: map[string]ui.ConditionFunc{
			"notes.named": func(_ context.Context, c ui.Call) (bool, error) { return n.names[c.User.ID] != "", nil },
		},
	}); err != nil {
		return err
	}
	ui.Deliver[noteReady](n.reg)
	return nil
}

func init() { tors.RegisterModule(new(notes)) }

const testTheme = `{
  "version": 1,
  "commands": {"start": "main"},
  "screens": {
    "main": {
      "data": {"me": "notes.me"},
      "blocks": [{"text": {"ru": "Привет, **{{.User.FirstName}}**! {{.me.Name}}", "en": "Hi, {{.User.FirstName}}"}}],
      "keyboard": [
        [{"text": {"ru": "Язык", "en": "Language"}, "goto": "language"}, {"text": {"ru": "Имя"}, "goto": "rename"}],
        [{"text": {"ru": "Список"}, "goto": "list"}],
        [{"text": {"ru": "Сайт"}, "url": "https://example.com"}],
        [{"text": {"ru": "Секрет"}, "goto": "main", "visible_if": "notes.named"}]
      ]
    },
    "language": {
      "blocks": [{"text": {"ru": "Выберите язык", "en": "Choose language"}}],
      "keyboard": [
        [{"text": {"ru": "Русский"}, "action": "users.set_language", "params": {"lang": "ru"}},
         {"text": {"ru": "English"}, "action": "users.set_language", "params": {"lang": "en"}}],
        [{"text": {"ru": "Назад", "en": "Back"}, "back": true}]
      ]
    },
    "rename": {
      "blocks": [{"text": {"ru": "Как вас зовут?"}}],
      "input": {"action": "notes.rename", "param": "name"},
      "keyboard": [[{"text": {"ru": "Назад"}, "back": true}]]
    },
    "list": {
      "data": {"items": "notes.list"},
      "blocks": [{"text": {"ru": "Заметки"}}],
      "keyboard": [
        {"repeat": ".items", "columns": 2, "page_size": 4, "button": {"text": {"ru": "{{.Name}}"}, "goto": "main", "params": {"id": "{{.ID}}"}}},
        [{"text": {"ru": "Домой"}, "home": true}]
      ]
    },
    "ready": {
      "blocks": [{"photo": "https://example.com/p.jpg"}, {"text": {"ru": "Готово, {{.Event.Name}}"}}],
      "keyboard": [[{"text": {"ru": "Меню"}, "home": true}]]
    }
  }
}`

func start(t *testing.T) (*tgtest.Server, *tors.Instance) {
	t.Helper()
	dsn := pgtest.DSN(t)
	tg := tgtest.New(t)
	cfg := map[string]any{
		"apps": map[string]any{
			"database": map[string]any{"dsn": dsn},
			"jobs":     map[string]any{"poll_interval": "50ms"},
			"kv":       map[string]any{},
			"ui":       map[string]any{},
			"users":    map[string]any{},
			"notes":    map[string]any{},
			"telegram": map[string]any{"token": tgtest.Token, "api_server": tg.URL, "theme": json.RawMessage(testTheme)},
		},
	}
	body, _ := json.Marshal(cfg)
	inst, err := tors.Run(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inst.Stop() })
	return tg, inst
}

func TestScreens(t *testing.T) {
	tg, _ := start(t)
	const me = 777

	tg.Text(me, "/start")
	msg := tg.Last(1, "sendMessage")
	if got := msg.Str("text"); got != "Привет, <b>Анна</b>!" {
		t.Fatalf("text %q", got)
	}
	kb := msg.Keyboard()
	if len(kb) != 3 || len(kb[0]) != 2 || kb[0][0].Data != "b:0" || kb[1][0].Data != "b:2" || kb[2][0].URL != "https://example.com" {
		t.Fatalf("keyboard %+v", kb)
	}
	id := 101 // the fake numbers messages from 101

	// Language → English: the screen is edited in place and speaks English.
	tg.Press(me, id, "b:0")
	if got := tg.Last(1, "editMessageText").Str("text"); got != "Выберите язык" {
		t.Fatalf("language screen %q", got)
	}
	tg.Press(me, id, "b:1")
	lang := tg.Last(2, "editMessageText")
	if lang.Str("text") != "Choose language" || lang.Keyboard()[1][0].Text != "Back" {
		t.Fatalf("after set_language %q %+v", lang.Str("text"), lang.Keyboard())
	}
	tg.Press(me, id, "b:2") // back
	if got := tg.Last(3, "editMessageText").Str("text"); got != "Hi, Анна" {
		t.Fatalf("back %q", got)
	}
	tg.Press(me, id, "b:0")
	tg.Press(me, id, "b:0") // Русский
	tg.Wait(5, "editMessageText")
	tg.Press(me, id, "b:2") // back to main
	tg.Wait(6, "editMessageText")

	// Rename via text input; a short name is refused by the action.
	tg.Press(me, id, "b:1")
	if got := tg.Last(7, "editMessageText").Str("text"); got != "Как вас зовут?" {
		t.Fatalf("rename %q", got)
	}
	tg.Text(me, "A")
	if got := tg.Last(2, "sendMessage").Str("text"); got != "Как вас зовут?" {
		t.Fatalf("bad name %q", got)
	}
	tg.Text(me, "Аня")
	main := tg.Last(3, "sendMessage")
	if got := main.Str("text"); got != "Привет, <b>Анна</b>! Аня" {
		t.Fatalf("renamed %q", got)
	}
	if kb := main.Keyboard(); len(kb) != 4 || kb[3][0].Text != "Секрет" {
		t.Fatalf("visible_if %+v", kb)
	}
	// Input is over: plain text is ignored now.
	tg.Text(me, "ещё")

	// A list with pages.
	tg.Press(me, 103, "b:2")
	list := tg.Last(8, "editMessageText").Keyboard()
	if len(list) != 4 || list[0][0].Text != "n1" || list[2][1].Text != "1/2" || list[3][0].Text != "Домой" {
		t.Fatalf("list %+v", list)
	}
	tg.Press(me, 103, list[2][2].Data)
	list = tg.Last(9, "editMessageText").Keyboard()
	if list[0][0].Text != "n5" || list[1][0].Text != "n7" || list[2][1].Text != "2/2" {
		t.Fatalf("page 2 %+v", list)
	}

	// Stale or forged buttons.
	tg.Press(me, 999, "b:0")
	answers := tg.Wait(10, "answerCallbackQuery")
	stale := false
	for _, a := range tg.Calls("answerCallbackQuery") {
		stale = stale || strings.Contains(a.Str("text"), "устарело")
	}
	if !stale || len(answers) == 0 {
		t.Fatalf("stale message not reported: %+v", tg.Calls("answerCallbackQuery"))
	}
	if n := len(tg.Calls("sendMessage")); n != 3 {
		t.Fatalf("plain text after input must be ignored, sendMessage=%d", n)
	}
}

func TestEvent(t *testing.T) {
	tg, inst := start(t)
	tg.Text(555, "/start")
	tg.Wait(1, "sendMessage")

	v, _ := inst.App("users")
	u, _, err := v.(*users.App).Touch(context.Background(), users.Profile{TelegramID: 555})
	if err != nil {
		t.Fatal(err)
	}
	if err := sent.q.Publish(context.Background(), noteReady{User: u.ID, Name: "Аня"}); err != nil {
		t.Fatal(err)
	}
	photo := tg.Last(1, "sendPhoto")
	if photo.Str("caption") != "Готово, Аня" || photo.Str("photo") != "https://example.com/p.jpg" {
		t.Fatalf("event screen %+v", photo.Params)
	}
	// Home from a photo screen replaces the message with a text one.
	tg.Press(555, 102, "b:0")
	tg.Wait(1, "deleteMessage")
	if got := tg.Last(2, "sendMessage").Str("text"); !strings.HasPrefix(got, "Привет") {
		t.Fatalf("home %q", got)
	}
}
