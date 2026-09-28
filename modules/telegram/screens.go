package telegram

import (
	"context"
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/tori43-hash/tors/modules/kv"
	"github.com/tori43-hash/tors/modules/ui"
	"github.com/tori43-hash/tors/modules/users"
	"github.com/tori43-hash/tors/theme"
)

// Every bot message with buttons keeps its state server-side, keyed by chat
// and message. Buttons carry only an index ("b:3"), so their parameters —
// a subscription id, a plan — cannot be forged by the user.
type navState struct {
	Screen  string            `json:"screen"`
	Params  map[string]string `json:"params,omitzero"`
	Pages   map[string]int    `json:"pages,omitzero"`
	Event   map[string]any    `json:"event,omitzero"`
	History []navEntry        `json:"history,omitzero"`
	Buttons []navButton       `json:"buttons,omitzero"`
	Photo   string            `json:"photo,omitzero"`
}

type navEntry struct {
	Screen string            `json:"screen"`
	Params map[string]string `json:"params,omitzero"`
}

type navButton struct {
	Ref      string            `json:"ref"`
	Kind     string            `json:"kind"`
	Target   string            `json:"target,omitzero"`
	Params   map[string]string `json:"params,omitzero"`
	Outcomes map[string]string `json:"outcomes,omitzero"`
}

// inputState remembers that a screen waits for a text message.
type inputState struct {
	Screen   string            `json:"screen"`
	Params   map[string]string `json:"params,omitzero"`
	Action   string            `json:"action"`
	Param    string            `json:"param"`
	Outcomes map[string]string `json:"outcomes,omitzero"`
}

const stateTTL = 30 * 24 * time.Hour

func navKey(chat int64, msg int) string { return fmt.Sprintf("nav:%d:%d", chat, msg) }
func inputKey(chat int64) string        { return fmt.Sprintf("input:%d", chat) }

func decodeUpdate(r *http.Request, upd *telego.Update) error {
	return jsonv1.NewDecoder(r.Body).Decode(upd)
}

func (a *App) handle(ctx context.Context, upd telego.Update) error {
	switch {
	case upd.PreCheckoutQuery != nil:
		return a.onPreCheckout(ctx, *upd.PreCheckoutQuery)
	case upd.CallbackQuery != nil:
		return a.onCallback(ctx, upd.CallbackQuery)
	case upd.Message != nil:
		return a.onMessage(ctx, upd.Message)
	}
	return nil
}

func (a *App) touch(ctx context.Context, from telego.User) (ui.User, users.User, error) {
	u, created, err := a.users.Touch(ctx, users.Profile{
		TelegramID: from.ID, FirstName: from.FirstName, Username: from.Username, LanguageCode: from.LanguageCode,
	})
	if err != nil {
		return ui.User{}, u, err
	}
	return viewUser(u, created), u, nil
}

func viewUser(u users.User, created bool) ui.User {
	return ui.User{ID: u.ID, TelegramID: u.TelegramID, FirstName: u.FirstName, Username: u.Username, Lang: u.Lang, New: created}
}

func (a *App) onMessage(ctx context.Context, m *telego.Message) error {
	if m.From == nil || m.Chat.Type != "private" {
		return nil
	}
	user, stored, err := a.touch(ctx, *m.From)
	if err != nil {
		return err
	}
	if m.SuccessfulPayment != nil {
		a.mu.RLock()
		hooks := append([]func(context.Context, users.User, telego.SuccessfulPayment) error(nil), a.payments...)
		a.mu.RUnlock()
		for _, h := range hooks {
			if err := h(ctx, stored, *m.SuccessfulPayment); err != nil {
				return err
			}
		}
		return nil
	}
	text := strings.TrimSpace(m.Text)
	if strings.HasPrefix(text, "/") {
		name := strings.ToLower(strings.TrimPrefix(strings.SplitN(strings.Fields(text)[0], "@", 2)[0], "/"))
		target := a.theme.CommandTarget(name, user.New)
		if target == "" {
			return nil
		}
		_ = a.kv.Delete(ctx, inputKey(m.Chat.ID))
		return a.send(ctx, m.Chat.ID, user, &navState{Screen: target})
	}
	in, ok, err := kv.GetJSON[inputState](ctx, a.kv, inputKey(m.Chat.ID))
	if err != nil || !ok || text == "" {
		return err
	}
	params := maps.Clone(in.Params)
	if params == nil {
		params = map[string]string{}
	}
	params[in.Param] = text
	res, err := a.runAction(ctx, in.Action, ui.Call{User: user, Params: params})
	if err != nil {
		return a.sorry(ctx, m.Chat.ID, err)
	}
	target, ok := in.Outcomes[res.Outcome]
	if !ok {
		return fmt.Errorf("действие %s вернуло неизвестный исход %q", in.Action, res.Outcome)
	}
	if target == "" {
		return nil
	}
	maps.Copy(params, res.Params)
	if u, err := a.users.Get(ctx, user.ID); err == nil {
		user = viewUser(u, false)
	}
	return a.send(ctx, m.Chat.ID, user, &navState{Screen: target, Params: params})
}

