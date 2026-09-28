// Package tgtest is a fake Telegram Bot API server for tests: it records the
// calls a bot makes, answers them plausibly and feeds updates to long polling.
package tgtest

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Token is a well-formed bot token accepted by the fake server.
const Token = "123456:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// Call is one Bot API request.
type Call struct {
	Method string
	Params map[string]any
}

// Str returns a string parameter.
func (c Call) Str(key string) string {
	s, _ := c.Params[key].(string)
	return s
}

// Keyboard returns the labels and callback data of the inline keyboard.
func (c Call) Keyboard() [][]Button {
	markup, _ := c.Params["reply_markup"].(map[string]any)
	rows, _ := markup["inline_keyboard"].([]any)
	var out [][]Button
	for _, r := range rows {
		var line []Button
		for _, b := range r.([]any) {
			m := b.(map[string]any)
			btn := Button{}
			btn.Text, _ = m["text"].(string)
			btn.Data, _ = m["callback_data"].(string)
			btn.URL, _ = m["url"].(string)
			line = append(line, btn)
		}
		out = append(out, line)
	}
	return out
}

// Button is an inline keyboard button as the bot sent it.
type Button struct {
	Text, Data, URL string
}

// Server is the fake Bot API.
type Server struct {
	*httptest.Server
	t *testing.T

	mu      sync.Mutex
	calls   []Call
	updates []json.RawMessage
	nextMsg int
	nextUpd int
	notify  chan struct{}
	// Replies overrides results per method.
	Replies map[string]any
}

// New starts a fake server; it is closed when the test ends.
func New(t *testing.T) *Server {
	s := &Server{t: t, nextMsg: 100, notify: make(chan struct{}, 1), Replies: map[string]any{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	method := parts[len(parts)-1]
	params := map[string]any{}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 20); err == nil {
			for k, v := range r.MultipartForm.Value {
				var j any
				if json.Unmarshal([]byte(v[0]), &j) == nil {
					params[k] = j
				} else {
					params[k] = v[0]
				}
			}
			for k := range r.MultipartForm.File {
				params[k] = "upload"
			}
		}
	default:
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &params)
	}
	if method == "getUpdates" {
		s.reply(w, s.poll(r))
		return
	}
	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: method, Params: params})
	result, ok := s.Replies[method]
	if !ok {
		result = s.defaultResult(method, params)
	}
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	s.reply(w, result)
}

func (s *Server) reply(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (s *Server) defaultResult(method string, p map[string]any) any {
	switch method {
	case "getMe":
		return map[string]any{"id": 123456, "is_bot": true, "first_name": "Test", "username": "test_bot"}
	case "sendMessage", "sendPhoto", "sendInvoice":
		s.nextMsg++
		msg := map[string]any{"message_id": s.nextMsg, "date": time.Now().Unix(), "chat": map[string]any{"id": p["chat_id"], "type": "private"}}
		if method == "sendPhoto" {
			msg["photo"] = []any{map[string]any{"file_id": fmt.Sprintf("photo-%d", s.nextMsg), "file_unique_id": "u", "width": 1, "height": 1}}
		}
		return msg
	case "editMessageText", "editMessageCaption", "editMessageMedia":
		return map[string]any{"message_id": p["message_id"], "date": time.Now().Unix(), "chat": map[string]any{"id": p["chat_id"], "type": "private"}}
	case "getChatMember":
		return map[string]any{"status": "member", "user": map[string]any{"id": p["user_id"], "is_bot": false, "first_name": "x"}}
	}
	return true
}

func (s *Server) poll(r *http.Request) []json.RawMessage {
	deadline := time.After(200 * time.Millisecond)
	for {
		s.mu.Lock()
		if len(s.updates) > 0 {
			out := s.updates
			s.updates = nil
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		select {
		case <-deadline:
			return []json.RawMessage{}
		case <-r.Context().Done():
			return []json.RawMessage{}
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Push queues an update; update_id is filled in.
func (s *Server) Push(update map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextUpd++
	update["update_id"] = s.nextUpd
	b, _ := json.Marshal(update)
	s.updates = append(s.updates, b)
}

// User is the Telegram user of test updates.
func User(id int64) map[string]any {
	return map[string]any{"id": id, "is_bot": false, "first_name": "Анна", "username": "anna", "language_code": "ru"}
}

// Text pushes a private text message from a user.
func (s *Server) Text(from int64, text string) {
	s.Push(map[string]any{"message": map[string]any{
		"message_id": 1, "date": time.Now().Unix(), "text": text,
		"from": User(from), "chat": map[string]any{"id": from, "type": "private"},
	}})
}

// Press pushes a press of a button of a bot message.
func (s *Server) Press(from int64, msg int, data string) {
	s.Push(map[string]any{"callback_query": map[string]any{
		"id": fmt.Sprintf("cq%d", time.Now().UnixNano()), "from": User(from), "chat_instance": "ci", "data": data,
		"message": map[string]any{"message_id": msg, "date": time.Now().Unix(), "chat": map[string]any{"id": from, "type": "private"}},
	}})
}

// Calls returns the recorded calls, optionally of some methods only.
func (s *Server) Calls(methods ...string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if len(methods) == 0 || contains(methods, c.Method) {
			out = append(out, c)
		}
	}
	return out
}

// Wait waits until n calls of the methods have been made and returns them.
func (s *Server) Wait(n int, methods ...string) []Call {
	s.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if c := s.Calls(methods...); len(c) >= n {
			return c
		}
		select {
		case <-s.notify:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			s.t.Fatalf("ждали %d вызовов %v, есть %d: %+v", n, methods, len(s.Calls(methods...)), s.Calls())
		}
	}
}

// Last waits for the n-th call of the methods and returns it.
func (s *Server) Last(n int, methods ...string) Call {
	s.t.Helper()
	return s.Wait(n, methods...)[n-1]
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
