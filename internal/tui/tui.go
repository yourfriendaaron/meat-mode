// Package tui runs the terminal viewer: a Bubble Tea program that draws
// view.Frame, feeds keys to view.Model.Key and carries out its effects with
// appclient and clipboard. Mouse reporting is never turned on, so the
// terminal's own selection always works.
package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/appclient"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tui/clipboard"
	"github.com/yourfriendaaron/meat-mode/internal/tui/view"
)

// Options is what Run needs.
type Options struct {
	// Client is built with an onSession that saves the session, so an
	// in-place login and the daily renewal both persist.
	Client    *appclient.Client
	Clipboard clipboard.Env
	// Here is where the viewer was started: it opens on that project's
	// lists alone. The zero Here shows every list.
	Here view.Here
}

// Run shows the viewer until the user quits or ctx ends.
//
// The OSC 52 sequence of a copy goes out through Bubble Tea's own output
// (tea.Raw), which writes it between frames from the goroutine that draws
// them; a write of its own to /dev/tty could land in the middle of a frame.
// So the program clears Options.Clipboard's TTY and clipboard.Copy does only
// the native part.
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := newProgram(ctx, o)
	defer p.stop()
	_, err := tea.NewProgram(p, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrInterrupted) || (errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil) {
		return nil
	}
	return err
}

const (
	// noticeFor is how long a notice the program sets stays up.
	noticeFor = 3 * time.Second
	// coalesce is how long a refetch waits for more asks to join it, such as
	// the change the hub sends right after the stream opens.
	coalesce = 50 * time.Millisecond
)

// program is the Bubble Tea model: the view's model plus the hub link.
// Only Update touches its fields; the event goroutine talks to it through
// events.
type program struct {
	ctx    context.Context
	client *appclient.Client
	clip   clipboard.Env // TTY cleared: the sequence goes through tea.Raw
	getenv func(string) string
	now    func() time.Time

	m     view.Model
	state *api.State
	epics *api.Epics

	want     fetchSet // asked for and not yet being fetched
	fetching bool
	// busy holds the items with an action under way, by key: 0 while it is
	// being sent, then the fetch round after which the screen shows it. A
	// second x before then would act on the old status, so it waits.
	busy      map[string]int
	round     int // fetches started
	fetchedTo int // the round of the fetch in flight
	scheduled bool

	events     chan tea.Msg // from the event goroutine; one listen is always waiting on it
	gen        int          // the event goroutine's generation; older ones' messages are dropped
	stopEvents context.CancelFunc
	authing    bool // a session check or the password prompt is under way
}

type fetchSet struct{ state, epics bool }

var both = fetchSet{state: true, epics: true}

func (s fetchSet) any() bool { return s.state || s.epics }

func newProgram(ctx context.Context, o Options) *program {
	clip := o.Clipboard
	clip.TTY = nil
	getenv := clip.Getenv
	if getenv == nil {
		getenv = os.Getenv
		clip.Getenv = getenv
	}
	return &program{
		ctx:    ctx,
		client: o.Client,
		clip:   clip,
		getenv: getenv,
		now:    time.Now,
		m:      view.New(o.Client.Hub()).SetHere(o.Here),
		events: make(chan tea.Msg, 16),
	}
}

// stop ends the event goroutine.
func (p *program) stop() {
	if p.stopEvents != nil {
		p.stopEvents()
	}
}

// Messages the program sends itself.
type (
	eventMsg struct {
		gen int
		ev  appclient.Event
	}
	eventsEndedMsg struct {
		gen int
		err error
	}
	fetchNowMsg struct{}
	fetchedMsg  struct {
		state *api.State
		epics *api.Epics
		err   error
	}
	copiedMsg struct {
		ref  string
		sent bool
		res  clipboard.Result
	}
	restartMsg struct{ gen int }
	actedMsg   struct {
		key    string
		action api.Action
		before []api.Action
		done   string
		err    error
	}
	sessionMsg struct {
		login, authed bool
		err           error
	}
	loginMsg  struct{ err error }
	clearMsg  struct{ seq int }
	minuteMsg struct{}
)

func (p *program) Init() tea.Cmd {
	p.startEvents()
	return tea.Batch(p.listen, p.ask(both), p.everyMinute())
}

func (p *program) View() tea.View {
	v := tea.NewView(strings.Join(view.Frame(p.m), "\n"))
	v.AltScreen = true
	return v
}

