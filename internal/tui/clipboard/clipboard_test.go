package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSequenceFraming(t *testing.T) {
	text := "Water the plants\nnaïve ☕"
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	plain := "\x1b]52;c;" + b64 + "\a"
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"plain", nil, plain},
		{"tmux", map[string]string{"TMUX": "/tmp/x,1,0"}, "\x1bPtmux;\x1b\x1b]52;c;" + b64 + "\a\x1b\\"},
		{"screen", map[string]string{"STY": "1.pts"}, "\x1bP" + plain + "\x1b\\"},
		{"tmux wins over screen", map[string]string{"TMUX": "x", "STY": "y"}, "\x1bPtmux;\x1b\x1b]52;c;" + b64 + "\a\x1b\\"},
	}
	for _, c := range cases {
		if got := Sequence(text, envMap(c.env)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

type buf struct {
	bytes.Buffer
	closed bool
}

func (b *buf) Close() error { b.closed = true; return nil }

type call struct {
	name string
	args []string
	text string
}

// fake is an Env that records what Copy did.
type fake struct {
	env    map[string]string
	goos   string
	have   map[string]bool // commands LookPath finds
	exists map[string]bool
	fail   map[string]error // by command base name
	hang   map[string]bool
	tty    *buf
	ttyErr error
	calls  []call
}

func (f *fake) Env() Env {
	e := Env{
		Getenv: envMap(f.env),
		GOOS:   f.goos,
		Exists: func(p string) bool { return f.exists[p] },
		LookPath: func(n string) (string, error) {
			if f.have[n] {
				return "/bin/" + n, nil
			}
			return "", exec.ErrNotFound
		},
		Run: func(ctx context.Context, name string, args []string, text string) error {
			base := strings.TrimPrefix(name, "/bin/")
			f.calls = append(f.calls, call{base, args, text})
			if f.hang[base] {
				<-ctx.Done()
				return ctx.Err()
			}
			return f.fail[base]
		},
	}
	if f.tty != nil || f.ttyErr != nil {
		e.TTY = func() (io.WriteCloser, error) {
			if f.ttyErr != nil {
				return nil, f.ttyErr
			}
			return f.tty, nil
		}
	}
	return e
}

func (f *fake) names() string {
	var n []string
	for _, c := range f.calls {
		n = append(n, c.name)
	}
	return strings.Join(n, ",")
}

func TestCopyEmpty(t *testing.T) {
	f := &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, tty: &buf{}}
	r := Copy(context.Background(), "", f.Env())
	if !errors.Is(r.Err, ErrEmpty) || r.Sent || r.Native || f.tty.Len() != 0 || len(f.calls) != 0 {
		t.Fatalf("got %+v, tty %q, calls %v", r, f.tty.String(), f.calls)
	}
}

func TestCopyLimit(t *testing.T) {
	f := &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, tty: &buf{}}
	r := Copy(context.Background(), strings.Repeat("a", MaxOSC52), f.Env())
	if !r.Sent || r.TooLong || !r.Native || r.Err != nil || !f.tty.closed {
		t.Fatalf("at limit: %+v", r)
	}
	f = &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, tty: &buf{}}
	big := strings.Repeat("a", MaxOSC52+1)
	r = Copy(context.Background(), big, f.Env())
	if r.Sent || !r.TooLong || !r.Native || r.Err != nil || f.tty.Len() != 0 {
		t.Fatalf("over limit: %+v", r)
	}
	if len(f.calls) != 1 || f.calls[0].text != big {
		t.Fatalf("native should get the whole text: %v", f.calls)
	}
}

func TestCopyWritesSequence(t *testing.T) {
	f := &fake{goos: "linux", tty: &buf{}}
	r := Copy(context.Background(), "hi", f.Env())
	if !r.Sent || r.Native || r.Err != nil {
		t.Fatalf("%+v", r)
	}
	if want := Sequence("hi", f.Env().Getenv); f.tty.String() != want {
		t.Fatalf("got %q want %q", f.tty.String(), want)
	}
}

