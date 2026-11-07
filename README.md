# paddock

A Shepherd extension in one Go binary: a web page (for Shepherd's Extensions, or any browser), the HTTP API behind it,
a CLI over the same commands for agents, and the install that puts it where Shepherd finds it. Standard library only.

```go
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
- a section of the skill `demo install` writes to `~/.claude/skills/demo/SKILL.md`.

Fields of type string, bool, int, float64 and `[]string` become flags; `--json '{…}'` takes the rest.

## The binary

```
demo install [--port N] [--start login|shepherd]
                           copies itself into Shepherd's extensions, writes the skill, starts it; prints the URL.
                           login (default): a launchd agent (macOS) or systemd user unit (Linux), from login on;
                           shepherd: no service, Shepherd runs it while it is open. Installing again updates it.
demo uninstall [--purge]   the service, the skill and the binary go; the data stays unless --purge
demo url                   http://127.0.0.1:<port>/?token=…
demo status | start | stop | logs [-f]
demo serve                 in the foreground, as the service runs it
demo skill [--install]
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
named after Shepherd's theme tokens: `--background --shell --raised --card --dialog --input --hover --selected --kbd --focus --scrollbar --scrollbar-active --border
--border-strong --text --text-muted --text-faint --accent --on-accent --danger --warning --success --on-color --scrim
--shadow --switch-thumb --switch-thumb-edge --switch-off --switch-on-edge --spinner-1..3 --font --mono --font-size`.
The defaults are Shepherd's default themes (Ayu Light and Dark, by `prefers-color-scheme`); setting them on `:root`
brings another.

## Security

The server listens on 127.0.0.1 only, answers only to loopback `Host`s (no DNS rebinding) and only with the token: the
URL's `?token=` becomes a `SameSite=Strict` cookie named after the extension, and the CLI sends it as a bearer.

## Start one

```sh
go run golang.org/x/tools/cmd/gonew@latest github.com/getshepherd/paddock/examples/demo github.com/you/weather
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