func (a *App) onCallback(ctx context.Context, q *telego.CallbackQuery) error {
	answer := func(text string, alert bool) {
		_ = a.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: text, ShowAlert: alert})
	}
	if q.Message == nil {
		answer("", false)
		return nil
	}
	chat, msg := q.Message.GetChat().ID, q.Message.GetMessageID()
	st, ok, err := kv.GetJSON[navState](ctx, a.kv, navKey(chat, msg))
	idx, perr := strconv.Atoi(strings.TrimPrefix(q.Data, "b:"))
	if err != nil || !ok || perr != nil || idx < 0 || idx >= len(st.Buttons) {
		answer(a.phrase(q.From.LanguageCode, "stale"), true)
		return err
	}
	user, _, err := a.touch(ctx, q.From)
	if err != nil {
		answer("", false)
		return err
	}
	b := st.Buttons[idx]
	switch b.Kind {
	case theme.KindGoto:
		st.History = append(st.History, navEntry{Screen: st.Screen, Params: st.Params})
		st.Screen, st.Params, st.Pages, st.Event = b.Target, b.Params, nil, nil
	case theme.KindBack:
		if len(st.History) == 0 {
			answer("", false)
			return nil
		}
		prev := st.History[len(st.History)-1]
		st.History = st.History[:len(st.History)-1]
		st.Screen, st.Params, st.Pages, st.Event = prev.Screen, prev.Params, nil, nil
	case theme.KindHome:
		st.History = nil
		st.Screen, st.Params, st.Pages, st.Event = a.theme.CommandTarget("start", false), nil, nil, nil
	case theme.KindPage:
		page, _ := strconv.Atoi(b.Target)
		if st.Pages == nil {
			st.Pages = map[string]int{}
		}
		st.Pages[strings.TrimSuffix(b.Ref, ".p")] = page
	case theme.KindAction:
		res, err := a.runAction(ctx, b.Target, ui.Call{User: user, Params: b.Params})
		if err != nil {
			answer(a.phrase(user.Lang, "error"), true)
			return err
		}
		target, ok := b.Outcomes[res.Outcome]
		if !ok {
			answer("", false)
			return fmt.Errorf("действие %s вернуло неизвестный исход %q", b.Target, res.Outcome)
		}
		if target == "" {
			answer("", false)
			return nil
		}
		params := maps.Clone(b.Params)
		if params == nil {
			params = map[string]string{}
		}
		maps.Copy(params, res.Params)
		if target != st.Screen {
			st.History = append(st.History, navEntry{Screen: st.Screen, Params: st.Params})
		}
		st.Screen, st.Params, st.Pages, st.Event = target, params, nil, nil
		// The action may have changed the user (the language, for one).
		if u, err := a.users.Get(ctx, user.ID); err == nil {
			user = viewUser(u, false)
		}
	default:
		answer("", false)
		return nil
	}
	answer("", false)
	return a.edit(ctx, chat, msg, user, &st)
}

