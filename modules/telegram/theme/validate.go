package theme

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Issue levels.
const (
	LevelError   = "error"
	LevelWarning = "warning"
)

// Issue is a problem found in a theme. Screen and Ref point at the place:
// Ref uses the same notation as RenderedButton.Ref ("k.1.0", "b.2", …).
type Issue struct {
	Level   string `json:"level"`
	Screen  string `json:"screen,omitzero"`
	Ref     string `json:"ref,omitzero"`
	Message string `json:"message"`
}

var (
	reCommand = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	reAlias   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reDigits  = regexp.MustCompile(`^[0-9]+$`)
	reserved  = []string{"User", "Params", "Event"}
)

// Validate checks a theme against a manifest. It never stops at the first
// problem: every issue is reported so the editor can show them all at once.
func Validate(t *Theme, m *Manifest) []Issue {
	v := &validator{t: t, m: m}
	v.run()
	slices.SortStableFunc(v.issues, func(a, b Issue) int {
		return cmp.Or(cmp.Compare(a.Screen, b.Screen), cmp.Compare(a.Ref, b.Ref))
	})
	seen := make(map[Issue]bool, len(v.issues))
	return slices.DeleteFunc(v.issues, func(i Issue) bool {
		dup := seen[i]
		seen[i] = true
		return dup
	})
}

type validator struct {
	t        *Theme
	m        *Manifest
	issues   []Issue
	edges    map[string][]string // screen → screens it can open
	incoming map[string][]string // screen → params it may receive
}

