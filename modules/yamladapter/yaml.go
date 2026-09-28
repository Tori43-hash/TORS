// Package yamladapter is config.adapters.yaml: YAML configs for humans.
package yamladapter

import (
	"encoding/json/v2"
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/tori43-hash/tors"
)

func init() { tors.RegisterModule(Adapter{}) }

// Adapter converts YAML to JSON.
type Adapter struct{}

func (Adapter) TorsModule() tors.ModuleInfo {
	return tors.ModuleInfo{ID: "config.adapters.yaml", New: func() tors.Module { return new(Adapter) }}
}

func (Adapter) Adapt(body []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("YAML: %w", err)
	}
	return json.Marshal(v, json.Deterministic(true))
}
