package paddock

import (
	"encoding/xml"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The service is the system's own: a launchd agent on macOS, a systemd user unit on Linux. Either starts it at login,
// restarts it when it dies and keeps its output, and the CLI works whether Shepherd is open or not.
// ponytail: no Windows service; there `serve` runs in the foreground. Task Scheduler if it's needed.

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

func (a *App) install(args []string) error {
	fs := flag.NewFlagSet(a.Name+" install", flag.ContinueOnError)
	port := fs.Int("port", 0, "the port to listen on (default: the one picked before, else a free one)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := a.unitPath()
	if err != nil {
		return err
	}
	st, err := a.ensureState(*port)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
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
		err = run("launchctl", "bootstrap", a.domain(), path)
	} else {
		if err = run("systemctl", "--user", "daemon-reload"); err == nil {
			if err = run("systemctl", "--user", "enable", filepath.Base(path)); err == nil {
				err = run("systemctl", "--user", "restart", filepath.Base(path))
			}
		}
	}
	if err != nil {
		return err
	}
	fmt.Println(st.url())
	return nil
}

func (a *App) uninstall() error {
	path, err := a.unitPath()
	if err != nil {
		return err
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

func (a *App) service(verb string) error {
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
		return fmt.Errorf("not running (port %d)", st.Port)
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
	if runtime.GOOS == "linux" {
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