func (v *validator) errorf(screen, ref, format string, args ...any) {
	v.issues = append(v.issues, Issue{Level: LevelError, Screen: screen, Ref: ref, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(screen, ref, format string, args ...any) {
	v.issues = append(v.issues, Issue{Level: LevelWarning, Screen: screen, Ref: ref, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) run() {
	t := v.t
	v.edges = map[string][]string{}
	v.incoming = IncomingParams(t, v.m)
	if t.Version != FormatVersion {
		v.errorf("", "", "Неподдерживаемая версия формата темы: %d (нужна %d)", t.Version, FormatVersion)
	}
	if len(t.Screens) == 0 {
		v.errorf("", "", "В теме нет ни одного экрана")
	}
	v.entries()
	for _, id := range sortedKeys(t.Screens) {
		v.screen(id, t.Screens[id])
	}
	v.reachability()
}

func (v *validator) entries() {
	t, m := v.t, v.m
	if _, ok := t.Commands["start"]; !ok {
		v.errorf("", "command:start", "Нет экрана для команды /start")
	}
	for _, name := range sortedKeys(t.Commands) {
		e := t.Commands[name]
		ref := "command:" + name
		if !reCommand.MatchString(name) {
			v.errorf("", ref, "Команда /%s: допустимы только строчные латинские буквы, цифры и _ (до 32 символов)", name)
		}
		if e.Default == "" {
			v.errorf("", ref, "Команда /%s не ведёт ни на какой экран", name)
		}
		for _, target := range []string{e.Default, e.NewUser} {
			if target == "" {
				continue
			}
			if v.screenExists("", ref, target, "Команда /"+name) {
				v.requireNoParams(ref, target, "Команда /"+name)
			}
		}
	}
	for _, name := range sortedKeys(t.Events) {
		if _, ok := m.Events[name]; !ok {
			v.errorf("", "event:"+name, "Нет такого события: %s", name)
		}
	}
	for _, name := range sortedKeys(m.Events) {
		if target := t.eventTarget(m, name); target != "" {
			v.screenExists("", "event:"+name, target, "Событие «"+m.Events[name].Title+"»")
		}
	}
	for _, action := range sortedKeys(t.Routes) {
		a, ok := m.Actions[action]
		ref := "route:" + action
		if !ok {
			v.errorf("", ref, "Нет такого действия: %s", action)
			continue
		}
		for _, outcome := range sortedKeys(t.Routes[action]) {
			if _, ok := a.outcome(outcome); !ok {
				v.errorf("", ref, "У действия %s нет исхода %s", action, outcome)
				continue
			}
			v.screenExists("", ref, t.Routes[action][outcome], "Исход "+action+" → "+outcome)
		}
	}
}

func (v *validator) screen(id string, s *Screen) {
	for alias, name := range s.Data {
		switch {
		case !reAlias.MatchString(alias):
			v.errorf(id, "data", "Имя данных «%s»: только латинские буквы, цифры и _", alias)
		case slices.Contains(reserved, alias):
			v.errorf(id, "data", "Имя данных «%s» зарезервировано", alias)
		}
		if _, ok := v.m.Data[name]; !ok {
			v.errorf(id, "data", "Нет такого источника данных: %s", name)
		}
	}
	buttons := 0
	for i, b := range s.Blocks {
		ref := "b." + strconv.Itoa(i)
		kinds := 0
		for _, set := range []bool{b.Photo != "", len(b.Text) > 0, b.Buttons != nil} {
			if set {
				kinds++
			}
		}
		if kinds != 1 {
			v.errorf(id, ref, "Блок должен быть чем-то одним: фото, текстом или рядом кнопок")
			continue
		}
		if len(b.Text) > 0 {
			v.translations(id, ref, b.Text)
		}
		if b.Buttons != nil {
			if !slices.Contains([]string{"", "left", "center", "right"}, b.Buttons.Align) {
				v.errorf(id, ref, "Выравнивание кнопок: left, center или right")
			}
			if len(b.Buttons.Items) == 0 {
				v.errorf(id, ref, "Ряд кнопок внутри сообщения пуст")
			}
			if len(b.Buttons.Items) > MaxButtonsPerRow {
				v.errorf(id, ref, "В одном ряду не больше %d кнопок", MaxButtonsPerRow)
			}
			for j := range b.Buttons.Items {
				v.button(id, ref+"."+strconv.Itoa(j), &b.Buttons.Items[j], true)
			}
			buttons += len(b.Buttons.Items)
		}
	}
	hasRepeat := false
	for i, row := range s.Keyboard {
		ref := "k." + strconv.Itoa(i)
		switch {
		case row.Repeat != nil:
			hasRepeat = true
			v.repeat(id, ref, s, row.Repeat)
			buttons += max(row.Repeat.PageSize, 1)
		case row.Use != "":
			frag, ok := v.t.Fragments[row.Use]
			if !ok {
				v.errorf(id, ref, "Нет такого фрагмента: %s", row.Use)
			}
			for fi, frow := range frag {
				v.row(id, fmt.Sprintf("%s.f%d", ref, fi), frow.Buttons)
				buttons += len(frow.Buttons)
			}
		default:
			v.row(id, ref, row.Buttons)
			buttons += len(row.Buttons)
		}
	}
	if buttons > MaxButtons {
		v.errorf(id, "", "На экране не больше %d кнопок", MaxButtons)
	}
	if s.Empty != "" {
		v.screenExists(id, "empty", s.Empty, "Экран для пустого списка")
		v.edge(id, s.Empty)
		if !hasRepeat {
			v.warnf(id, "empty", "Экран для пустого списка задан, но на экране нет списка")
		}
	}
	if s.Input != nil {
		v.input(id, s.Input)
	}
	v.templates(id)
}

func (v *validator) row(id, ref string, buttons []Button) {
	if len(buttons) == 0 {
		v.errorf(id, ref, "Пустой ряд кнопок")
	}
	if len(buttons) > MaxButtonsPerRow {
		v.errorf(id, ref, "В одном ряду не больше %d кнопок", MaxButtonsPerRow)
	}
	for j := range buttons {
		v.button(id, ref+"."+strconv.Itoa(j), &buttons[j], false)
	}
}

func (v *validator) repeat(id, ref string, s *Screen, rp *Repeat) {
	alias, ok := strings.CutPrefix(rp.Source, ".")
	src, bound := s.Data[alias]
	switch {
	case !ok || !bound:
		v.errorf(id, ref, "Список %s не подключён к экрану: добавьте его в данные экрана", rp.Source)
	case !v.m.Data[src].List:
		if _, known := v.m.Data[src]; known {
			v.errorf(id, ref, "%s — не список, по нему нельзя сделать кнопки", rp.Source)
		}
	}
	if rp.Columns < 0 || rp.Columns > MaxButtonsPerRow {
		v.errorf(id, ref, "Колонок в списке: от 1 до %d", MaxButtonsPerRow)
	}
	if rp.PageSize < 0 {
		v.errorf(id, ref, "Размер страницы не может быть отрицательным")
	}
	v.button(id, ref+".r", &rp.Button, false)
}

func (v *validator) button(id, ref string, b *Button, inBody bool) {
	switch n := b.targetCount(); {
	case n == 0:
		v.errorf(id, ref, "Кнопка никуда не ведёт")
	case n > 1:
		v.errorf(id, ref, "У кнопки несколько действий — оставьте одно")
	}
	styles := []string{"", "primary", "success", "danger"}
	if inBody {
		styles = append(styles, "link")
	}
	if !slices.Contains(styles, b.Style) {
		v.errorf(id, ref, "Недопустимый цвет кнопки: %s", b.Style)
	}
	if b.Style == "link" && !slices.Contains([]string{KindGoto, KindAction, KindBack, KindHome}, b.Kind()) {
		v.errorf(id, ref, "Стиль «ссылка» доступен только кнопкам, которые открывают экран или выполняют действие")
	}
	if b.Icon != "" && !reDigits.MatchString(b.Icon) {
		v.errorf(id, ref, "Иконка — это числовой ID кастомного эмодзи")
	}
	if len(b.Text) == 0 && b.Emoji == "" && (b.Action == "" || len(v.m.Actions[b.Action].Label) == 0) {
		v.errorf(id, ref, "У кнопки нет текста")
	}
	if len(b.Text) > 0 {
		v.translations(id, ref, b.Text)
	}
	for _, c := range []string{b.VisibleIf, b.DisabledIf} {
		if name := strings.TrimPrefix(c, "!"); c != "" {
			if _, ok := v.m.Conditions[name]; !ok {
				v.errorf(id, ref, "Нет такого условия: %s", name)
			}
		}
	}
	if len(b.On) > 0 && b.Action == "" {
		v.errorf(id, ref, "Исходы задаются только для кнопки с действием")
	}
	switch b.Kind() {
	case KindGoto:
		if v.screenExists(id, ref, b.Goto, "Кнопка") {
			v.edge(id, b.Goto)
			v.requireParams(id, ref, b.Goto, b.Params, "Кнопка")
		}
	case KindAction:
		v.action(id, ref, b.Action, b.On, b.Params, "")
	case KindWebApp:
		if !strings.Contains(b.WebApp, "{{") && !strings.HasPrefix(b.WebApp, "https://") {
			v.errorf(id, ref, "Mini App открывается только по https://")
		}
	case KindURL:
		if !strings.Contains(b.URL, "{{") && !hasScheme(b.URL) {
			v.errorf(id, ref, "Ссылка должна начинаться с https://, http:// или tg://")
		}
	}
}

func (v *validator) action(id, ref, action string, on, params map[string]string, inputParam string) {
	a, ok := v.m.Actions[action]
	if !ok {
		v.errorf(id, ref, "Нет такого действия: %s", action)
		return
	}
	for _, p := range a.Params {
		if _, ok := params[p]; !ok && p != inputParam {
			v.errorf(id, ref, "Действию %s нужен параметр %s", action, p)
		}
	}
	for _, name := range sortedKeys(on) {
		if _, ok := a.outcome(name); !ok {
			v.errorf(id, ref, "У действия %s нет исхода %s", action, name)
		}
	}
	outcomes := v.t.ResolveOutcomes(v.m, action, on)
	for _, name := range sortedKeys(outcomes) {
		target := outcomes[name]
		o, _ := a.outcome(name)
		what := "Исход «" + cmp.Or(o.Title, name) + "» действия " + action
		switch {
		case target == "" && !o.Terminal:
			v.errorf(id, ref, "%s никуда не ведёт", what)
		case target != "":
			if v.screenExists(id, ref, target, what) {
				v.edge(id, target)
				v.requireParams(id, ref, target, params, what)
			}
		}
	}
}

func (v *validator) input(id string, in *Input) {
	a, ok := v.m.Actions[in.Action]
	if ok && !a.Input {
		v.errorf(id, "input", "Действие %s не принимает ввод текста", in.Action)
	}
	if in.Param == "" {
		v.errorf(id, "input", "Укажите, в какой параметр передать введённый текст")
	}
	params := v.incomingParams(id)
	params[in.Param] = ""
	v.action(id, "input", in.Action, in.On, params, in.Param)
}

// incomingParams is what a screen may receive: params passed to it by other
// screens plus the params its data sources need (checked at the callers).
func (v *validator) incomingParams(id string) map[string]string {
	out := map[string]string{}
	for _, p := range v.incoming[id] {
		out[p] = ""
	}
	for _, p := range v.requiredParams(id) {
		out[p] = ""
	}
	return out
}

func (v *validator) requiredParams(id string) []string {
	s := v.t.Screens[id]
	if s == nil {
		return nil
	}
	var out []string
	for _, name := range s.Data {
		for _, p := range v.m.Data[name].Params {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return out
}

func (v *validator) requireParams(id, ref, target string, params map[string]string, what string) {
	for _, p := range v.requiredParams(target) {
		if _, ok := params[p]; !ok {
			v.errorf(id, ref, "%s ведёт на экран «%s», которому нужен параметр %s — передайте его в параметрах кнопки", what, target, p)
		}
	}
}

func (v *validator) requireNoParams(ref, target, what string) {
	if ps := v.requiredParams(target); len(ps) > 0 {
		v.errorf("", ref, "%s открывает экран «%s», которому нужен параметр %s — команда не может его передать", what, target, strings.Join(ps, ", "))
	}
}

func (v *validator) screenExists(id, ref, target, what string) bool {
	if _, ok := v.t.Screens[target]; ok {
		return true
	}
	v.errorf(id, ref, "%s ведёт на экран «%s», которого нет", what, target)
	return false
}

func (v *validator) edge(from, to string) { v.edges[from] = append(v.edges[from], to) }

func (v *validator) translations(id, ref string, t Text) {
	var missing []string
	for _, l := range v.m.Languages {
		if _, ok := t[l]; !ok {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		v.warnf(id, ref, "Нет перевода: %s", strings.Join(missing, ", "))
	}
	for l := range t {
		if !slices.Contains(v.m.Languages, l) {
			v.warnf(id, ref, "Язык %s не поддерживается ботом", l)
		}
	}
}

// templates renders the screen with sample data in every language and reports
// template errors: unknown variables, bad syntax, unknown functions.
func (v *validator) templates(id string) {
	params := v.incomingParams(id)
	langs := v.m.Languages
	if len(langs) == 0 {
		langs = []string{""}
	}
	for _, lang := range langs {
		r, err := Render(v.t, v.m, id, RenderOptions{Lang: lang, Params: params, ShowHidden: true})
		if err != nil {
			v.errorf(id, "", "%v", err)
			return
		}
		v.issues = append(v.issues, r.Errors...)
	}
}

func (v *validator) reachability() {
	seen := map[string]bool{}
	var queue []string
	push := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			queue = append(queue, id)
		}
	}
	for _, e := range v.t.Commands {
		push(e.Default)
		push(e.NewUser)
	}
	for name := range v.m.Events {
		push(v.t.eventTarget(v.m, name))
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range v.edges[id] {
			push(next)
		}
	}
	for _, id := range sortedKeys(v.t.Screens) {
		if !seen[id] {
			v.warnf(id, "", "Экран недостижим: на него не ведёт ни одна кнопка, команда или событие")
		}
	}
}

func hasScheme(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "tg://")
}

func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
