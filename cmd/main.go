// Package torscmd is the command line of TORS. A distribution is a main
// package that imports the modules it needs and calls Main.
package torscmd

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	"github.com/tori43-hash/tors"
)

// Command is a subcommand of the tors binary. Modules may register their own.
type Command struct {
	Name  string
	Usage string
	Short string
	Run   func(args []string) error
}

var commands = map[string]Command{}

// RegisterCommand adds a subcommand; call it from init().
func RegisterCommand(c Command) {
	if _, ok := commands[c.Name]; ok {
		panic("torscmd: command " + c.Name + " is already registered")
	}
	commands[c.Name] = c
}

func init() {
	RegisterCommand(Command{Name: "run", Usage: "run [--config tors.json]", Short: "Запустить бота", Run: cmdRun})
	RegisterCommand(Command{Name: "validate", Usage: "validate [--config tors.json]", Short: "Проверить конфигурацию, не запуская бота", Run: cmdValidate})
	RegisterCommand(Command{Name: "adapt", Usage: "adapt [--config tors.yaml]", Short: "Показать конфигурацию в JSON", Run: cmdAdapt})
	RegisterCommand(Command{Name: "list-modules", Usage: "list-modules [--namespace ns]", Short: "Список вкомпилированных модулей", Run: cmdListModules})
	RegisterCommand(Command{Name: "version", Usage: "version", Short: "Версии ядра и модулей", Run: cmdVersion})
	RegisterCommand(Command{Name: "build", Usage: "build [--config bot.json] [-o tors] [--replace path]", Short: "Собрать бинарь с модулями из конфигурации", Run: cmdBuild})
}

// Main runs the command line and exits.
func Main() {
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		os.Exit(0)
	}
	c, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err := c.Run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Использование: tors <команда> [флаги]\n\nКоманды:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		fmt.Printf("  %-44s %s\n", commands[n].Usage, commands[n].Short)
	}
}

func configFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("TORS_CONFIG")
	if def == "" {
		def = "tors.json"
	}
	return fs.String("config", def, "файл конфигурации (.json, .yaml)")
}

// LoadConfigFile reads a config file, converting it to JSON by its extension.
func LoadConfigFile(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	adapter := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if adapter == "yml" {
		adapter = "yaml"
	}
	return tors.Adapt(adapter, body)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := LoadConfigFile(*path)
	if err != nil {
		return err
	}
	inst, err := tors.Run(body)
	if err != nil {
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	return inst.Stop()
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	path := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := LoadConfigFile(*path)
	if err != nil {
		return err
	}
	cfg, err := tors.ParseConfig(body)
	if err != nil {
		return err
	}
	inst, err := tors.Load(cfg)
	if err != nil {
		return err
	}
	if err := inst.Stop(); err != nil {
		return err
	}
	fmt.Println("Конфигурация в порядке")
	return nil
}

func cmdAdapt(args []string) error {
	fs := flag.NewFlagSet("adapt", flag.ContinueOnError)
	path := configFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := LoadConfigFile(*path)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(body, '\n'))
	return err
}

func cmdListModules(args []string) error {
	fs := flag.NewFlagSet("list-modules", flag.ContinueOnError)
	ns := fs.String("namespace", "*", "только модули из этого пространства имён (\"\" — приложения)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, m := range tors.Modules() {
		if *ns != "*" && m.ID.Namespace() != *ns {
			continue
		}
		fmt.Printf("%-40s %s\n", m.ID, m.PackagePath())
	}
	return nil
}

func cmdVersion([]string) error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("сведения о сборке недоступны")
	}
	fmt.Printf("%s %s (%s)\n", info.Main.Path, info.Main.Version, info.GoVersion)
	for _, d := range info.Deps {
		if d.Replace != nil {
			fmt.Printf("  %s %s => %s\n", d.Path, d.Version, d.Replace.Path)
			continue
		}
		fmt.Printf("  %s %s\n", d.Path, d.Version)
	}
	return nil
}