func TestNativeCandidates(t *testing.T) {
	all := map[string]bool{"pbcopy": true, "wl-copy": true, "xclip": true, "xsel": true}
	run := "/run/user/1000"
	cases := []struct {
		name   string
		f      fake
		want   string
		native bool
	}{
		{"darwin", fake{goos: "darwin", have: all}, "pbcopy", true},
		{"wayland live name", fake{goos: "linux", have: all, env: map[string]string{"WAYLAND_DISPLAY": "wayland-0", "XDG_RUNTIME_DIR": run, "DISPLAY": ":0"},
			exists: map[string]bool{run + "/wayland-0": true}}, "wl-copy", true},
		{"wayland live absolute", fake{goos: "linux", have: all, env: map[string]string{"WAYLAND_DISPLAY": "/tmp/sock"},
			exists: map[string]bool{"/tmp/sock": true}}, "wl-copy", true},
		{"wayland stale falls to xclip", fake{goos: "linux", have: all, env: map[string]string{"WAYLAND_DISPLAY": "wayland-0", "XDG_RUNTIME_DIR": run, "DISPLAY": ":0"}}, "xclip", true},
		{"wayland stale no display", fake{goos: "linux", have: all, env: map[string]string{"WAYLAND_DISPLAY": "wayland-0", "XDG_RUNTIME_DIR": run}}, "", false},
		{"x11 only", fake{goos: "linux", have: all, env: map[string]string{"DISPLAY": ":0"}}, "xclip", true},
		{"x11 xsel only", fake{goos: "linux", have: map[string]bool{"xsel": true}, env: map[string]string{"DISPLAY": ":0"}}, "xsel", true},
		{"wayland without wl-copy uses xclip", fake{goos: "linux", have: map[string]bool{"xclip": true}, env: map[string]string{"WAYLAND_DISPLAY": "/s", "DISPLAY": ":0"},
			exists: map[string]bool{"/s": true}}, "xclip", true},
		{"nothing", fake{goos: "linux", have: all}, "", false},
		{"nothing installed", fake{goos: "linux", env: map[string]string{"DISPLAY": ":0"}}, "", false},
		{"other os", fake{goos: "plan9", have: all, env: map[string]string{"DISPLAY": ":0"}}, "", false},
	}
	for _, c := range cases {
		f := c.f
		r := Copy(context.Background(), "x", f.Env())
		if f.names() != strings.TrimSuffix(c.want, "") && !(c.want == "" && len(f.calls) == 0) {
			t.Errorf("%s: ran %q want %q", c.name, f.names(), c.want)
		}
		if r.Native != c.native || r.Err != nil {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
}

func TestNativeArgs(t *testing.T) {
	f := &fake{goos: "linux", have: map[string]bool{"xclip": true}, env: map[string]string{"DISPLAY": ":0"}}
	Copy(context.Background(), "x", f.Env())
	if got := strings.Join(f.calls[0].args, " "); got != "-selection clipboard" {
		t.Fatalf("xclip args %q", got)
	}
	f = &fake{goos: "linux", have: map[string]bool{"xsel": true}, env: map[string]string{"DISPLAY": ":0"}}
	Copy(context.Background(), "x", f.Env())
	if got := strings.Join(f.calls[0].args, " "); got != "--clipboard --input" {
		t.Fatalf("xsel args %q", got)
	}
}

func TestSSHSkipsNative(t *testing.T) {
	for _, k := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		f := &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, env: map[string]string{k: "x"}, tty: &buf{}}
		r := Copy(context.Background(), "x", f.Env())
		if r.Native || !r.Sent || len(f.calls) != 0 {
			t.Errorf("%s: %+v calls %v", k, r, f.calls)
		}
	}
}

func TestFailureFallsThrough(t *testing.T) {
	f := &fake{goos: "linux", have: map[string]bool{"xclip": true, "xsel": true}, env: map[string]string{"DISPLAY": ":0"},
		fail: map[string]error{"xclip": errors.New("boom")}}
	r := Copy(context.Background(), "x", f.Env())
	if !r.Native || r.Err != nil || f.names() != "xclip,xsel" {
		t.Fatalf("%+v ran %s", r, f.names())
	}
}

func TestErrOnlyWhenNothingWorked(t *testing.T) {
	boom := errors.New("boom")
	f := &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, fail: map[string]error{"pbcopy": boom}}
	if r := Copy(context.Background(), "x", f.Env()); !errors.Is(r.Err, boom) || r.Native || r.Sent {
		t.Fatalf("native fails alone: %+v", r)
	}
	// OSC 52 sent, native failed: no error.
	f = &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, fail: map[string]error{"pbcopy": boom}, tty: &buf{}}
	if r := Copy(context.Background(), "x", f.Env()); r.Err != nil || !r.Sent {
		t.Fatalf("sent but native failed: %+v", r)
	}
}