func (a *App) onPreCheckout(ctx context.Context, q telego.PreCheckoutQuery) error {
	a.mu.RLock()
	hooks := append([]func(context.Context, telego.PreCheckoutQuery) error(nil), a.preCheckout...)
	a.mu.RUnlock()
	var fail error
	for _, h := range hooks {
		if err := h(ctx, q); err != nil {
			fail = err
			break
		}
	}
	p := &telego.AnswerPreCheckoutQueryParams{PreCheckoutQueryID: q.ID, Ok: fail == nil}
	if fail != nil {
		p.ErrorMessage = fail.Error()
	}
	return a.bot.AnswerPreCheckoutQuery(ctx, p)
}

func (a *App) runAction(ctx context.Context, name string, c ui.Call) (ui.Result, error) {
	fn, ok := a.ui.Action(name)
	if !ok {
		return ui.Result{}, fmt.Errorf("действие %s не подключено", name)
	}
	return fn(ctx, c)
}

// presentEvent sends the screen of an event to a user as a new message.
func (a *App) presentEvent(ctx context.Context, event string, userID int64, data map[string]any) error {
	if a.manifest == nil {
		return errors.New("бот ещё не запущен")
	}
	target := a.theme.EventTarget(a.manifest, event)
	if target == "" || a.theme.Screens[target] == nil {
		return nil
	}
	u, err := a.users.Get(ctx, userID)
	if err != nil {
		return err
	}
	return a.send(ctx, u.TelegramID, viewUser(u, false), &navState{Screen: target, Event: data})
}

// render runs data sources and conditions and renders the screen, following
// empty-list redirects.
func (a *App) render(ctx context.Context, user ui.User, st *navState) (*theme.Rendered, error) {
	for range 5 {
		s := a.theme.Screens[st.Screen]
		if s == nil {
			return nil, fmt.Errorf("экран %s не найден", st.Screen)
		}
		call := ui.Call{User: user, Params: st.Params}
		data := map[string]any{
			"User":   map[string]any{"FirstName": user.FirstName, "Username": user.Username, "ID": user.TelegramID},
			"Params": toAny(st.Params),
		}
		if st.Event != nil {
			data["Event"] = st.Event
		}
		for alias, name := range s.Data {
			fn, ok := a.ui.Data(name)
			if !ok {
				return nil, fmt.Errorf("источник данных %s не подключён", name)
			}
			v, err := fn(ctx, call)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			data[alias] = templateValue(v)
		}
		conds := map[string]bool{}
		a.theme.EachButton(st.Screen, func(_ string, b *theme.Button) {
			for _, c := range []string{b.VisibleIf, b.DisabledIf} {
				name := strings.TrimPrefix(c, "!")
				if name == "" {
					continue
				}
				if _, done := conds[name]; done {
					continue
				}
				fn, ok := a.ui.Condition(name)
				if !ok {
					conds[name] = false
					continue
				}
				v, err := fn(ctx, call)
				if err != nil {
					a.log.Error("условие", "name", name, "error", err)
				}
				conds[name] = v
			}
		})
		r, err := theme.Render(a.theme, a.manifest, st.Screen, theme.RenderOptions{
			Lang: user.Lang, Params: st.Params, Conditions: conds, Pages: st.Pages, Data: data,
		})
		if err != nil {
			return nil, err
		}
		for _, e := range r.Errors {
			a.log.Warn("шаблон", "screen", e.Screen, "ref", e.Ref, "error", e.Message)
		}
		if r.Redirect == "" {
			return r, nil
		}
		st.Screen, st.Pages = r.Redirect, nil
	}
	return nil, errors.New("слишком много переходов по пустым спискам")
}

// message is a rendered screen ready to go to Telegram.
type message struct {
	photo      string
	photoBelow bool
	html       string
	keyboard   *telego.InlineKeyboardMarkup
	buttons    []navButton
	input      *theme.Input
}

