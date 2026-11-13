# Shepherd extensions

A Shepherd extension in one Go binary: a web page (for Shepherd's Extensions, or any browser), the HTTP API behind it,
a CLI over the same commands for agents, and the install that puts it where Shepherd finds it. This repo is the Go
package that builds one, `paddock`, and a template to start from (`examples/demo`). Standard library only.

```go
import paddock "github.com/getshepherd/extensions"

//go:embed web
var web embed.FS

func main() {
	app := paddock.New("demo", "An example Shepherd web page", web)
	paddock.Command(app, "add", "Add a note", func(c *paddock.Ctx, in struct {
		Text string `json:"text" help:"the note's text"`
	}) (Note, error) {
		n := save(in.Text)
		c.Emit("changed", n) // open pages hear it
		return n, nil
	})
	app.Run()
}
```

One command is three things at once:

- `POST /api/add` for the page: `import { call, on } from '/_paddock/paddock.js'`, then `await call('add', { text })`
  and `on('changed', reload)`;
- `demo add --text …` for agents and shells, printing JSON. It runs on the service, so an open page sees the change;
- a section of the skill `demo skill` prints, when the extension has one (below).

Fields of type string, bool, int, float64 and `[]string` become flags; `--json '{…}'` takes the rest.

An extension has a skill for agents only when it says so: its text (when to call the commands, what a task looks like)
in a `skill.md` beside `web/`, followed by the commands. Without one, the commands are the page's and the shell's, and
agents aren't told about them. Shepherd hands a skill to agents that ask it (`shepherd skill demo`).

```go
//go:embed skill.md
var skill string

app.Skill(skill)
```

The skill lives in the binary: nothing is written to `~/.claude/skills` or any other agent's folder, which may belong
to another orchestrator.

## The binary

```
demo install [--port N] [--start login|shepherd]
                           copies itself into Shepherd's extensions, starts it; prints the URL.
                           login (default): a launchd agent (macOS) or systemd user unit (Linux), from login on;
                           shepherd: no service, Shepherd runs it while it is open. Installing again updates it.
demo uninstall [--purge]   the service and the binary go; the data stays unless --purge
demo url                   http://127.0.0.1:<port>/?token=…
demo status | start | stop | logs [-f]
demo serve                 in the foreground, as the service runs it
demo skill                 the SKILL.md for agents: skill.md, then the commands; empty without skill.md
demo <command> [flags]
```

Each extension is a folder in Shepherd's own config folder, `extensions/<name>/`: macOS
`~/Library/Application Support/Shepherd`, Linux `~/.config/shepherd` (`$XDG_CONFIG_HOME`), Windows
`%APPDATA%\Shepherd`. It holds the binary, `state.json` (0600), the log, and whatever the extension keeps
(`App.Dir()`). Port and token survive reinstalls, so a URL saved anywhere keeps working.

## For Shepherd

Shepherd lists `<config dir>/extensions/*/` that hold `state.json` and the binary `<name>` (`<name>.exe` on Windows):

```json
{ "name": "demo", "description": "…", "port": 50856, "token": "…", "start": "login" }
```

Its page is `http://127.0.0.1:<port>/?token=<token>`. With `"start": "shepherd"`, Shepherd runs `<binary> serve`
while it is open (output to `log` in the folder) and restarts it when the binary changes; with `"login"`, the system
runs it. Switching is `<binary> install --start login|shepherd`, removing is `<binary> uninstall`, and installing
from GitHub is the release asset `<repo>-<os>-<arch>` (`.exe` on Windows) downloaded and run as `install`: the
template's release workflow publishes those on every tag.

Agents learn an extension from Shepherd: `shepherd skill` lists the extensions whose `<binary> skill` prints
something, with the `description` of its front matter, and `shepherd skill <name>` prints that skill whole.
Installing one is all it takes for agents to find it.

A page opens in Shepherd's main area, from the sidebar, or as a tab of a workspace's right panel, from its `+`. Shepherd
tells it what it shows: `window.shepherd` and a `shepherd` event on `window` with the same as its `detail`, sent once
the page loads, each time it is shown and when what it shows changes:

```json
{
  "dir": "/Users/me/.herdr/worktrees/app/fix-login",
  "workspace": { "id": "w1", "label": "fix-login" },
  "project": { "root": "/Users/me/src/app", "key": "/Users/me/src/app/.git" },
  "worktree": { "path": "/Users/me/.herdr/worktrees/app/fix-login", "branch": "fix-login", "linked": true }
}
```

In the right panel that is the panel's workspace; in the main area, the workspace shown. `dir` is the folder of the
workspace's focused pane; `project` and `worktree` are `null` for a workspace outside git, `branch` for a detached
HEAD, and all of it when no workspace is shown.

## The page

Put it in `web/` next to `main.go`. `/_paddock/paddock.css` dresses plain HTML in both themes; `/_paddock/paddock.js`
has `call` and `on`, and importing it brings the rest. No build step, no Shadow DOM: a page's own CSS reaches everything.

- buttons: primary by default, `.secondary`, `.quiet`, `.danger`, `.small`, `.icon`; `aria-busy="true"` spins;
- fields, checkboxes, radios, and a switch: `<input type="checkbox" role="switch">`;
- loading: `<p-spinner>` (Zeron's 3×3 matrix) and `<p-spinner mini>` (2×3, in the text's color), `<progress>`,
  `.skeleton`;
- `<p-icon name="folder">`: Solar's Linear icons, the set Shepherd uses, in the text's color
  (`/_paddock/icons.json` lists the 1467 names; the demo's Icons tab searches them);
- `<dialog>` as is, and `await confirm('Delete?', { ok: 'Delete', danger: true })`;
- `toast('Saved', { kind: 'success' })`;
- `<kbd>⌘</kbd> <kbd>K</kbd>`: key caps, as the command panel shows a chord (a menu item's sits at its right end);
- `<p-tabs>`: a `<nav>` of buttons, then a `<section>` each;
- a menu: `<button popovertarget="m">` and `<menu id="m" popover>` of buttons; it opens under its button, and arrows,
  Home/End, Enter and Esc work in it;
- `<table>` (a sticky, muted header; rows light up on hover) and `<ul class="rows">` for a list that does too;
- focus, visible on everything the keyboard reaches; Shepherd's scrollbars; `prefers-reduced-motion` respected.

`make run` serves `examples/demo`, which shows all of it.

The look is Shepherd's own (Zeron's widgets, the Geist font, 28 px controls), and the theme is CSS variables only,
named after Shepherd's theme tokens: `--background --shell --raised --card --dialog --input --hover --selected --kbd
--focus --scrollbar --scrollbar-active --border --border-strong --text --text-muted --text-faint --accent --on-accent
--danger --warning --success --on-color --scrim --shadow --switch-thumb --switch-thumb-edge --switch-off
--switch-on-edge --spinner-1..3 --font --mono --font-size`. The defaults are Shepherd's default themes (Ayu Light and
Dark, by `prefers-color-scheme`); Shepherd sets them on `:root` to bring its own theme, and so can a page.

## Security

The server listens on 127.0.0.1 only, answers only to loopback `Host`s (no DNS rebinding) and only with the token: the
URL's `?token=` becomes a `SameSite=Strict` cookie named after the extension, and the CLI sends it as a bearer.

## Start one

```sh
go run golang.org/x/tools/cmd/gonew@latest github.com/getshepherd/extensions/examples/demo github.com/you/weather
cd weather   # change the name in main.go's paddock.New
go run . install --start shepherd
```

Go is needed only to write one: `go run . install` builds it and installs the result, and every change is another
`go run . install`. To share it, push the repo and a tag (`git tag v0.1.0 && git push --tags`): the template's
`.github/workflows/release.yml` attaches a binary for every system, and anyone installs it from Shepherd's
Extensions with `you/weather`, no Go needed.

In this repo, `make run` serves the demo and prints its URL (go.work builds it against this checkout).

Not here yet: a Windows service (`serve` runs there in the foreground), starting on demand.

Icons: Solar icon set, Linear style (`icons/solar-linear.json`), by 480 Design
(https://www.figma.com/community/file/1166831539721848736), Creative Commons Attribution 4.0
(https://creativecommons.org/licenses/by/4.0/), as published at https://iconify.design.

Fonts: Geist and Geist Mono (`ui/fonts`), by Vercel, SIL Open Font License 1.1 (`ui/fonts/Geist-OFL.txt`).
