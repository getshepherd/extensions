// Package paddock builds a local extension into one binary: a web page (for Shepherd's Web pages, or any browser),
// the HTTP API behind it, a CLI over the same commands for agents, and the service that keeps it running.
package paddock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
)

// App is one extension. Make it with New, add commands with Command, and hand os.Args over with Run.
type App struct {
	Name, Description string
	web               fs.FS
	cmds              []*command
	mu                sync.Mutex
	subs              map[chan string]struct{}
}

type command struct {
	name, help string
	in         reflect.Type
	run        func(*Ctx, []byte) (any, error)
}

// Ctx is what a command runs with: the request's context, and Emit to tell open pages something changed.
type Ctx struct {
	context.Context
	app *App
}

// Emit sends an event to every open page (paddock.js's on(event, fn)).
func (c *Ctx) Emit(event string, data any) { c.app.Emit(event, data) }

// New makes an extension. web holds its page in a web/ directory (`//go:embed web`); nil serves only the API.
func New(name, description string, web fs.FS) *App {
	a := &App{Name: name, Description: description, subs: map[chan string]struct{}{}}
	if web != nil {
		sub, err := fs.Sub(web, "web")
		if err != nil {
			panic(err)
		}
		a.web = sub
	}
	return a
}

var builtins = []string{"serve", "install", "uninstall", "start", "stop", "status", "logs", "url", "skill", "help"}

// Command adds a command, reachable as `POST /api/<name>` from the page and as `<binary> <name> --flags` from a
// shell. In's fields of type string, bool, int, float64 and []string become flags (named by their json tag, described
// by a help tag); --json takes the whole input for the rest. Out goes back as JSON.
func Command[In, Out any](a *App, name, help string, fn func(*Ctx, In) (Out, error)) {
	if slices.Contains(builtins, name) {
		panic("paddock: " + name + " is a built-in command")
	}
	a.cmds = append(a.cmds, &command{name: name, help: help, in: reflect.TypeFor[In](), run: func(c *Ctx, raw []byte) (any, error) {
		var in In
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, fmt.Errorf("bad input: %w", err)
			}
		}
		return fn(c, in)
	}})
}

// Dir is where the extension keeps its state and may keep its data: <user config dir>/paddock/<name>.
func (a *App) Dir() string {
	base, _ := os.UserConfigDir()
	d := filepath.Join(base, "paddock", a.Name)
	_ = os.MkdirAll(d, 0o700)
	return d
}

// Emit sends an event to every open page.
func (a *App) Emit(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := "event: " + event + "\ndata: " + string(b) + "\n\n"
	a.mu.Lock()
	defer a.mu.Unlock()
	for ch := range a.subs {
		select {
		case ch <- msg:
		default: // ponytail: a page 16 events behind loses the rest; it reloads on its next event anyway
		}
	}
}

func (a *App) find(name string) *command {
	for _, c := range a.cmds {
		if c.name == name {
			return c
		}
	}
	return nil
}

// Run does what os.Args say: a built-in (serve, install, …) or one of the extension's commands. It exits on error.
func (a *App) Run() {
	if err := a.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, a.Name+":", err)
		os.Exit(1)
	}
}

func (a *App) run(args []string) error {
	if len(args) == 0 {
		a.usage()
		return nil
	}
	switch args[0] {
	case "serve":
		return a.serve()
	case "install":
		return a.install(args[1:])
	case "uninstall":
		return a.uninstall()
	case "start", "stop":
		return a.service(args[0])
	case "status":
		return a.status()
	case "logs":
		return a.logs(args[1:])
	case "url":
		st, err := a.ensureState(0)
		if err == nil {
			fmt.Println(st.url())
		}
		return err
	case "skill":
		return a.skill(args[1:])
	case "help", "-h", "--help":
		a.usage()
		return nil
	}
	if c := a.find(args[0]); c != nil {
		return a.cli(c, args[1:])
	}
	return fmt.Errorf("unknown command %q (see %s help)", args[0], a.Name)
}

func (a *App) usage() {
	fmt.Printf("%s: %s\n\nService:\n", a.Name, a.Description)
	for _, l := range [][2]string{
		{"install [--port N]", "run as a service, now and after login; prints the page's URL"},
		{"uninstall", "remove the service (data stays in " + a.Dir() + ")"},
		{"start | stop | status", "the service; stop lasts until the next login"},
		{"logs [-f]", "the service's output"},
		{"url", "the page's address, for Shepherd's Web pages or a browser"},
		{"serve", "run in the foreground"},
		{"skill [--install]", "a SKILL.md that teaches agents this CLI"},
	} {
		fmt.Printf("  %-22s %s\n", l[0], l[1])
	}
	if len(a.cmds) > 0 {
		fmt.Println("\nCommands (" + a.Name + " <command> -h for flags):")
		for _, c := range a.cmds {
			fmt.Printf("  %-22s %s\n", c.name, c.help)
		}
	}
}
