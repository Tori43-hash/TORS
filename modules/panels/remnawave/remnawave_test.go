package remnawave

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	remapi "github.com/Jolymmiles/remnawave-api-go/v3/api"
	"github.com/google/uuid"

	"github.com/tori43-hash/tors"
	"github.com/tori43-hash/tors/modules/panels"
)

// fakePanel emulates the few Remnawave endpoints the provider uses.
type fakePanel struct {
	mu    sync.Mutex
	users map[int]*remapi.UserItemInfo
	next  int
	seen  []string
}

func (f *fakePanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	path := r.URL.Path
	switch {
	case r.Method == "GET" && strings.HasPrefix(path, "/api/users/by-username/"):
		name := strings.TrimPrefix(path, "/api/users/by-username/")
		for _, u := range f.users {
			if u.Username == name {
				f.write(w, 200, u)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"User not found","statusCode":404}`))
	case r.Method == "POST" && path == "/api/users":
		f.next++
		u := &remapi.UserItemInfo{ID: f.next, Username: in["username"].(string), ShortUuid: "s" + strconv.Itoa(f.next), VlessUuid: uuid.New()}
		f.apply(u, in)
		f.users[u.ID] = u
		f.write(w, 201, u)
	case r.Method == "PATCH" && path == "/api/users":
		u := f.users[int(in["id"].(float64))]
		f.apply(u, in)
		f.write(w, 200, u)
	case r.Method == "POST" && strings.HasSuffix(path, "/actions/revoke"):
		id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(path, "/api/users/"), "/actions/revoke"))
		u := f.users[id]
		u.ShortUuid += "r"
		u.SubscriptionUrl = "https://sub.example.com/" + u.ShortUuid
		f.write(w, 200, u)
	case r.Method == "GET" && strings.HasPrefix(path, "/api/users/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/api/users/"))
		f.write(w, 200, f.users[id])
	case r.Method == "DELETE":
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/api/users/"))
		delete(f.users, id)
		w.WriteHeader(204)
	default:
		w.WriteHeader(404)
	}
}

func (f *fakePanel) apply(u *remapi.UserItemInfo, in map[string]any) {
	if v, ok := in["expireAt"].(string); ok {
		u.ExpireAt, _ = time.Parse(time.RFC3339, v)
	}
	if v, ok := in["trafficLimitBytes"].(float64); ok {
		u.TrafficLimitBytes = int(v)
	}
	if v, ok := in["hwidDeviceLimit"].(float64); ok {
		u.HwidDeviceLimit = remapi.NewNilInt(int(v))
	}
	if v, ok := in["status"].(string); ok {
		u.Status = remapi.UserItemInfoStatus(v)
	}
	u.ActiveInternalSquads = nil
	for _, s := range in["activeInternalSquads"].([]any) {
		u.ActiveInternalSquads = append(u.ActiveInternalSquads, remapi.ActiveInternalSquad{UUID: uuid.MustParse(s.(string)), Name: "sq"})
	}
	u.Email, u.Tag, u.Description = remapi.NilString{Null: true}, remapi.NilString{Null: true}, remapi.NilString{Null: true}
	u.TelegramId = remapi.NilInt{Null: true}
	u.SubscriptionUrl = "https://sub.example.com/" + u.ShortUuid
	u.UserTraffic.UsedTrafficBytes = 1 << 30
	u.TrafficLimitStrategy = remapi.UserItemInfoTrafficLimitStrategyNORESET
	u.CreatedAt, u.UpdatedAt = time.Now(), time.Now()
}

func (f *fakePanel) write(w http.ResponseWriter, code int, u *remapi.UserItemInfo) {
	b, err := (&remapi.UserResponse{Response: *u}).MarshalJSON()
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

func TestProvider(t *testing.T) {
	f := &fakePanel{users: map[int]*remapi.UserItemInfo{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	squad := uuid.NewString()
	p := &Panel{URL: srv.URL, Token: "secret", Squads: []string{squad}}
	if err := p.Provision(tors.Context{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exp := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	acc := panels.Account{Name: "17", TelegramID: 42, ExpiresAt: exp, TrafficGB: 100, Devices: 3}
	a, err := p.Ensure(ctx, "", acc)
	if err != nil {
		t.Fatal(err)
	}
	u := f.users[1]
	if a.Ref != "1" || u.Username != "tors_17" || u.TrafficLimitBytes != 100<<30 || u.HwidDeviceLimit.Value != 3 ||
		len(u.ActiveInternalSquads) != 1 || u.ActiveInternalSquads[0].UUID.String() != squad || !u.ExpireAt.Equal(exp) {
		t.Fatalf("created %+v %+v", a, u)
	}
	// A retry without the ref finds the account by username instead of duplicating it.
	again, err := p.Ensure(ctx, "", acc)
	if err != nil || again.Ref != "1" || len(f.users) != 1 {
		t.Fatalf("retry %+v %v %d", again, err, len(f.users))
	}
	acc.ExpiresAt = exp.Add(30 * 24 * time.Hour)
	if _, err := p.Ensure(ctx, "1", acc); err != nil || !f.users[1].ExpireAt.Equal(acc.ExpiresAt) || f.users[1].Status != "ACTIVE" {
		t.Fatalf("extend %v %+v", err, f.users[1])
	}
	r, err := p.Revoke(ctx, "1")
	if err != nil || r.URL != "https://sub.example.com/s1r" {
		t.Fatalf("revoke %+v %v", r, err)
	}
	us, err := p.Usage(ctx, "1")
	if err != nil || us.UsedBytes != 1<<30 {
		t.Fatalf("usage %+v %v", us, err)
	}
	if err := p.Delete(ctx, "1"); err != nil || len(f.users) != 0 {
		t.Fatalf("delete %v", err)
	}
	if err := p.Delete(ctx, "1"); err != nil {
		t.Fatalf("delete twice %v", err)
	}
}
