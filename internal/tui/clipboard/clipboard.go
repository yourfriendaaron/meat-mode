// Package clipboard copies text for the terminal viewer so that it reaches
// the user's own clipboard: an OSC 52 sequence to the terminal every time,
// which passes through ssh and multiplexers, and the native clipboard as
// well on a session that is not over ssh. See the epic's design.
package clipboard

import (
	"context"
	"errors"
	"io"
)

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
func System() Env { return Env{} }

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
func Copy(ctx context.Context, text string, env Env) Result { return Result{Err: errNotBuilt} }

// Sequence is the OSC 52 sequence for text as Copy writes it: plain, or
// wrapped for tmux ($TMUX) or screen ($STY).
func Sequence(text string, getenv func(string) string) string { return "" }

var errNotBuilt = errors.New("clipboard: not built yet")
