package tors

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config is the whole bot configuration.
type Config struct {
	Logging *Logging `json:"logging,omitzero"`
	// Build lists the Go modules this config needs compiled in; `tors build`
	// uses it to produce the binary, and Load checks it.
	Build *Build    `json:"build,omitzero"`
	Apps  ModuleMap `json:"apps,omitzero"`
}

// Logging configures the process logger.
type Logging struct {
	Level  string `json:"level,omitzero"`  // debug | info | warn | error
	Format string `json:"format,omitzero"` // text | json
}

// Build describes the binary a config is meant for.
type Build struct {
	// Core is the version of github.com/tori43-hash/tors to build with.
	Core    string        `json:"core,omitzero"`
	Modules []BuildModule `json:"modules,omitzero"`
}

// BuildModule is a Go module (or package) with module registrations.
type BuildModule struct {
	Source  string `json:"source"`
	Version string `json:"version,omitzero"`
}

// ModuleMap maps module names to their raw config.
type ModuleMap map[string]jsontext.Value

// ParseConfig substitutes {env.*} and {file.*} placeholders and decodes the
// config strictly: unknown fields are errors.
func ParseConfig(b []byte) (*Config, error) {
	b, err := ReplacePlaceholders(b)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("конфигурация: %w", err)
	}
	if cfg.Apps == nil {
		cfg.Apps = ModuleMap{}
	}
	return &cfg, nil
}

var rePlaceholder = regexp.MustCompile(`\{(env|file)\.([^{}\s]+)\}`)

// ReplacePlaceholders replaces {env.NAME} with an environment variable and
// {file./path} with a file's contents in every JSON string, keeping numbers
// and structure intact. Template text such as "{{ .User.FirstName }}" is left alone.
func ReplacePlaceholders(in []byte) ([]byte, error) {
	if !rePlaceholder.Match(in) {
		return in, nil
	}
	dec := jsontext.NewDecoder(bytes.NewReader(in))
	var out bytes.Buffer
	enc := jsontext.NewEncoder(&out, jsontext.WithIndent("  "))
	var errs []error
	for {
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if tok.Kind() == '"' {
			s := rePlaceholder.ReplaceAllStringFunc(tok.String(), func(m string) string {
				p := rePlaceholder.FindStringSubmatch(m)
				switch p[1] {
				case "env":
					v, ok := os.LookupEnv(p[2])
					if !ok {
						errs = append(errs, fmt.Errorf("переменная окружения %s не задана", p[2]))
					}
					return v
				default:
					b, err := os.ReadFile(p[2])
					if err != nil {
						errs = append(errs, fmt.Errorf("файл %s: %w", p[2], err))
					}
					return strings.TrimRight(string(b), "\r\n")
				}
			})
			tok = jsontext.String(s)
		}
		if err := enc.WriteToken(tok); err != nil {
			return nil, err
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out.Bytes(), nil
}

// Duration is a time.Duration that also understands days and weeks: "30d", "1d12h", "2w".
type Duration time.Duration

var reDays = regexp.MustCompile(`^(\d+(?:\.\d+)?)([dw])`)

// ParseDuration parses a duration with optional d (day) and w (week) units.
func ParseDuration(s string) (time.Duration, error) {
	var total time.Duration
	rest := strings.TrimSpace(s)
	for {
		m := reDays.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		n, _ := strconv.ParseFloat(m[1], 64)
		unit := 24 * time.Hour
		if m[2] == "w" {
			unit *= 7
		}
		total += time.Duration(n * float64(unit))
		rest = rest[len(m[0]):]
	}
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return 0, fmt.Errorf("длительность %q: ожидается например 30d, 12h, 90m", s)
		}
		total += d
	}
	return total, nil
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("длительность должна быть строкой вроде \"30d\" или \"12h\"")
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	v := time.Duration(d)
	if v != 0 && v%(24*time.Hour) == 0 {
		return json.Marshal(strconv.FormatInt(int64(v/(24*time.Hour)), 10) + "d")
	}
	return json.Marshal(v.String())
}

// Size is a number of bytes; config accepts "100GiB", "10GB", "512MiB" or a number.
type Size int64

var sizeUnits = map[string]float64{
	"":   1,
	"b":  1,
	"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
}

var reSize = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([a-zA-Z]*)\s*$`)

// ParseSize parses a size such as "100GiB".
func ParseSize(s string) (int64, error) {
	m := reSize.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("размер %q: ожидается например 100GiB", s)
	}
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if !ok {
		return 0, fmt.Errorf("размер %q: неизвестная единица %s", s, m[2])
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	return int64(math.Round(n * unit)), nil
}

func (s *Size) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case float64:
		*s = Size(x)
	case string:
		n, err := ParseSize(x)
		if err != nil {
			return err
		}
		*s = Size(n)
	default:
		return fmt.Errorf("размер должен быть числом байт или строкой вроде \"100GiB\"")
	}
	return nil
}
