package ui

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/tori43-hash/tors"
	torscmd "github.com/tori43-hash/tors/cmd"
	"github.com/tori43-hash/tors/market"
)

func init() {
	torscmd.RegisterCommand(torscmd.Command{
		Name: "describe", Usage: "describe [--package prefix] [-o tors-module.json]",
		Short: "Описать модули для конструктора (свои модули импортируются этим файлом)", Run: cmdDescribe,
	})
	torscmd.RegisterCommand(torscmd.Command{
		Name: "market-index", Usage: "market-index [--verified dir] [-o index.json]",
		Short: "Собрать индекс маркета: официальные модули и проверенные сторонние", Run: cmdIndex,
	})
}

// Describe returns the descriptor of a compiled module, if it has one.
func Describe(info tors.ModuleInfo) (market.Module, bool) {
	inst := info.New()
	d, ok := inst.(market.Describer)
	if !ok {
		return market.Module{}, false
	}
	m := market.Module{ID: string(info.ID), Package: info.PackagePath(), Info: d.Market()}
	if p, ok := inst.(Provider); ok {
		c := p.UI()
		m.UI = &market.UI{Data: c.Data, Actions: c.Actions, Conditions: c.Conditions, Events: c.Events, Screens: c.Screens}
	}
	return m, true
}

// version returns the Go module version a package is built from.
func version(pkg string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	best, v := "", ""
	for _, d := range append([]*debug.Module{&bi.Main}, bi.Deps...) {
		if (pkg == d.Path || strings.HasPrefix(pkg, d.Path+"/")) && len(d.Path) > len(best) {
			best, v = d.Path, d.Version
			if d.Replace != nil && d.Replace.Version != "" {
				v = d.Replace.Version
			}
		}
	}
	if v == "(devel)" {
		return ""
	}
	return v
}

func cmdDescribe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	prefix := fs.String("package", "", "только модули из пакетов с этим префиксом, например github.com/you/tors-wallet")
	out := fs.String("o", "", "файл (по умолчанию — вывод в терминал)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pack := market.Pack{Version: 1}
	for _, info := range tors.Modules() {
		if !strings.HasPrefix(info.PackagePath(), *prefix) {
			continue
		}
		if m, ok := Describe(info); ok {
			m.Version = version(m.Package)
			pack.Modules = append(pack.Modules, m)
		}
	}
	if len(pack.Modules) == 0 {
		return errors.New("нет модулей с описанием для конструктора (метод Market)")
	}
	return write(*out, pack)
}

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("market-index", flag.ContinueOnError)
	verified := fs.String("verified", "", "папка с проверенными tors-module.json сторонних авторов")
	out := fs.String("o", "", "файл (по умолчанию — вывод в терминал)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pack := market.Pack{Version: 1, Core: version(torscmd.CorePath)}
	seen := map[string]string{}
	for _, info := range tors.Modules() {
		pkg := info.PackagePath()
		if pkg != torscmd.CorePath && !strings.HasPrefix(pkg, torscmd.CorePath+"/") {
			continue
		}
		if m, ok := Describe(info); ok {
			m.Trust = market.Official
			pack.Modules = append(pack.Modules, m)
			seen[m.ID] = "официальный модуль"
		}
	}
	if *verified != "" {
		files, err := filepath.Glob(filepath.Join(*verified, "*.json"))
		if err != nil {
			return err
		}
		slices.Sort(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			var p market.Pack
			if err := json.Unmarshal(b, &p, json.RejectUnknownMembers(true)); err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			for _, m := range p.Modules {
				switch {
				case m.Version == "" || m.Version == "latest":
					return fmt.Errorf("%s: у %s нет версии — проверяется только конкретная версия", f, m.ID)
				case strings.HasPrefix(m.Package, torscmd.CorePath+"/"):
					return fmt.Errorf("%s: %s выдаёт себя за официальный модуль", f, m.ID)
				case seen[m.ID] != "":
					return fmt.Errorf("%s: модуль %s уже есть (%s)", f, m.ID, seen[m.ID])
				}
				m.Trust = market.Verified
				seen[m.ID] = f
				pack.Modules = append(pack.Modules, m)
			}
		}
	}
	return write(*out, pack)
}

func write(path string, pack market.Pack) error {
	b, err := json.Marshal(pack, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