func (p *program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.m = p.m.Resize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return p, tea.Quit
		}
		var effects []view.Effect
		p.m, effects = p.m.Key(k)
		return p, p.run(effects)
	case tea.PasteMsg:
		p.m = p.m.Paste(msg.Content)
	case eventMsg:
		if msg.gen != p.gen {
			return p, p.listen
		}
		return p, tea.Batch(p.listen, p.onEvent(msg.ev))
	case eventsEndedMsg:
		if msg.gen != p.gen || !errors.Is(msg.err, appclient.ErrAuth) {
			return p, p.listen
		}
		return p, tea.Batch(p.listen, p.auth())
	case fetchNowMsg:
		p.scheduled = false
		return p, p.fetch()
	case fetchedMsg:
		return p, p.onFetched(msg)
	case copiedMsg:
		return p, p.onCopied(msg)
	case actedMsg:
		return p, p.onActed(msg)
	case sessionMsg:
		return p, p.onSession(msg)
	case loginMsg:
		return p, p.onLogin(msg)
	case clearMsg:
		p.m = p.m.ClearNotice(msg.seq)
	case restartMsg:
		if msg.gen == p.gen {
			p.startEvents()
		}
	case minuteMsg:
		p.m = p.m.SetNow(p.now())
		return p, p.everyMinute()
	}
	return p, nil
}

// run carries out the view's effects.
func (p *program) run(effects []view.Effect) tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range effects {
		switch e := e.(type) {
		case view.Copy:
			cmds = append(cmds, p.copy(e)...)
		case view.Act:
			key := e.Action.Key
			if _, ok := p.busy[key]; ok && key != "" {
				cmds = append(cmds, p.notice("Still sending the last change to this item", view.NoticeWarn))
				continue
			}
			if key != "" {
				if p.busy == nil {
					p.busy = map[string]int{}
				}
				p.busy[key] = 0
			}
			cmds = append(cmds, func() tea.Msg {
				var before []api.Action
				for _, b := range e.Before {
					a, err := p.client.Act(p.ctx, b)
					if err != nil {
						return actedMsg{key: key, done: e.Done, err: err}
					}
					before = append(before, a)
				}
				a, err := p.client.Act(p.ctx, e.Action)
				for _, n := range e.After {
					if err != nil {
						break
					}
					_, err = p.client.Act(p.ctx, n)
				}
				return actedMsg{key: key, action: a, before: before, done: e.Done, err: err}
			})
		case view.OpenURL:
			// A terminal may have no browser and a narrow pane cuts the
			// line, so the link goes to the clipboard as a copy does.
			cmds = append(cmds, p.copy(view.Copy{Text: e.URL, Ref: "the link " + e.URL})...)
		case view.Refresh:
			cmds = append(cmds, p.ask(both))
		case view.Login:
			pw := e.Password
			cmds = append(cmds, func() tea.Msg {
				_, err := p.client.Login(p.ctx, pw)
				return loginMsg{err: err}
			})
		case view.Quit:
			cmds = append(cmds, tea.Quit)
		}
	}
	return tea.Batch(cmds...)
}

// copy sends the OSC 52 sequence through Bubble Tea's output and runs the
// native copy in a command.
func (p *program) copy(c view.Copy) []tea.Cmd {
	var cmds []tea.Cmd
	sent := c.Text != "" && len(c.Text) <= clipboard.MaxOSC52
	if sent {
		cmds = append(cmds, tea.Raw(clipboard.Sequence(c.Text, p.getenv)))
	}
	return append(cmds, func() tea.Msg {
		return copiedMsg{ref: c.Ref, sent: sent, res: clipboard.Copy(p.ctx, c.Text, p.clip)}
	})
}

func (p *program) onCopied(msg copiedMsg) tea.Cmd {
	r := msg.res
	switch {
	case errors.Is(r.Err, clipboard.ErrEmpty):
		return p.notice("Nothing to copy", view.NoticeWarn)
	case r.Native:
		return p.notice("Copied "+msg.ref, view.NoticeInfo)
	case msg.sent || r.Sent:
		return p.notice("Sent "+msg.ref+" to the terminal's clipboard", view.NoticeInfo)
	case r.TooLong:
		return p.notice(msg.ref+" is too long to send to the terminal's clipboard", view.NoticeWarn)
	case r.Err != nil:
		return p.notice("Could not copy "+msg.ref+": "+r.Err.Error(), view.NoticeError)
	}
	return p.notice("Could not copy "+msg.ref, view.NoticeError)
}

