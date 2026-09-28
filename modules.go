// Package tors is the core of the TORS bot: a registry of modules, their
// lifecycle, configuration and the event bus. It knows nothing about
// Telegram, VPN panels or payments — all of that lives in modules.
package tors

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// ModuleID names a module inside a namespace: "panels.providers.remnawave"
// is module "remnawave" in namespace "panels.providers". Top-level modules
// (apps) have an empty namespace.
type ModuleID string

// Namespace is the ID without its last segment.
func (id ModuleID) Namespace() string {
	i := strings.LastIndex(string(id), ".")
	if i < 0 {
		return ""
	}
	return string(id)[:i]
}

// Name is the last segment of the ID.
func (id ModuleID) Name() string {
	return string(id)[strings.LastIndex(string(id), ".")+1:]
}

// ModuleInfo describes a registered module.
type ModuleInfo struct {
	ID ModuleID
	// New returns a new, empty instance of the module. It must have no side effects.
	New func() Module
}

// Module is implemented by every module.
type Module interface {
	TorsModule() ModuleInfo
}

var (
	registryMu sync.RWMutex
	registry   = map[ModuleID]ModuleInfo{}
	reID       = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)
)

// RegisterModule adds a module to the registry. Call it from init();
// it panics on an invalid or duplicate ID.
func RegisterModule(m Module) {
	info := m.TorsModule()
	if !reID.MatchString(string(info.ID)) {
		panic(fmt.Sprintf("tors: invalid module ID %q", info.ID))
	}
	if info.New == nil {
		panic(fmt.Sprintf("tors: module %s has no New function", info.ID))
	}
	if v := info.New(); v == nil || reflect.TypeOf(v).Kind() != reflect.Pointer {
		panic(fmt.Sprintf("tors: module %s: New must return a pointer", info.ID))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[info.ID]; ok {
		panic(fmt.Sprintf("tors: module %s is already registered", info.ID))
	}
	registry[info.ID] = info
}

// GetModule returns a registered module by ID.
func GetModule(id string) (ModuleInfo, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	info, ok := registry[ModuleID(id)]
	if !ok {
		return ModuleInfo{}, fmt.Errorf("модуль %s не найден: он не вкомпилирован в этот бинарь", id)
	}
	return info, nil
}

// GetModules returns the modules directly inside a namespace, sorted by ID.
func GetModules(namespace string) []ModuleInfo {
	var out []ModuleInfo
	for _, m := range Modules() {
		if m.ID.Namespace() == namespace {
			out = append(out, m)
		}
	}
	return out
}

// Modules returns every registered module, sorted by ID.
func Modules() []ModuleInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]ModuleInfo, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b ModuleInfo) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return out
}

// PackagePath returns the Go package that implements the module.
func (m ModuleInfo) PackagePath() string {
	t := reflect.TypeOf(m.New())
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.PkgPath()
}
