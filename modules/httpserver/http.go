// Package httpserver is the "http" app: one HTTP server for webhooks and
// health checks. Modules mount their routes during Provision.
package httpserver

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tori43-hash/tors"
)

func init() { tors.RegisterModule(new(App)) }

// App is the HTTP server.
type App struct {
	Listen string `json:"listen,omitzero"` // default ":8080"
	// PublicURL is how Telegram and payment systems reach this server, e.g. https://bot.example.com.
	PublicURL string `json:"public_url,omitzero"`

	mu     sync.Mutex
	mux    *http.ServeMux
	routes map[string]string
	srv    *http.Server
	ln     net.Listener
	health func(context.Context) map[string]error
}

func (*App) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "http", New: func() tors.Module { return new(App) }}
}

func (a *App) Provision(ctx tors.Context) error {
	if a.Listen == "" {
		a.Listen = ":8080"
	}
	a.PublicURL = strings.TrimRight(a.PublicURL, "/")
	a.mux = http.NewServeMux()
	a.routes = map[string]string{}
	a.health = ctx.Health
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	a.mux.HandleFunc("GET /readyz", a.ready)
	return nil
}

// Mount adds a route; the same pattern mounted twice is an error.
func (a *App) Mount(owner tors.ModuleID, pattern string, h http.Handler) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if prev, ok := a.routes[pattern]; ok {
		return fmt.Errorf("маршрут %s уже занят модулем %s", pattern, prev)
	}
	a.routes[pattern] = string(owner)
	a.mux.Handle(pattern, h)
	return nil
}

// URL returns the public address of a path, or an error if public_url is not set.
func (a *App) URL(path string) (string, error) {
	if a.PublicURL == "" {
		return "", errors.New("в модуле http не задан public_url — адрес, по которому сервер доступен снаружи")
	}
	return a.PublicURL + path, nil
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{}
	code := http.StatusOK
	for name, err := range a.health(r.Context()) {
		status[name] = "ok"
		if err != nil {
			status[name] = err.Error()
			code = http.StatusServiceUnavailable
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.MarshalWrite(w, status, json.Deterministic(true))
}

func (a *App) Start() error {
	ln, err := net.Listen("tcp", a.Listen)
	if err != nil {
		return err
	}
	a.ln = ln
	a.srv = &http.Server{Handler: a.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = a.srv.Serve(ln) }()
	return nil
}

func (a *App) Stop() error {
	if a.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := a.srv.Shutdown(ctx)
	a.srv = nil
	return err
}

// Addr is the address the server listens on (useful with ":0").
func (a *App) Addr() string {
	if a.ln == nil {
		return ""
	}
	return a.ln.Addr().String()
}
