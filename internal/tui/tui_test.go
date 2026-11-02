package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/yourfriendaaron/meat-mode/internal/appclient"
	"github.com/yourfriendaaron/meat-mode/internal/hub/hubtest"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tui/clipboard"
	"github.com/yourfriendaaron/meat-mode/internal/tui/view"
)

// harness runs the program's commands the way Bubble Tea does, each in its
// own goroutine with the message going back to Update, so the test sees
// the same frames a terminal would.
type harness struct {
	t    *testing.T
	p    *program
	msgs chan tea.Msg
	done chan struct{}
	raw  []string
	quit bool
}

func newHarness(t *testing.T, o Options) *harness {
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, p: newProgram(ctx, o), msgs: make(chan tea.Msg, 64), done: make(chan struct{})}
	t.Cleanup(func() {
		cancel()
		h.p.stop()
		close(h.done)
	})
	h.handle(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.exec(h.p.Init())
	return h
}

func (h *harness) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		select {
		case h.msgs <- msg:
		case <-h.done:
		}
	}()
}

func (h *harness) handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			h.exec(c)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		if r, ok := msg.(tea.RawMsg); ok {
			h.raw = append(h.raw, r.Msg.(string))
		}
		_, cmd := h.p.Update(msg)
		h.exec(cmd)
	}
}

// frame is the screen as plain text.
func (h *harness) frame() string {
	return ansi.Strip(h.p.View().Content)
}

// until handles messages until ok holds for the frame.
func (h *harness) until(what string, ok func(string) bool) {
	h.t.Helper()
	deadline := time.After(10 * time.Second)
	for !ok(h.frame()) {
		select {
		case msg := <-h.msgs:
			h.handle(msg)
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s; the frame is:\n%s", what, h.frame())
		}
	}
}

func (h *harness) shows(text string) {
	h.t.Helper()
	h.until("“"+text+"”", func(f string) bool { return strings.Contains(f, text) })
}

func (h *harness) keys(ks ...tea.KeyPressMsg) {
	for _, k := range ks {
		h.handle(k)
	}
}