func (a *App) compose(r *theme.Rendered) message {
	var m message
	var texts []string
	firstText := -1
	for i, b := range r.Blocks {
		switch b.Kind {
		case "photo":
			if m.photo == "" {
				m.photo = b.Photo
				m.photoBelow = firstText >= 0
			}
		case "text":
			if firstText < 0 {
				firstText = i
			}
			texts = append(texts, b.Text)
		}
	}
	m.html = toHTML(strings.Join(texts, "\n\n"))
	var rows [][]theme.RenderedButton
	for _, b := range r.Blocks {
		if b.Kind == "buttons" {
			rows = append(rows, b.Buttons)
		}
	}
	rows = append(rows, r.Keyboard...)
	var kb [][]telego.InlineKeyboardButton
	for _, row := range rows {
		var line []telego.InlineKeyboardButton
		for _, b := range row {
			btn := telego.InlineKeyboardButton{Text: b.Label, IconCustomEmojiID: b.Icon}
			if b.Style != "link" {
				btn.Style = b.Style
			}
			switch {
			case b.Disabled:
				btn.Disabled = &telego.DisabledButton{}
			case b.Kind == theme.KindURL:
				btn.URL = b.Target
			case b.Kind == theme.KindWebApp:
				btn.WebApp = &telego.WebAppInfo{URL: b.Target}
			case b.Kind == theme.KindCopy:
				btn.CopyText = &telego.CopyTextButton{Text: b.Target}
			default:
				btn.CallbackData = "b:" + strconv.Itoa(len(m.buttons))
				m.buttons = append(m.buttons, navButton{Ref: b.Ref, Kind: b.Kind, Target: b.Target, Params: b.Params, Outcomes: b.Outcomes})
			}
			line = append(line, btn)
		}
		if len(line) > 0 {
			kb = append(kb, line)
		}
	}
	if len(kb) > 0 {
		m.keyboard = &telego.InlineKeyboardMarkup{InlineKeyboard: kb}
	}
	m.input = r.Input
	return m
}

// send shows a screen as a new message.
func (a *App) send(ctx context.Context, chat int64, user ui.User, st *navState) error {
	r, err := a.render(ctx, user, st)
	if err != nil {
		return a.sorry(ctx, chat, err)
	}
	m := a.compose(r)
	var sent *telego.Message
	if m.photo != "" {
		file, done := a.photo(ctx, m.photo)
		defer done()
		p := &telego.SendPhotoParams{ChatID: tu.ID(chat), Photo: file, Caption: m.html, ParseMode: telego.ModeHTML, ShowCaptionAboveMedia: m.photoBelow}
		if m.keyboard != nil {
			p.ReplyMarkup = m.keyboard
		}
		sent, err = a.bot.SendPhoto(ctx, p)
		a.rememberPhoto(ctx, m.photo, sent)
	} else {
		p := &telego.SendMessageParams{ChatID: tu.ID(chat), Text: m.html, ParseMode: telego.ModeHTML, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}}
		if m.keyboard != nil {
			p.ReplyMarkup = m.keyboard
		}
		sent, err = a.bot.SendMessage(ctx, p)
	}
	if err != nil {
		return err
	}
	return a.remember(ctx, chat, sent.MessageID, st, m)
}

// edit replaces a message with another screen. Telegram cannot turn a text
// message into a photo or back, so such a change sends a new message.
func (a *App) edit(ctx context.Context, chat int64, msg int, user ui.User, st *navState) error {
	r, err := a.render(ctx, user, st)
	if err != nil {
		return a.sorry(ctx, chat, err)
	}
	m := a.compose(r)
	if (st.Photo == "") != (m.photo == "") {
		_ = a.bot.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: tu.ID(chat), MessageID: msg})
		_ = a.kv.Delete(ctx, navKey(chat, msg))
		return a.send(ctx, chat, user, st)
	}
	switch {
	case m.photo == "":
		_, err = a.bot.EditMessageText(ctx, &telego.EditMessageTextParams{ChatID: tu.ID(chat), MessageID: msg, Text: m.html,
			ParseMode: telego.ModeHTML, LinkPreviewOptions: &telego.LinkPreviewOptions{IsDisabled: true}, ReplyMarkup: m.keyboard})
	case m.photo != st.Photo:
		file, done := a.photo(ctx, m.photo)
		defer done()
		var edited *telego.Message
		edited, err = a.bot.EditMessageMedia(ctx, &telego.EditMessageMediaParams{ChatID: tu.ID(chat), MessageID: msg, ReplyMarkup: m.keyboard,
			Media: &telego.InputMediaPhoto{Type: telego.MediaTypePhoto, Media: file, Caption: m.html, ParseMode: telego.ModeHTML, ShowCaptionAboveMedia: m.photoBelow}})
		a.rememberPhoto(ctx, m.photo, edited)
	default:
		_, err = a.bot.EditMessageCaption(ctx, &telego.EditMessageCaptionParams{ChatID: tu.ID(chat), MessageID: msg, Caption: m.html,
			ParseMode: telego.ModeHTML, ShowCaptionAboveMedia: m.photoBelow, ReplyMarkup: m.keyboard})
	}
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		return err
	}
	return a.remember(ctx, chat, msg, st, m)
}

