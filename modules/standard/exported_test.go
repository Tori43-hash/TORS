package standard_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/internal/pgtest"
	"github.com/tori43-hash/tors/internal/tgtest"
)

// TestExported runs a bot.json made in the builder: the export format and the
// bot must stay in step. TORS_BOT_JSON points to another export to try.
func TestExported(t *testing.T) {
	path := os.Getenv("TORS_BOT_JSON")
	if path == "" {
		path = "testdata/builder-bot.json"
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bot map[string]any
	if err := json.Unmarshal(body, &bot); err != nil {
		t.Fatal(err)
	}
	tg := tgtest.New(t)
	apps := bot["apps"].(map[string]any)
	apps["telegram"].(map[string]any)["api_server"] = tg.URL
	body, _ = json.Marshal(bot)
	t.Setenv("BOT_TOKEN", tgtest.Token)
	t.Setenv("DATABASE_URL", pgtest.DSN(t))
	t.Setenv("REMNAWAVE_TOKEN", "secret")
	inst, err := tors.Run(body)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop()
	tg.Text(1, "/start")
	msg := tg.Last(1, "sendMessage")
	if msg.Str("text") == "" || len(msg.Keyboard()) == 0 {
		t.Fatalf("start screen %+v", msg.Params)
	}
	tg.Press(1, 101, msg.Keyboard()[0][0].Data)
	tg.Last(1, "editMessageText")
}
