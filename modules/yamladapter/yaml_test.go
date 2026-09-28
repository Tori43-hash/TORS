package yamladapter_test

import (
	"testing"

	"github.com/tori43-hash/tors"
	_ "github.com/tori43-hash/tors/modules/yamladapter"
)

func TestAdapt(t *testing.T) {
	out, err := tors.Adapt("yaml", []byte("apps:\n  http:\n    listen: \":8080\"\nlogging: {level: debug}\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := tors.ParseConfig(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.Apps["http"]) != `{"listen":":8080"}` || cfg.Logging.Level != "debug" {
		t.Fatalf("got %s / %+v", out, cfg.Logging)
	}
}