func TestTTYOpenFails(t *testing.T) {
	boom := errors.New("no tty")
	f := &fake{goos: "darwin", have: map[string]bool{"pbcopy": true}, ttyErr: boom}
	r := Copy(context.Background(), "x", f.Env())
	if r.Sent || !r.Native || r.Err != nil {
		t.Fatalf("native saves it: %+v", r)
	}
	f = &fake{goos: "linux", ttyErr: boom}
	r = Copy(context.Background(), "x", f.Env())
	if r.Sent || r.Native || !errors.Is(r.Err, boom) {
		t.Fatalf("nothing worked: %+v", r)
	}
	// No TTY at all and no native path: nothing failed, so no error.
	f = &fake{goos: "linux"}
	if r := Copy(context.Background(), "x", f.Env()); r.Err != nil || r.Sent || r.Native {
		t.Fatalf("%+v", r)
	}
}

func TestHangingCommandTimesOut(t *testing.T) {
	old := nativeTimeout
	nativeTimeout = 50 * time.Millisecond
	defer func() { nativeTimeout = old }()
	f := &fake{goos: "linux", have: map[string]bool{"xclip": true, "xsel": true}, env: map[string]string{"DISPLAY": ":0"},
		hang: map[string]bool{"xclip": true}}
	start := time.Now()
	r := Copy(context.Background(), "x", f.Env())
	if time.Since(start) > 2*time.Second {
		t.Fatal("did not time out")
	}
	if !r.Native || f.names() != "xclip,xsel" {
		t.Fatalf("%+v ran %s", r, f.names())
	}
}

func TestSystemRun(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("needs sh")
	}
	run := System().Run
	ctx := context.Background()

	// Text reaches stdin.
	out := t.TempDir() + "/out"
	if err := run(ctx, "sh", []string{"-c", "cat > " + out}, "hello"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "hello" {
		t.Fatalf("stdin got %q", b)
	}

	// A background child that keeps stdin open while the text is too large
	// to write, as xclip can, counts as success once WaitDelay expires. The
	// pipe goes to fd 3 first: dash points a background job's stdin at
	// /dev/null before its own redirections, so "<&0" alone gives sleep
	// /dev/null there.
	start := time.Now()
	if err := run(ctx, "sh", []string{"-c", "exec 3<&0; sleep 5 <&3 & exit 0"}, strings.Repeat("x", 1<<20)); err != nil {
		t.Fatalf("ErrWaitDelay should be success: %v", err)
	}
	if d := time.Since(start); d < 500*time.Millisecond || d > 4*time.Second {
		t.Fatalf("returned after %v, want about the 1 s WaitDelay", d)
	}

	// A failing command is an error.
	if err := run(ctx, "sh", []string{"-c", "exit 3"}, "x"); err == nil {
		t.Fatal("exit 3 should fail")
	}
}

// Inside tmux the text also goes through tmux load-buffer -w, which reaches
// the outer terminal under tmux's defaults (set-clipboard external,
// allow-passthrough off), over ssh too.
func TestCopyTmuxLoadBuffer(t *testing.T) {
	f := &fake{goos: "linux", env: map[string]string{"TMUX": "/tmp/x,1,0", "SSH_TTY": "/dev/pts/1"}, have: map[string]bool{"tmux": true}}
	r := Copy(context.Background(), "mm-12 Water the plants", f.Env())
	if !r.Sent || r.Native || r.Err != nil {
		t.Fatalf("result = %+v, want Sent only", r)
	}
	if len(f.calls) != 1 || f.calls[0].name != "tmux" || strings.Join(f.calls[0].args, " ") != "load-buffer -w -" || f.calls[0].text != "mm-12 Water the plants" {
		t.Fatalf("calls = %+v", f.calls)
	}

	f = &fake{goos: "linux", env: map[string]string{"TMUX": "x"}, have: map[string]bool{"tmux": true}, fail: map[string]error{"tmux": errors.New("no server")}}
	if r := Copy(context.Background(), "a", f.Env()); r.Sent || r.Err == nil {
		t.Fatalf("failing tmux: result = %+v, want an error", r)
	}

	f = &fake{goos: "linux", env: map[string]string{}, have: map[string]bool{"tmux": true}}
	Copy(context.Background(), "a", f.Env())
	if f.names() != "" {
		t.Fatalf("outside tmux ran %s", f.names())
	}
}