func (p *program) onActed(msg actedMsg) tea.Cmd {
	if msg.err != nil {
		delete(p.busy, msg.key)
	} else if msg.key != "" {
		// Free once a state fetch that starts from now on has come back.
		p.busy[msg.key] = p.round + 1
	}
	if errors.Is(msg.err, appclient.ErrAuth) {
		return tea.Batch(p.notice("Not sent: the session ended", view.NoticeError), p.auth())
	}
	if msg.err != nil {
		return p.notice("Not sent: "+p.reason(msg.err), view.NoticeError)
	}
	p.m = p.m.Acted(msg.action, msg.before...)
	// The view does not say whether the row came from the epics, so both
	// are fetched again.
	return tea.Batch(p.notice(msg.done, view.NoticeInfo), p.ask(both))
}

// notice shows text and clears it after noticeFor.
func (p *program) notice(text string, kind view.NoticeKind) tea.Cmd {
	var seq int
	p.m, seq = p.m.Notice(text, kind)
	return tea.Tick(noticeFor, func(time.Time) tea.Msg { return clearMsg{seq: seq} })
}

func (p *program) everyMinute() tea.Cmd {
	return tea.Every(time.Minute, func(time.Time) tea.Msg { return minuteMsg{} })
}

// ask asks for a refetch; asks within coalesce of each other, or made while
// a fetch runs, share one fetch.
func (p *program) ask(s fetchSet) tea.Cmd {
	p.want.state = p.want.state || s.state
	p.want.epics = p.want.epics || s.epics
	if p.fetching || p.scheduled {
		return nil
	}
	p.scheduled = true
	return tea.Tick(coalesce, func(time.Time) tea.Msg { return fetchNowMsg{} })
}

func (p *program) fetch() tea.Cmd {
	if p.fetching || !p.want.any() {
		return nil
	}
	w := p.want
	p.want = fetchSet{}
	p.fetching = true
	if w.state {
		p.round++
		p.fetchedTo = p.round
	}
	return func() tea.Msg {
		var msg fetchedMsg
		if w.state {
			s, err := p.client.State(p.ctx)
			if err != nil {
				msg.err = err
			} else {
				msg.state = &s
			}
		}
		if w.epics && msg.err == nil {
			e, err := p.client.Epics(p.ctx)
			if err != nil {
				msg.err = err
			} else {
				msg.epics = &e
			}
		}
		return msg
	}
}

func (p *program) onFetched(msg fetchedMsg) tea.Cmd {
	p.fetching = false
	if msg.state != nil {
		for k, r := range p.busy {
			if r > 0 && r <= p.fetchedTo {
				delete(p.busy, k)
			}
		}
	}
	if msg.state != nil {
		p.state = msg.state
	}
	if msg.epics != nil {
		p.epics = msg.epics
	}
	if (msg.state != nil || msg.epics != nil) && p.state != nil {
		var e api.Epics
		if p.epics != nil {
			e = *p.epics
		}
		p.m = p.m.SetData(*p.state, e, p.now())
	}
	var cmds []tea.Cmd
	switch {
	case errors.Is(msg.err, appclient.ErrAuth):
		cmds = append(cmds, p.auth())
	case msg.err != nil && p.ctx.Err() == nil:
		cmds = append(cmds, p.notice("Could not load from the hub: "+p.reason(msg.err), view.NoticeError))
	}
	if p.want.any() {
		p.scheduled = true
		cmds = append(cmds, tea.Tick(coalesce, func(time.Time) tea.Msg { return fetchNowMsg{} }))
	}
	return tea.Batch(cmds...)
}

// startEvents starts a new event goroutine, ending the one before.
func (p *program) startEvents() {
	p.stop()
	p.gen++
	gen := p.gen
	ctx, cancel := context.WithCancel(p.ctx)
	p.stopEvents = cancel
	send := func(m tea.Msg) {
		select {
		case p.events <- m:
		case <-ctx.Done():
		}
	}
	go func() {
		err := p.client.Events(ctx, func(e appclient.Event) { send(eventMsg{gen: gen, ev: e}) })
		send(eventsEndedMsg{gen: gen, err: err})
	}()
}

// restartPause is how long the event stream waits before it starts again
// after a session check that did not end in a password prompt.
var restartPause = 5 * time.Second

