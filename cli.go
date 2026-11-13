package paddock

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// flags makes a command's flag set, bound to a fresh In.
func (c *command) flags(prog string) (*flag.FlagSet, reflect.Value, *string) {
	fs := flag.NewFlagSet(prog+" "+c.name, flag.ContinueOnError)
	in := reflect.New(c.in)
	if c.in.Kind() == reflect.Struct {
		bindFlags(fs, in.Elem())
	}
	raw := fs.String("json", "", "the whole input as JSON (its fields win over the flags)")
	return fs, in, raw
}

func bindFlags(fs *flag.FlagSet, v reflect.Value) {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		help := f.Tag.Get("help")
		switch p := v.Field(i).Addr().Interface().(type) {
		case *string:
			fs.StringVar(p, name, *p, help)
		case *bool:
			fs.BoolVar(p, name, *p, help)
		case *int:
			fs.IntVar(p, name, *p, help)
		case *float64:
			fs.Float64Var(p, name, *p, help)
		case *[]string:
			fs.Func(name, help+" (repeatable)", func(s string) error { *p = append(*p, s); return nil })
		}
	}
}

// cli runs a command from the shell: on the running service, so the open page sees what it changes.
func (a *App) cli(c *command, args []string) error {
	fs, in, raw := c.flags(a.Name)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "%s %s: %s\n", a.Name, c.name, c.help)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *raw != "" {
		if err := json.Unmarshal([]byte(*raw), in.Interface()); err != nil {
			return fmt.Errorf("--json: %w", err)
		}
	}
	body, err := json.Marshal(in.Interface())
	if err != nil {
		return err
	}
	st, err := a.loadState()
	if err != nil {
		return fmt.Errorf("not set up yet: run `%s install`", a.Name)
	}
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/api/%s", st.Port, c.name), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+st.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return a.notRunning(st)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return errors.New(strings.TrimSpace(string(out)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, bytes.TrimSpace(out), "", "  ") == nil {
		out = pretty.Bytes()
	}
	fmt.Println(string(out))
	return nil
}

// skill prints the SKILL.md that teaches agents this extension: Shepherd asks for it (`shepherd skill <name>`) and
// nothing is written to the user's skill folders, which another orchestrator may own. Empty when there is nothing to
// teach: no Skill text and no commands.
func (a *App) skill() error {
	exe := a.binPath()
	if _, err := os.Stat(exe); err != nil {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	fmt.Print(a.skillText(exe))
	return nil
}

// oldSkillMark is in every SKILL.md paddock used to write to ~/.claude/skills/<name>: what tells it from a skill the
// user wrote under the same name.
const oldSkillMark = "A local service with a page the user may have open"

// removeOldSkill takes away the ~/.claude/skills/<name> that paddock wrote before skills moved into the binary, and
// only that one.
func (a *App) removeOldSkill() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".claude", "skills", a.Name)
	if b, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err == nil && strings.Contains(string(b), oldSkillMark) {
		os.RemoveAll(dir)
	}
}

func (a *App) skillText(exe string) string {
	if a.guide == "" && len(a.cmds) == 0 {
		return ""
	}
	var b strings.Builder
	use := ""
	if len(a.cmds) > 0 {
		use = fmt.Sprintf(" Use the `%s` CLI to read and change it.", a.Name)
	}
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s.%s\n---\n\n", a.Name, a.Description, use)
	fmt.Fprintf(&b, "# %s\n\n%s.\n\n", a.Name, a.Description)
	if a.guide != "" {
		b.WriteString(strings.TrimSpace(a.guide) + "\n\n")
	}
	if len(a.cmds) == 0 {
		return b.String()
	}
	b.WriteString("A local service with a page the user may have open; every command below talks to it and prints JSON, and the page updates as you change things. If a command says it isn't running, follow what it says.\n\n## Commands\n")
	for _, c := range a.cmds {
		fmt.Fprintf(&b, "\n### `\"%s\" %s`\n\n%s\n\n", exe, c.name, c.help) // quoted: macOS's path has a space
		fs, _, _ := c.flags(a.Name)
		fs.VisitAll(func(f *flag.Flag) {
			typ, usage := flag.UnquoteUsage(f)
			fmt.Fprintf(&b, "- `--%s %s`: %s\n", f.Name, typ, usage)
		})
	}
	return b.String()
}
