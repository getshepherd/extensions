# paddock

A local extension in one Go binary: a web page (for Shepherd's Web pages, or any browser), the HTTP API behind it,
a CLI over the same commands for agents, and a service that keeps it running after login. Standard library only.

```go
//go:embed web
var web embed.FS

func main() {
	app := paddock.New("notes", "Short notes kept on this machine", web)
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
- `notes add --text …` for agents and shells, printing JSON. It runs on the service, so an open page sees the change;
- a section of the skill `notes skill --install` writes to `~/.claude/skills/notes/SKILL.md`.

Fields of type string, bool, int, float64 and `[]string` become flags; `--json '{…}'` takes the rest.

## The binary

```
notes install [--port N]   launchd agent (macOS) or systemd user unit (Linux): runs now and at every login; prints the URL
notes url                  http://127.0.0.1:<port>/?token=…: add it to Shepherd's Web pages
notes status | start | stop | logs [-f] | uninstall
notes serve                in the foreground, as the service runs it
notes skill [--install]
notes <command> [flags]
```

Install from where the binary will stay: the service and the skill point at its path. Port and token live in
`<config dir>/paddock/<name>/state.json` (0600) and survive reinstalls, so the URL saved in Shepherd keeps working.
`App.Dir()` is that folder, for the extension's own data.

## The page

Put it in `web/` next to `main.go`. `/_paddock/paddock.css` dresses plain HTML elements (buttons, inputs, lists) in
both themes; `/_paddock/paddock.js` has `call` and `on`.

## Security

The server listens on 127.0.0.1 only, answers only to loopback `Host`s (no DNS rebinding) and only with the token: the
URL's `?token=` becomes a `SameSite=Strict` cookie named after the extension, and the CLI sends it as a bearer.

## Start one

Copy `examples/notes`, change the module import if you vendor it elsewhere, and `go build -o notes ./examples/notes`.

Not here yet: a Windows service (`serve` runs there in the foreground), starting on demand.