// restartLater starts the event stream again after restartPause.
func (p *program) restartLater() tea.Cmd {
	gen := p.gen
	return tea.Tick(restartPause, func(time.Time) tea.Msg { return restartMsg{gen: gen} })
}

// listen waits for the next message from the event goroutine.
func (p *program) listen() tea.Msg {
	select {
	case m := <-p.events:
		return m
	case <-p.ctx.Done():
		return nil
	}
}

func (p *program) onEvent(e appclient.Event) tea.Cmd {
	switch e.Kind {
	case appclient.EventConnected:
		p.m = p.m.SetConn(view.ConnLive)
		return p.ask(both)
	case appclient.EventChange:
		return p.ask(fetchSet{state: true})
	case appclient.EventEpics:
		return p.ask(fetchSet{epics: true})
	case appclient.EventDown:
		p.m = p.m.SetConn(view.ConnDown)
		if e.Err != nil {
			return p.notice(p.downText(e.Err), view.NoticeWarn)
		}
	}
	// EventAuth: Events returns ErrAuth next, handled as eventsEndedMsg.
	return nil
}

// auth checks the session after an ErrAuth and asks for the password when
// it has ended.
func (p *program) auth() tea.Cmd {
	if p.authing {
		return nil
	}
	p.authing = true
	return func() tea.Msg {
		login, authed, err := p.client.CheckSession(p.ctx)
		return sessionMsg{login: login, authed: authed, err: err}
	}
}

func (p *program) onSession(msg sessionMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		// The hub is out of reach; the event stream starts again after a
		// pause, so a check that keeps failing cannot spin.
		p.authing = false
		return tea.Batch(p.notice(p.downText(msg.err), view.NoticeWarn), p.restartLater())
	case !msg.login:
		p.authing = false
		return p.notice("The hub has no password now: set one in the app, then restart the viewer", view.NoticeError)
	case msg.authed:
		// The session is good though the stream was refused: start it again
		// after a pause, in case something in front of the hub answers the
		// two routes differently.
		p.authing = false
		return tea.Batch(p.restartLater(), p.ask(both))
	}
	p.m = p.m.AskPassword("The session ended. Enter the hub's password.")
	return nil
}

func (p *program) onLogin(msg loginMsg) tea.Cmd {
	var retry *appclient.RetryError
	switch {
	case msg.err == nil:
		p.authing = false
		p.startEvents()
		return p.ask(both)
	case errors.Is(msg.err, appclient.WrongPassword):
		p.m = p.m.AskPassword("Wrong password.")
	case errors.As(msg.err, &retry):
		p.m = p.m.AskPassword(waitText(retry.After))
	case errors.Is(msg.err, appclient.ErrNoPassword):
		p.authing = false
		return p.notice("The hub has no password now: set one in the app, then restart the viewer", view.NoticeError)
	default:
		p.m = p.m.AskPassword(capitalize(p.reason(msg.err)) + ". Enter the hub's password to try again.")
	}
	return nil
}

// waitText is the password prompt's line after a 429.
func waitText(after int) string {
	if after <= 0 {
		return "Too many tries. Wait a moment, then enter the password again."
	}
	return fmt.Sprintf("Too many tries. Wait %s, then enter the password again.", time.Duration(after)*time.Second)
}

// reason says in plain words why a call to the hub failed.
func (p *program) reason(err error) string {
	var refused *netguard.RefusedError
	var herr *appclient.HTTPError
	switch {
	case errors.As(err, &refused):
		return fmt.Sprintf("%s is not on a private network (it resolves to %s); set %s if it is your own network",
			refused.Host, refused.IP, netguard.EnvTrusted)
	case errors.As(err, &herr) && herr.Status == http.StatusMisdirectedRequest:
		host := p.client.Hub()
		if u, perr := url.Parse(host); perr == nil {
			host = u.Host
		}
		return "the hub does not answer to the host name " + host + "; use the host name of its public URL"
	}
	return err.Error()
}

// downText is the notice for a dropped event stream.
func (p *program) downText(err error) string {
	var refused *netguard.RefusedError
	var herr *appclient.HTTPError
	if errors.As(err, &refused) || (errors.As(err, &herr) && herr.Status == http.StatusMisdirectedRequest) {
		return capitalize(p.reason(err))
	}
	return "Hub unreachable, retrying: " + err.Error()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