func (a *App) remember(ctx context.Context, chat int64, msg int, st *navState, m message) error {
	st.Buttons, st.Photo = m.buttons, m.photo
	if err := kv.SetJSON(ctx, a.kv, navKey(chat, msg), st, stateTTL); err != nil {
		return err
	}
	if m.input == nil {
		return a.kv.Delete(ctx, inputKey(chat))
	}
	outcomes := a.theme.ResolveOutcomes(a.manifest, m.input.Action, m.input.On)
	return kv.SetJSON(ctx, a.kv, inputKey(chat), inputState{
		Screen: st.Screen, Params: st.Params, Action: m.input.Action, Param: m.input.Param, Outcomes: outcomes,
	}, stateTTL)
}

// photo turns a theme photo into a Telegram file: a URL, a local asset (its
// file_id is cached after the first upload) or a file_id given in the theme.
func (a *App) photo(ctx context.Context, v string) (telego.InputFile, func()) {
	nothing := func() {}
	switch {
	case strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://"):
		return telego.InputFile{URL: v}, nothing
	case isAsset(v):
		if id, ok, _ := a.kv.Get(ctx, "file:"+v); ok {
			return telego.InputFile{FileID: string(id)}, nothing
		}
		f, err := os.Open(filepath.Join(a.Assets, filepath.Clean("/"+v)))
		if err != nil {
			a.log.Error("фото", "file", v, "error", err)
			return telego.InputFile{URL: v}, nothing
		}
		return telego.InputFile{File: f}, func() { _ = f.Close() }
	default:
		return telego.InputFile{FileID: v}, nothing
	}
}

// isAsset tells a local file ("assets/start.jpg") from a Telegram file_id,
// which has neither slashes nor dots.
func isAsset(v string) bool { return strings.ContainsAny(v, "/.") }

func (a *App) rememberPhoto(ctx context.Context, v string, sent *telego.Message) {
	if sent == nil || len(sent.Photo) == 0 || strings.HasPrefix(v, "http") {
		return
	}
	_ = a.kv.Set(ctx, "file:"+v, []byte(sent.Photo[len(sent.Photo)-1].FileID), 0)
}

// sorry tells the user something went wrong without exposing internals.
func (a *App) sorry(ctx context.Context, chat int64, cause error) error {
	a.log.Error("экран", "chat", chat, "error", cause)
	_, err := a.bot.SendMessage(ctx, &telego.SendMessageParams{ChatID: tu.ID(chat), Text: a.phrase("", "error")})
	return errors.Join(cause, err)
}

func (a *App) phrase(lang, key string) string {
	en := strings.HasPrefix(lang, "en")
	switch key {
	case "stale":
		if en {
			return "This message is out of date. Send /start."
		}
		return "Это сообщение устарело. Отправьте /start."
	default:
		if en {
			return "Something went wrong. Please try again later."
		}
		return "Что-то пошло не так. Попробуйте позже."
	}
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// templateValue normalizes data source results for templates: structs become
// maps, slices become []any, integers become int64; times stay as they are.
func templateValue(v any) any {
	switch x := v.(type) {
	case nil, string, bool, float64, time.Time:
		return v
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = templateValue(val)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = templateValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return templateValue(rv.Elem().Interface())
	case reflect.Struct, reflect.Map:
		if rv.Kind() == reflect.Map && rv.Type().Key().Kind() != reflect.String {
			return v
		}
		if rv.Kind() == reflect.Map {
			out := make(map[string]any, rv.Len())
			for it := rv.MapRange(); it.Next(); {
				out[it.Key().String()] = templateValue(it.Value().Interface())
			}
			return out
		}
		return templateValue(ui.Record(v))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint())
	case reflect.Float32:
		return rv.Float()
	case reflect.String:
		return rv.String()
	}
	return v
}
