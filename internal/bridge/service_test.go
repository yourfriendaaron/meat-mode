package bridge

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// recorder is a Runner that records commands instead of running them.
type recorder struct{ calls []string }

func (r *recorder) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return []byte("active\n"), nil
}

func TestSystemdUnit(t *testing.T) {
	home := "/srv/people/alex"
	unit, err := SystemdUnit("/srv/people/alex/.local/bin/meat-mode", "/srv/people/alex/.local/bin:/usr/bin:/bin", home)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\nExecStart=%h/.local/bin/meat-mode bridge run\n",
		"\nEnvironment=PATH=%h/.local/bin:/usr/bin:/bin\n",
		"\nRestart=always\n",
		"\nWantedBy=default.target\n",
		"\nAfter=network-online.target\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, home) {
		t.Errorf("unit names the home directory instead of %%h:\n%s", unit)
	}
	odd, _ := SystemdUnit("/opt/Meat Mode/100%/meat-mode", "/usr/bin", home)
	if !strings.Contains(odd, `ExecStart="/opt/Meat Mode/100%%/meat-mode" bridge run`) {
		t.Errorf("path with a space and %% not quoted and escaped:\n%s", odd)
	}
	// systemd expands $VAR in ExecStart; $$ is a literal $.
	dollar, _ := SystemdUnit("/opt/$HOME/${PATH}/meat-mode", "/opt/$x/bin", home)
	if !strings.Contains(dollar, "\nExecStart=/opt/$$HOME/$${PATH}/meat-mode bridge run\n") ||
		!strings.Contains(dollar, "\nEnvironment=PATH=/opt/$x/bin\n") {
		t.Errorf("$ not escaped in ExecStart only:\n%s", dollar)
	}
	for _, bad := range [][2]string{
		{"/opt/meat-mode\nExecStartPre=/bin/sh", "/usr/bin"},
		{"/opt/meat-mode", "/usr/bin\r\nExecStartPre=/bin/sh"},
	} {
		if u, err := SystemdUnit(bad[0], bad[1], home); err == nil {
			t.Errorf("unit for %q, %q was written:\n%s", bad[0], bad[1], u)
		}
		if p, err := LaunchdPlist(bad[0], bad[1], home); err == nil {
			t.Errorf("plist for %q, %q was written:\n%s", bad[0], bad[1], p)
		}
	}
	if err := (&Systemd{Dir: t.TempDir(), Home: home, Run: (&recorder{}).run}).Install("/opt/a\nb", "/usr/bin"); err == nil {
		t.Error("Install accepted a path with a newline")
	}
}

func TestSystemdInstall(t *testing.T) {
	home := t.TempDir()
	rec := &recorder{}
	svc, err := NewService("linux", home, rec.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Install(filepath.Join(home, "bin", "meat-mode"), "/usr/bin"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, ".config", "systemd", "user", "meat-mode-bridge.service")
	if svc.File() != file {
		t.Errorf("unit file %s", svc.File())
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ExecStart=%h/bin/meat-mode bridge run") {
		t.Errorf("unit:\n%s", b)
	}
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now meat-mode-bridge.service",
		"systemctl --user restart meat-mode-bridge.service",
	}
	if !slices.Equal(rec.calls, want) {
		t.Errorf("calls = %q", rec.calls)
	}
	if s := svc.Status(); !strings.Contains(s, "active") {
		t.Errorf("status %q", s)
	}
	rec.calls = nil
	if err := svc.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("unit file left behind")
	}
	if !slices.Equal(rec.calls, []string{"systemctl --user disable --now meat-mode-bridge.service", "systemctl --user daemon-reload"}) {
		t.Errorf("uninstall calls = %q", rec.calls)
	}
}

