package theme

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Execute renders a text template with the theme functions for lang.
func Execute(src string, data any, lang string) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	tpl, err := template.New("t").Option("missingkey=error").Funcs(funcs(lang)).Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func funcs(lang string) template.FuncMap {
	return template.FuncMap{
		"date":     func(v any) (string, error) { return formatTime(v, lang, false) },
		"datetime": func(v any) (string, error) { return formatTime(v, lang, true) },
		"bytes":    func(v any) (string, error) { return formatBytes(v, lang) },
		"plural":   plural,
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
	}
}

func formatTime(v any, lang string, withClock bool) (string, error) {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case string:
		p, err := time.Parse(time.RFC3339, x)
		if err != nil {
			return "", fmt.Errorf("date: %q is not a time", x)
		}
		t = p
	default:
		return "", fmt.Errorf("date: %T is not a time", v)
	}
	layout := "02.01.2006"
	if lang == "en" {
		layout = "Jan 2, 2006"
	}
	if withClock {
		layout += " 15:04"
	}
	return t.Format(layout), nil
}

func formatBytes(v any, lang string) (string, error) {
	n, ok := toFloat(v)
	if !ok {
		return "", fmt.Errorf("bytes: %T is not a number", v)
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	if lang == "ru" {
		units = []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}
	}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	s := strconv.FormatFloat(math.Round(n*10)/10, 'f', -1, 64)
	if lang == "ru" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s + " " + units[i], nil
}

// plural picks a word form: two forms follow English rules, three follow Russian.
func plural(v any, forms ...string) (string, error) {
	f, ok := toFloat(v)
	if !ok {
		return "", fmt.Errorf("plural: %T is not a number", v)
	}
	n := int64(math.Abs(f))
	switch len(forms) {
	case 2:
		if n == 1 {
			return forms[0], nil
		}
		return forms[1], nil
	case 3:
		switch {
		case n%10 == 1 && n%100 != 11:
			return forms[0], nil
		case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
			return forms[1], nil
		default:
			return forms[2], nil
		}
	}
	return "", fmt.Errorf("plural: need 2 or 3 forms, got %d", len(forms))
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

var (
	reMissingKey = regexp.MustCompile(`at <([^>]+)>: map has no entry for key`)
	reNoField    = regexp.MustCompile(`at <([^>]+)>: can't evaluate field`)
	reNoFunc     = regexp.MustCompile(`function "([^"]+)" not defined`)
)

// templateMessage turns a text/template error into a message for theme authors.
func templateMessage(err error) string {
	msg := err.Error()
	switch {
	case reMissingKey.MatchString(msg):
		return "Нет такой переменной: " + reMissingKey.FindStringSubmatch(msg)[1]
	case reNoField.MatchString(msg):
		return "Переменную нельзя так использовать: " + reNoField.FindStringSubmatch(msg)[1]
	case reNoFunc.MatchString(msg):
		return "Нет такой функции: " + reNoFunc.FindStringSubmatch(msg)[1]
	}
	if i := strings.Index(msg, ": "); strings.HasPrefix(msg, "template: ") && i >= 0 {
		msg = msg[strings.LastIndex(msg, ": ")+2:]
	}
	return "Ошибка в шаблоне: " + msg
}