func typed(s string) []tea.KeyPressMsg {
	var ks []tea.KeyPressMsg
	for _, r := range s {
		if r == ' ' {
			ks = append(ks, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		ks = append(ks, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return ks
}

var enter = tea.KeyPressMsg{Code: tea.KeyEnter}

// sessions records the sessions the client hands to onSession.
type sessions struct {
	mu  sync.Mutex
	all []appclient.Session
}

func (s *sessions) add(x appclient.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.all = append(s.all, x)
}

func (s *sessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.all)
}

// noNative is a clipboard world with no native copy command, as over ssh.
func noNative() clipboard.Env {
	return clipboard.Env{
		Getenv:   func(string) string { return "" },
		GOOS:     "linux",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Run:      func(context.Context, string, []string, string) error { return errors.New("not run") },
		Exists:   func(string) bool { return false },
	}
}

func TestProgram(t *testing.T) {
	hub := hubtest.New(t, hubtest.Options{})
	hub.Seed(t)
	dir := t.TempDir()

	// A saved session, as `meat-mode tui` leaves it.
	first, err := appclient.New(hub.URL, netguard.Set{}, nil, func(s appclient.Session) { _ = appclient.SaveSession(dir, s) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Login(context.Background(), hubtest.Password); err != nil {
		t.Fatal(err)
	}
	saved, ok, err := appclient.LoadSession(dir, hub.URL)
	if err != nil || !ok {
		t.Fatalf("LoadSession = %v, %v", ok, err)
	}

	var got sessions
	c, err := appclient.New(hub.URL, netguard.Set{}, &saved, got.add)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{Client: c, Clipboard: noNative()})

	// The to-dos.
	h.shows(hubtest.PersonalTitle)
	// The agents' task for the person waits in All; it has no date, so the
	// Today groups leave it out, as the app does.
	h.keys(typed("3")...)
	h.shows(hubtest.HumanTitle)
	h.keys(typed("1")...)
	h.shows("Any time")

	// y: the OSC 52 sequence goes through Bubble Tea's output.
	h.keys(typed("y")...)
	h.shows("to the terminal's clipboard")
	h.until("the raw sequence", func(string) bool { return len(h.raw) > 0 })
	if len(h.raw) != 1 || !strings.HasPrefix(h.raw[0], "\x1b]52;c;") {
		t.Errorf("raw output %q, want one OSC 52 sequence", h.raw)
	}

	// x on the personal to-do: a done, then it shows under Done today.
	h.keys(typed("/Feed")...)
	h.keys(enter)
	h.keys(typed("x")...)
	h.shows("Done: " + hubtest.PersonalTitle)
	h.keys(tea.KeyPressMsg{Code: tea.KeyEscape}) // clear the filter
	h.until("the to-do under Done today", func(f string) bool {
		i, j := strings.Index(f, "Done today"), strings.Index(f, hubtest.PersonalTitle)
		return i >= 0 && j > i
	})

	// The hub ends the session: the program asks for the password in place.
	hub.EndSession(t, saved.Cookie)
	hub.DropStreams()
	h.shows("The session ended. Enter the hub's password.")

	h.keys(typed("feed the cat")...)
	h.keys(enter)
	h.shows("Wrong password.")

	h.keys(typed(hubtest.Password)...)
	h.keys(enter)
	h.until("the view back with a new session", func(f string) bool {
		return got.count() > 0 && !strings.Contains(f, "Password") && strings.Contains(f, "Done today")
	})

	// The view is live again: a change on the hub shows.
	hub.Touch(t, "Take out the bins")
	h.shows("Take out the bins")

	// q quits.
	h.keys(typed("q")...)
	if !h.quit {
		h.until("quit", func(string) bool { return h.quit })
	}
}

func TestCtrlCQuitsFromThePasswordPrompt(t *testing.T) {
	hub := hubtest.New(t, hubtest.Options{})
	c, err := appclient.New(hub.URL, netguard.Set{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{Client: c, Clipboard: noNative()})
	h.shows("The session ended")
	h.keys(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	h.until("quit", func(string) bool { return h.quit })
}

func TestCopyNotices(t *testing.T) {
	p := &program{m: view.New("https://meat-mode.example.com")}
	for _, c := range []struct {
		msg  copiedMsg
		want string
	}{
		{copiedMsg{ref: "mm-1", sent: true, res: clipboard.Result{Native: true}}, "Copied mm-1"},
		{copiedMsg{ref: "mm-1", sent: true}, "Sent mm-1 to the terminal's clipboard"},
		{copiedMsg{ref: "mm-1", res: clipboard.Result{TooLong: true}}, "mm-1 is too long to send to the terminal's clipboard"},
		{copiedMsg{ref: "mm-1", res: clipboard.Result{TooLong: true, Native: true}}, "Copied mm-1"},
		{copiedMsg{ref: "mm-1", res: clipboard.Result{Err: errors.New("no display")}}, "Could not copy mm-1: no display"},
	} {
		p.m = p.m.Resize(100, 20)
		p.onCopied(c.msg)
		if f := ansi.Strip(strings.Join(view.Frame(p.m), "\n")); !strings.Contains(f, c.want) {
			t.Errorf("%+v: the frame lacks %q:\n%s", c.msg, c.want, f)
		}
	}
}

func TestReason(t *testing.T) {
	c, err := appclient.New("https://meat-mode.example.com", netguard.Set{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &program{client: c}
	refused := &netguard.RefusedError{Host: "meat-mode.example.com"}
	if got := p.downText(refused); !strings.Contains(got, "not on a private network") || !strings.Contains(got, netguard.EnvTrusted) {
		t.Errorf("refused: %q", got)
	}
	if got := p.downText(&appclient.HTTPError{Status: 421, Msg: "misdirected"}); !strings.Contains(got, "does not answer to the host name meat-mode.example.com") {
		t.Errorf("421: %q", got)
	}
	if got := p.downText(errors.New("connection refused")); got != "Hub unreachable, retrying: connection refused" {
		t.Errorf("other: %q", got)
	}
}