func TestLaunchdPlist(t *testing.T) {
	plist, err := LaunchdPlist("/srv/people/sam/bin/meat & mode", "/opt/homebrew/bin:/usr/bin", "/srv/people/sam")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Dict struct {
			Inner []string `xml:",any"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal([]byte(plist), &doc); err != nil {
		t.Fatalf("plist is not well-formed XML: %v\n%s", err, plist)
	}
	for _, want := range []string{
		"<string>" + LaunchdLabel + "</string>",
		"<string>/srv/people/sam/bin/meat &amp; mode</string>\n\t\t<string>bridge</string>\n\t\t<string>run</string>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<string>/opt/homebrew/bin:/usr/bin</string>",
		"<string>/srv/people/sam/Library/Logs/meat-mode-bridge.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
}

// launchctl is a fake launchctl: an agent that is loaded until booted out
// takes stillThere more prints to go, and bootstrap fails busy more times.
type launchctl struct {
	loaded           bool
	stillThere, busy int
	calls            []string
	sleeps           int
}

func (l *launchctl) run(name string, args ...string) ([]byte, error) {
	l.calls = append(l.calls, name+" "+strings.Join(args, " "))
	switch args[0] {
	case "bootout":
		if !l.loaded {
			return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3")
		}
		l.loaded = false
		return nil, nil
	case "print":
		if l.stillThere > 0 {
			l.stillThere--
			return nil, nil
		}
		return nil, errors.New("exit status 113")
	case "bootstrap":
		if l.busy > 0 {
			l.busy--
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		l.loaded = true
		return nil, nil
	}
	return nil, errors.New("unexpected")
}

func TestLaunchdInstall(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist")
	target := "gui/501/" + LaunchdLabel
	newSvc := func(l *launchctl) *Launchd {
		return &Launchd{Dir: filepath.Join(home, "Library", "LaunchAgents"), Home: home, UID: 501, Run: l.run,
			Sleep: func(time.Duration) { l.sleeps++ }}
	}

	// First install: nothing to boot out, so no wait.
	first := &launchctl{}
	if err := newSvc(first).Install("/usr/local/bin/meat-mode", "/usr/bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if want := []string{"launchctl bootout " + target, "launchctl bootstrap gui/501 " + file}; !slices.Equal(first.calls, want) {
		t.Errorf("first install calls = %q", first.calls)
	}

	// Replacing a running agent waits for it to go, and retries a busy
	// bootstrap, as launchd answers while the old one is still leaving.
	again := &launchctl{loaded: true, stillThere: 2, busy: 2}
	svc := newSvc(again)
	if err := svc.Install("/opt/meat-mode/2.0.0/meat-mode", "/usr/bin"); err != nil {
		t.Fatalf("%v; calls %q", err, again.calls)
	}
	want := []string{
		"launchctl bootout " + target,
		"launchctl print " + target, "launchctl print " + target, "launchctl print " + target,
		"launchctl bootstrap gui/501 " + file, "launchctl bootstrap gui/501 " + file, "launchctl bootstrap gui/501 " + file,
	}
	if !slices.Equal(again.calls, want) || !again.loaded || again.sleeps != 4 {
		t.Errorf("replace calls = %q, loaded %v, sleeps %d", again.calls, again.loaded, again.sleeps)
	}
	if got := svc.Executable(); got != "/opt/meat-mode/2.0.0/meat-mode" {
		t.Errorf("Executable = %q", got)
	}

	// A bootstrap that never succeeds gives up, saying how to load it.
	stuck := &launchctl{busy: 1000}
	err := newSvc(stuck).Install("/usr/local/bin/meat-mode", "/usr/bin")
	if err == nil || !strings.Contains(err.Error(), "Input/output error") || !strings.Contains(err.Error(), "launchctl bootstrap gui/501 "+file) {
		t.Errorf("stuck install = %v", err)
	}
	if n := strings.Count(strings.Join(stuck.calls, "\n"), "bootstrap"); n != launchdTries {
		t.Errorf("tried bootstrap %d times, want %d", n, launchdTries)
	}
}

// TestServiceExecutable checks that each service reads back the program it
// was written for, whatever the path holds.
func TestServiceExecutable(t *testing.T) {
	home := t.TempDir()
	for _, exe := range []string{
		filepath.Join(home, ".local", "bin", "meat-mode"),
		"/opt/Meat Mode/100%/meat-mode",
		"/opt/$HOME/a\\b\"c/meat-mode",
		"/srv/meat & <mode>/meat-mode",
	} {
		sd := &Systemd{Dir: t.TempDir(), Home: home, Run: (&recorder{}).run}
		if err := sd.Install(exe, "/usr/bin"); err != nil {
			t.Fatal(err)
		}
		if got := sd.Executable(); got != exe {
			t.Errorf("systemd Executable = %q, want %q", got, exe)
		}
		plist, err := LaunchdPlist(exe, "/usr/bin", home)
		if err != nil {
			t.Fatal(err)
		}
		if got := plistProgram([]byte(plist)); got != exe {
			t.Errorf("launchd program = %q, want %q", got, exe)
		}
	}
	none := &Systemd{Dir: t.TempDir(), Home: home}
	if got := none.Executable(); got != "" {
		t.Errorf("Executable with no unit = %q", got)
	}
	if got := (&Launchd{Dir: t.TempDir(), Home: home}).Executable(); got != "" {
		t.Errorf("Executable with no plist = %q", got)
	}
}

func TestServicePath(t *testing.T) {
	if got := ServicePath("/opt/tools/bin/bd"); got != "/opt/tools/bin:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin" {
		t.Errorf("ServicePath = %q", got)
	}
	if got := ServicePath("/usr/bin/bd"); got != "/usr/bin:/usr/local/bin:/opt/homebrew/bin:/bin" {
		t.Errorf("ServicePath = %q", got)
	}
}
