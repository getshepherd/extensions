// Package paddock builds a local extension into one binary: a web page (for Shepherd's Extensions, or any browser),
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
	"runtime"
	"slices"
	"sync"
)

// App is one extension. Make it with New, add commands with Command, and hand os.Args over with Run.
type App struct {
	Name, Description string
	web               fs.FS
	guide             string
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

// Skill gives the extension a skill for agents: text (when to use it, what a task looks like), then its commands, as
// `<binary> skill` prints it. Without it the extension has none, its commands being only its page's. Usually a
// skill.md beside web/, `//go:embed skill.md` into a string.
func (a *App) Skill(text string) { a.guide = text }

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

// Dir is the extension's folder, where Shepherd finds it: <Shepherd's config dir>/extensions/<name>. install puts
// the binary there beside state.json and the log; the extension may keep its data there too.
func (a *App) Dir() string {
	d := filepath.Join(ExtensionsDir(), a.Name)
	_ = os.MkdirAll(d, 0o700)
	return d
}

// ExtensionsDir is where Shepherd looks for extensions, inside its own config folder: macOS's Application Support,
// Windows' roaming AppData, else the XDG config folder (as Shepherd's settings.rs has it).
func ExtensionsDir() string {
	base, _ := os.UserConfigDir()
	shepherd := "Shepherd"
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		shepherd = "shepherd"
	}
	return filepath.Join(base, shepherd, "extensions")
}

// binPath is where install keeps the binary: <Dir>/<name>, .exe on Windows.
func (a *App) binPath() string {
	name := a.Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(a.Dir(), name)
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
		return a.uninstall(args[1:])
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
		return a.skill()
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
	fmt.Printf("%s: %s\n\nSetup:\n", a.Name, a.Description)
	for _, l := range [][2]string{
		{"install [--port N]", "copy into Shepherd's extensions and start it; prints the page's URL"},
		{"  [--start login|shepherd]", "run as a service from login on (default), or only while Shepherd runs"},
		{"uninstall [--purge]", "remove it (its data stays in " + a.Dir() + " unless --purge)"},
		{"start | stop | status", "the service; stop lasts until the next login"},
		{"logs [-f]", "its output"},
		{"url", "the page's address, for Shepherd or a browser"},
		{"serve", "run in the foreground"},
		{"skill", "the SKILL.md that teaches agents this CLI (Shepherd asks for it)"},
	} {
		fmt.Printf("  %-26s %s\n", l[0], l[1])
	}
	if len(a.cmds) > 0 {
		fmt.Println("\nCommands (" + a.Name + " <command> -h for flags):")
		for _, c := range a.cmds {
			fmt.Printf("  %-26s %s\n", c.name, c.help)
		}
	}
}
