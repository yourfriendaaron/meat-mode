// Package clipboard copies text for the terminal viewer so that it reaches
// the user's own clipboard: an OSC 52 sequence to the terminal every time,
// which passes through ssh and multiplexers, and the native clipboard as
// well on a session that is not over ssh. See the epic's design.
package clipboard

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// nativeTimeout bounds each native copy command; a variable so tests can
// shorten it.
var nativeTimeout = 2 * time.Second

// waitDelay is how long System's Run waits for a command's pipes to close
// after it exits.
const waitDelay = time.Second

// MaxOSC52 is the most text sent as OSC 52; terminals drop longer sequences
// without a word.
const MaxOSC52 = 64 << 10

// Env is the world Copy acts on, so tests need no terminal.
type Env struct {
	Getenv func(string) string
	GOOS   string
	// TTY opens the controlling terminal (/dev/tty) for writing; nil or an
	// error means no OSC 52.
	TTY func() (io.WriteCloser, error)
	// Run runs a native copy command with text on its stdin, under the
	// 2 s timeout and 1 s WaitDelay, treating exec.ErrWaitDelay as success.
	Run func(ctx context.Context, name string, args []string, text string) error
	// Exists reports whether a path exists (the Wayland socket check).
	Exists func(path string) bool
	// LookPath finds a command, as exec.LookPath.
	LookPath func(string) (string, error)
}

// System is Env for the real process.
func System() Env {
	return Env{
		Getenv: os.Getenv,
		GOOS:   runtime.GOOS,
		TTY: func() (io.WriteCloser, error) {
			return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		},
		Run: func(ctx context.Context, name string, args []string, text string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = strings.NewReader(text)
			cmd.WaitDelay = waitDelay
			err := cmd.Run()
			// xclip forks and keeps the pipes open; the copy has happened.
			if errors.Is(err, exec.ErrWaitDelay) {
				return nil
			}
			return err
		},
		Exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		LookPath: exec.LookPath,
	}
}

// Result is what Copy did.
type Result struct {
	Native  bool // a native clipboard command succeeded
	Sent    bool // the OSC 52 sequence was written to the terminal
	TooLong bool // text was over MaxOSC52, so no OSC 52 was sent
	// Err is ErrEmpty for empty text, otherwise set only when neither path
	// worked and something failed.
	Err error
}

// ErrEmpty is Copy's error for empty text: an empty OSC 52 clears the
// clipboard, so nothing is sent.
var ErrEmpty = errors.New("nothing to copy")

// Copy sends text by every path the environment allows. It bounds each
// native command with a 2 s timeout on ctx; System's Run adds a 1 s
// WaitDelay. The program calls it from a tea.Cmd, between frames; if the
// /dev/tty write could interleave with Bubble Tea's output, lane 4 uses
// Bubble Tea's raw output instead and reports it.
func Copy(ctx context.Context, text string, env Env) Result {
	if text == "" {
		return Result{Err: ErrEmpty}
	}
	var res Result
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	if len(text) > MaxOSC52 {
		res.TooLong = true
	} else if env.TTY != nil {
		if w, err := env.TTY(); err != nil {
			fail(err)
		} else {
			_, err := io.WriteString(w, Sequence(text, env.Getenv))
			if cerr := w.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				fail(err)
			} else {
				res.Sent = true
			}
		}
	}
	for _, c := range nativeCandidates(env) {
		path, err := env.LookPath(c.name)
		if err != nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, nativeTimeout)
		err = env.Run(cctx, path, c.args, text)
		cancel()
		if err == nil {
			res.Native = true
			break
		}
		fail(err)
	}
	if !res.Sent && !res.Native {
		res.Err = firstErr
	}
	return res
}

type candidate struct {
	name string
	args []string
}

// nativeCandidates lists the native copy commands to try, in order. There
// are none over ssh, where the machine's clipboard is not the user's.
func nativeCandidates(env Env) []candidate {
	if env.LookPath == nil || env.Run == nil {
		return nil
	}
	for _, k := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		if env.Getenv(k) != "" {
			return nil
		}
	}
	switch env.GOOS {
	case "darwin":
		return []candidate{{"pbcopy", nil}}
	case "linux":
		var cs []candidate
		if waylandLive(env) {
			cs = append(cs, candidate{"wl-copy", nil})
		}
		if env.Getenv("DISPLAY") != "" {
			cs = append(cs,
				candidate{"xclip", []string{"-selection", "clipboard"}},
				candidate{"xsel", []string{"--clipboard", "--input"}})
		}
		return cs
	}
	return nil
}

// waylandLive reports whether WAYLAND_DISPLAY names a socket that exists:
// an absolute path, or a name under XDG_RUNTIME_DIR.
func waylandLive(env Env) bool {
	w := env.Getenv("WAYLAND_DISPLAY")
	if w == "" || env.Exists == nil {
		return false
	}
	if !filepath.IsAbs(w) {
		dir := env.Getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			return false
		}
		w = filepath.Join(dir, w)
	}
	return env.Exists(w)
}

// Sequence is the OSC 52 sequence for text as Copy writes it: plain, or
// wrapped for tmux ($TMUX) or screen ($STY).
func Sequence(text string, getenv func(string) string) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	switch {
	case getenv("TMUX") != "":
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	case getenv("STY") != "":
		return "\x1bP" + seq + "\x1b\\"
	}
	return seq
}
