package httpserver_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/modules/httpserver"
)

func TestServer(t *testing.T) {
	inst, err := tors.Run([]byte(`{"apps": {"http": {"listen": "127.0.0.1:0"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Stop()
	v, _ := inst.App("http")
	h := v.(*httpserver.App)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hook")) })
	if err := h.Mount("a", "POST /hooks/x", ok); err != nil {
		t.Fatal(err)
	}
	if err := h.Mount("b", "POST /hooks/x", ok); err == nil {
		t.Fatal("duplicate route accepted")
	}
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200} {
		resp, err := http.Get("http://" + h.Addr() + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s → %d", path, resp.StatusCode)
		}
	}
	resp, err := http.Post("http://"+h.Addr()+"/hooks/x", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hook" {
		t.Fatalf("hook body %q", body)
	}
	if _, err := h.URL("/x"); err == nil {
		t.Fatal("URL without public_url")
	}
}
