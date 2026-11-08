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
	"time"
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
	// Executable is the program the service file runs, or "" when there is
	// no service file or it cannot be read.
	Executable() string
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

// ServicePath returns the PATH a service runs with: the directories of the
// tools given (the trackers' tools and the agent command) first, then the
// system directories.
func ServicePath(tools ...string) string {
	dirs := []string{}
	for _, t := range tools {
		if t != "" && filepath.IsAbs(t) && !slices.Contains(dirs, filepath.Dir(t)) {
			dirs = append(dirs, filepath.Dir(t))
		}
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

// checkServicePaths refuses a path that would end a line of the service
// file and start another.
func checkServicePaths(paths ...string) error {
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\r") {
			return fmt.Errorf("cannot write a service file for %q: the path has a line break", p)
		}
	}
	return nil
}

// SystemdUnit returns the unit text. Paths under home use the %h specifier;
// $ in the command line is doubled so systemd does not expand it.
func SystemdUnit(exe, pathEnv, home string) (string, error) {
	if err := checkServicePaths(exe, pathEnv, home); err != nil {
		return "", err
	}
	return `[Unit]
Description=Meat Mode bridge
Documentation=https://github.com/yourfriendaaron/meat-mode
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + strings.ReplaceAll(systemdQuote(homeSpec(exe, home)), "$", "$$") + ` bridge run
Environment=` + systemdQuote("PATH="+homeSpec(pathEnv, home)) + `
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, nil
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
	unit, err := SystemdUnit(exe, pathEnv, s.Home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.File(), []byte(unit), 0o644); err != nil {
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

// Executable implements Service: the program of the unit's ExecStart, with
// SystemdUnit's quoting, $$ and %h undone.
func (s *Systemd) Executable() string {
	b, err := os.ReadFile(s.File())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		v, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		v, ok = strings.CutSuffix(strings.TrimSpace(v), " bridge run")
		if !ok {
			return ""
		}
		v = strings.ReplaceAll(v, "$$", "$")
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(v[1 : len(v)-1])
		}
		return unHomeSpec(v, s.Home)
	}
	return ""
}

// unHomeSpec undoes homeSpec for one path: %h is the home folder, %% is %.
func unHomeSpec(s, home string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+1 < len(s) {
			switch s[i+1] {
			case '%':
				b.WriteByte('%')
				i++
				continue
			case 'h':
				b.WriteString(strings.TrimRight(home, "/"))
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
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
	Dir   string // ~/Library/LaunchAgents
	Home  string
	UID   int
	Run   Runner
	Sleep func(time.Duration) // nil: time.Sleep
}

// launchdTries is how many times Install checks that the old agent is gone,
// and then tries to load the new one, LaunchdPause apart.
const launchdTries = 20

// LaunchdPause is the wait between Install's checks.
const LaunchdPause = 250 * time.Millisecond

func (l *Launchd) sleep(d time.Duration) {
	if l.Sleep != nil {
		l.Sleep(d)
		return
	}
	time.Sleep(d)
}

// File implements Service.
func (l *Launchd) File() string { return filepath.Join(l.Dir, LaunchdLabel+".plist") }

// LaunchdPlist returns the agent's property list. launchd does not expand
// ~, so paths are absolute.
func LaunchdPlist(exe, pathEnv, home string) (string, error) {
	if err := checkServicePaths(exe, pathEnv, home); err != nil {
		return "", err
	}
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
`, nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (l *Launchd) domain() string { return "gui/" + strconv.Itoa(l.UID) }

// Install implements Service.
func (l *Launchd) Install(exe, pathEnv string) error {
	plist, err := LaunchdPlist(exe, pathEnv, l.Home)
	if err != nil {
		return err
	}
	var out []byte
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(l.Home, "Library", "Logs"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(l.File(), []byte(plist), 0o644); err != nil {
		return err
	}
	// Replace a running copy. bootout returns before the agent has gone,
	// and a bootstrap while it is still there fails with an I/O error, so
	// wait for it to go, then retry the bootstrap a few seconds whatever
	// the error, as launchd's error text is no contract to match on.
	target := l.domain() + "/" + LaunchdLabel
	if _, err := l.Run("launchctl", "bootout", target); err == nil {
		for range launchdTries {
			if _, err := l.Run("launchctl", "print", target); err != nil {
				break
			}
			l.sleep(LaunchdPause)
		}
	}
	for i := range launchdTries {
		if i > 0 {
			l.sleep(LaunchdPause)
		}
		if out, err = l.Run("launchctl", "bootstrap", l.domain(), l.File()); err == nil {
			return nil
		}
	}
	return fmt.Errorf("launchctl bootstrap: %v: %s; load it with `launchctl bootstrap %s %s`",
		err, bytes.TrimSpace(out), l.domain(), l.File())
}

// Uninstall implements Service.
func (l *Launchd) Uninstall() error {
	_, _ = l.Run("launchctl", "bootout", l.domain()+"/"+LaunchdLabel)
	if err := os.Remove(l.File()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Executable implements Service: the first of the plist's ProgramArguments.
func (l *Launchd) Executable() string {
	b, err := os.ReadFile(l.File())
	if err != nil {
		return ""
	}
	return plistProgram(b)
}

// plistProgram reads the first string of ProgramArguments from a property
// list's top dictionary.
func plistProgram(b []byte) string {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	depth, key, inArgs := 0, "", false
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case depth == 3 && t.Name == xml.Name{Local: "key"}:
				var k string
				if d.DecodeElement(&k, &t) != nil {
					return ""
				}
				depth--
				key = k
			case depth == 3 && t.Name == xml.Name{Local: "array"}:
				inArgs = key == "ProgramArguments"
			case depth == 4 && inArgs && t.Name == xml.Name{Local: "string"}:
				var v string
				if d.DecodeElement(&v, &t) != nil {
					return ""
				}
				return v
			}
		case xml.EndElement:
			if depth == 3 {
				inArgs = false
			}
			depth--
		}
	}
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
