package bridge

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Service names.
const (
	SystemdUnitName = "meat-mode-bridge.service"
	LaunchdLabel    = "com.github.yourfriendaaron.meat-mode.bridge"
)

// Runner runs a service manager command and returns its combined output.
type Runner func(name string, args ...string) ([]byte, error)

// ExecRunner runs commands for real.
func ExecRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Service installs the bridge as a user service.
type Service interface {
	// Install writes the service file for exe and starts it.
	Install(exe, pathEnv string) error
	// Uninstall stops the service and removes its file.
	Uninstall() error
	// Status describes the service in one line.
	Status() string
	// File is where the service file lives.
	File() string
}

// NewService returns the service manager for goos: systemd on Linux, launchd
// on macOS.
func NewService(goos, home string, run Runner) (Service, error) {
	switch goos {
	case "linux":
		return &Systemd{Dir: filepath.Join(home, ".config", "systemd", "user"), Home: home, Run: run}, nil
	case "darwin":
		return &Launchd{Dir: filepath.Join(home, "Library", "LaunchAgents"), Home: home, UID: os.Getuid(), Run: run}, nil
	}
	return nil, fmt.Errorf("installing a service is not supported on %s; run `meat-mode bridge run` from your own service manager", goos)
}

// ServicePath returns the PATH a service runs with: the directory of bd
// first, then the system directories.
func ServicePath(bd string) string {
	dirs := []string{}
	if bd != "" && filepath.IsAbs(bd) {
		dirs = append(dirs, filepath.Dir(bd))
	}
	for _, d := range []string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"} {
		if !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return strings.Join(dirs, ":")
}

// Systemd manages a systemd user unit.
type Systemd struct {
	Dir  string // ~/.config/systemd/user
	Home string
	Run  Runner
}

// File implements Service.
func (s *Systemd) File() string { return filepath.Join(s.Dir, SystemdUnitName) }

// SystemdUnit returns the unit text. Paths under home use the %h specifier.
func SystemdUnit(exe, pathEnv, home string) string {
	return `[Unit]
Description=Meat Mode bridge
Documentation=https://github.com/yourfriendaaron/meat-mode
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + systemdQuote(homeSpec(exe, home)) + ` bridge run
Environment=` + systemdQuote("PATH="+homeSpec(pathEnv, home)) + `
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`
}

// homeSpec escapes % and swaps the home prefix of each path for %h.
func homeSpec(s, home string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if home == "" {
		return s
	}
	h := strings.ReplaceAll(strings.TrimRight(home, "/"), "%", "%%")
	parts := strings.Split(s, ":")
	for i, p := range parts {
		if p == h || strings.HasPrefix(p, h+"/") {
			parts[i] = "%h" + p[len(h):]
		}
	}
	return strings.Join(parts, ":")
}

func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Install implements Service.
func (s *Systemd) Install(exe, pathEnv string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.File(), []byte(SystemdUnit(exe, pathEnv, s.Home)), 0o644); err != nil {
		return err
	}
	if err := s.ctl("daemon-reload"); err != nil {
		return err
	}
	if err := s.ctl("enable", "--now", SystemdUnitName); err != nil {
		return err
	}
	return s.ctl("restart", SystemdUnitName) // pick up a new binary or config
}

func (s *Systemd) ctl(args ...string) error {
	out, err := s.Run("systemctl", append([]string{"--user"}, args...)...)
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

// Uninstall implements Service.
func (s *Systemd) Uninstall() error {
	_ = s.ctl("disable", "--now", SystemdUnitName)
	if err := os.Remove(s.File()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.ctl("daemon-reload")
}

// Status implements Service.
func (s *Systemd) Status() string {
	if _, err := os.Stat(s.File()); err != nil {
		return "not installed"
	}
	out, _ := s.Run("systemctl", "--user", "is-active", SystemdUnitName)
	return "systemd user unit " + SystemdUnitName + ": " + strings.TrimSpace(string(out))
}

// Launchd manages a launchd user agent.
type Launchd struct {
	Dir  string // ~/Library/LaunchAgents
	Home string
	UID  int
	Run  Runner
}

// File implements Service.
func (l *Launchd) File() string { return filepath.Join(l.Dir, LaunchdLabel+".plist") }

// LaunchdPlist returns the agent's property list. launchd does not expand
// ~, so paths are absolute.
func LaunchdPlist(exe, pathEnv, home string) string {
	log := filepath.Join(home, "Library", "Logs", "meat-mode-bridge.log")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + LaunchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
		<string>bridge</string>
		<string>run</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + xmlEscape(pathEnv) + `</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(log) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(log) + `</string>
</dict>
</plist>
`
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (l *Launchd) domain() string { return "gui/" + strconv.Itoa(l.UID) }

// Install implements Service.
func (l *Launchd) Install(exe, pathEnv string) error {
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(l.Home, "Library", "Logs"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(l.File(), []byte(LaunchdPlist(exe, pathEnv, l.Home)), 0o644); err != nil {
		return err
	}
	_, _ = l.Run("launchctl", "bootout", l.domain()+"/"+LaunchdLabel) // replace a running copy
	if out, err := l.Run("launchctl", "bootstrap", l.domain(), l.File()); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Uninstall implements Service.
func (l *Launchd) Uninstall() error {
	_, _ = l.Run("launchctl", "bootout", l.domain()+"/"+LaunchdLabel)
	if err := os.Remove(l.File()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status implements Service.
func (l *Launchd) Status() string {
	if _, err := os.Stat(l.File()); err != nil {
		return "not installed"
	}
	if _, err := l.Run("launchctl", "print", l.domain()+"/"+LaunchdLabel); err != nil {
		return "launchd agent " + LaunchdLabel + ": installed, not loaded"
	}
	return "launchd agent " + LaunchdLabel + ": loaded"
}
