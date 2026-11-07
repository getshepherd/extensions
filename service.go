package paddock

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// An extension runs one of two ways, its state's start says which. startLogin: the system's own service, a launchd
// agent on macOS or a systemd user unit on Linux, which starts it at login, restarts it when it dies and keeps its
// output, so the CLI works whether Shepherd is open or not. startShepherd: no service; Shepherd starts `<binary> serve`
// when it opens and stops it when it quits.
// ponytail: no Windows service; there `serve` runs in the foreground, or Shepherd runs it. Task Scheduler if needed.

func (a *App) label() string { return "dev.paddock." + a.Name }

func (a *App) unitPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		return filepath.Join(home, "Library", "LaunchAgents", a.label()+".plist"), err
	case "linux":
		cfg, err := os.UserConfigDir()
		return filepath.Join(cfg, "systemd", "user", "paddock-"+a.Name+".service"), err
	}
	return "", fmt.Errorf("no service on %s: run `%s serve`", runtime.GOOS, a.Name)
}

func (a *App) logPath() string { return filepath.Join(a.Dir(), "log") }

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// install puts the extension where Shepherd finds it and sets it running: the binary copied into Dir (so `go run .
// install` works, and installing again is an update), its state, the skill, and the service when it runs from login.
func (a *App) install(args []string) error {
	fs := flag.NewFlagSet(a.Name+" install", flag.ContinueOnError)
	port := fs.Int("port", 0, "the port to listen on (default: the one picked before, else a free one)")
	start := fs.String("start", "", `when it runs: "login" (a service, from login on) or "shepherd" (while Shepherd runs) (default: as before, else login)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *start != "" && *start != startLogin && *start != startShepherd {
		return fmt.Errorf(`--start is "login" or "shepherd", not %q`, *start)
	}
	st, err := a.ensureState(*port)
	if err != nil {
		return err
	}
	if *start != "" {
		st.Start = *start
	}
	if st.Start == "" {
		st.Start = startLogin
	}
	exe, err := a.place()
	if err != nil {
		return err
	}
	if err := a.saveState(st); err != nil {
		return err
	}
	if st.Start == startLogin {
		err = a.installService(exe)
	} else {
		err = a.removeService()
	}
	if err != nil {
		return err
	}
	if _, err := a.writeSkill(exe); err != nil {
		return err
	}
	fmt.Println(st.url())
	return nil
}

// place copies the running binary to binPath, writing beside it and renaming over it, which replaces a binary that is
// running (copying onto it in place would leave macOS killing the old one's pages).
func (a *App) place() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return "", err
	}
	dst := a.binPath()
	if same, _ := filepath.EvalSymlinks(dst); same == exe {
		return dst, nil
	}
	src, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer src.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func (a *App) installService(exe string) error {
	path, err := a.unitPath()
	if err != nil {
		return err
	}
	// The service gets this shell's PATH: launchd's and systemd's own are too short for most tools an extension runs.
	var unit string
	if runtime.GOOS == "darwin" {
		unit = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, esc(a.label()), esc(exe), esc(os.Getenv("PATH")), esc(a.logPath()), esc(a.logPath()))
	} else {
		unit = fmt.Sprintf("[Unit]\nDescription=%s (paddock)\n\n[Service]\nExecStart=%q serve\nEnvironment=%q\nRestart=always\n\n[Install]\nWantedBy=default.target\n",
			a.Description, exe, "PATH="+os.Getenv("PATH"))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		quiet("launchctl", "bootout", a.domain()+"/"+a.label()) // an earlier install, with an older binary or port
		return run("launchctl", "bootstrap", a.domain(), path)
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "enable", filepath.Base(path)); err != nil {
		return err
	}
	return run("systemctl", "--user", "restart", filepath.Base(path))
}

// removeService stops the service and removes its unit; none is fine.
func (a *App) removeService() error {
	path, err := a.unitPath()
	if err != nil {
		return nil // no services here: nothing to remove
	}
	if runtime.GOOS == "darwin" {
		quiet("launchctl", "bootout", a.domain()+"/"+a.label())
	} else {
		quiet("systemctl", "--user", "disable", "--now", filepath.Base(path))
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "linux" {
		quiet("systemctl", "--user", "daemon-reload")
	}
	return nil
}

// uninstall takes the extension out of Shepherd and the system: the service, the skill and the binary. Its data and
// state stay in Dir, so installing again brings back the same notes on the same URL; --purge removes Dir too.
func (a *App) uninstall(args []string) error {
	fs := flag.NewFlagSet(a.Name+" uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also remove "+a.Dir()+": the data, the port and the token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.removeService(); err != nil {
		return err
	}
	if p, err := a.skillPath(); err == nil {
		os.RemoveAll(filepath.Dir(p))
	}
	if *purge {
		return os.RemoveAll(a.Dir())
	}
	if err := os.Remove(a.binPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) service(verb string) error {
	if st, err := a.loadState(); err == nil && st.Start == startShepherd {
		return fmt.Errorf("it runs while Shepherd is open: `%s install --start login` makes it a service, `%s serve` runs it here", a.Name, a.Name)
	}
	path, err := a.unitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("not installed: run `%s install`", a.Name)
	}
	if runtime.GOOS == "linux" {
		return run("systemctl", "--user", verb, filepath.Base(path))
	}
	if verb == "stop" {
		return run("launchctl", "bootout", a.domain()+"/"+a.label())
	}
	if quiet("launchctl", "bootstrap", a.domain(), path) != nil { // already loaded
		return run("launchctl", "kickstart", a.domain()+"/"+a.label())
	}
	return nil
}

func (a *App) domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// notRunning says how to get the server up, by how the extension runs.
func (a *App) notRunning(st state) error {
	if st.Start == startShepherd {
		return fmt.Errorf("not running: it runs while Shepherd is open (or `%s serve`)", a.Name)
	}
	return fmt.Errorf("the service isn't running: `%s start`", a.Name)
}

// status asks the server itself, so it holds for a `serve` in a terminal too; it fails when nothing answers.
func (a *App) status() error {
	st, err := a.loadState()
	if err != nil {
		return fmt.Errorf("not set up yet: run `%s install`", a.Name)
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/_paddock/ping", st.Port), nil)
	req.Header.Set("Authorization", "Bearer "+st.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return a.notRunning(st)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("port %d answers %s: another program has it", st.Port, resp.Status)
	}
	fmt.Println("running:", st.url())
	return nil
}

func (a *App) logs(args []string) error {
	follow := len(args) > 0 && args[0] == "-f"
	st, _ := a.loadState()
	if runtime.GOOS == "linux" && st.Start != startShepherd {
		cmd := []string{"--user", "-u", "paddock-" + a.Name, "-n", "100", "--no-pager"}
		if follow {
			cmd = append(cmd, "-f")
		}
		return run("journalctl", cmd...)
	}
	cmd := []string{"-n", "100"}
	if follow {
		cmd = append(cmd, "-f")
	}
	return run("tail", append(cmd, a.logPath())...)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func quiet(name string, args ...string) error { return exec.Command(name, args...).Run() }
