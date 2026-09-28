package theme

import (
	"maps"
	"time"
)

// SampleData builds template data for a screen from manifest samples, the way
// the bot would from real data sources. Params are passed through as .Params.
func SampleData(t *Theme, m *Manifest, screenID string, params map[string]string) map[string]any {
	data := map[string]any{
		"User":   sampleRecord(m.User, nil),
		"Params": stringMap(params),
	}
	s := t.Screens[screenID]
	if s == nil {
		return data
	}
	for alias, name := range s.Data {
		src, ok := m.Data[name]
		if !ok {
			continue
		}
		if src.List {
			items := make([]any, 0, len(src.Samples))
			for _, raw := range src.Samples {
				items = append(items, sampleRecord(src.Fields, raw))
			}
			if len(src.Samples) == 0 {
				items = append(items, sampleRecord(src.Fields, nil))
			}
			data[alias] = items
			continue
		}
		var raw map[string]any
		if len(src.Samples) > 0 {
			raw = src.Samples[0]
		}
		data[alias] = sampleRecord(src.Fields, raw)
	}
	event := map[string]any{}
	for name, ev := range m.Events {
		if t.eventTarget(m, name) == screenID {
			maps.Copy(event, sampleRecord(ev.Fields, nil))
		}
	}
	if len(event) > 0 {
		data["Event"] = event
	}
	return data
}

func sampleRecord(fields []Field, raw map[string]any) map[string]any {
	rec := make(map[string]any, len(fields))
	for _, f := range fields {
		v, ok := raw[f.Name]
		if !ok || v == nil {
			v = f.Sample
		}
		rec[f.Name] = normalize(f.Type, v)
	}
	return rec
}

// normalize converts JSON sample values to the Go types templates will see.
func normalize(typ string, v any) any {
	switch typ {
	case "int", "bytes":
		if f, ok := toFloat(v); ok {
			return int64(f)
		}
		if typ == "bytes" {
			return int64(10 << 30)
		}
		return int64(1)
	case "bool":
		if b, ok := v.(bool); ok {
			return b
		}
		return true
	case "time":
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
		return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	case "url":
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		return "https://example.com"
	default:
		if s, ok := v.(string); ok {
			return s
		}
		if v == nil {
			return "…"
		}
		return v
	}
}

func stringMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
