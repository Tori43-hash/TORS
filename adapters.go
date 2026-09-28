package tors

import "fmt"

// ConfigAdapter converts a config in another format to JSON. Adapters are
// modules in the "config.adapters" namespace: "config.adapters.yaml".
type ConfigAdapter interface {
	Adapt(body []byte) ([]byte, error)
}

// Adapt converts body with the named adapter; "json" or "" returns it unchanged.
func Adapt(name string, body []byte) ([]byte, error) {
	if name == "" || name == "json" {
		return body, nil
	}
	info, err := GetModule("config.adapters." + name)
	if err != nil {
		return nil, fmt.Errorf("нет адаптера конфигурации %q", name)
	}
	a, ok := info.New().(ConfigAdapter)
	if !ok {
		return nil, fmt.Errorf("модуль %s не является адаптером", info.ID)
	}
	return a.Adapt(body)
}
