package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"github.com/yourfriendaaron/meat-mode/internal/appclient"
	"github.com/yourfriendaaron/meat-mode/internal/bridge"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tui"
	"github.com/yourfriendaaron/meat-mode/internal/tui/clipboard"
	"github.com/yourfriendaaron/meat-mode/internal/tui/view"
)

// maxPasswordTries is how many times the first login asks for the password.
const maxPasswordTries = 3

// tuiEnv is the world `meat-mode tui` acts on, so tests need no terminal.
type tuiEnv struct {
	dir        string // the config directory: bridge.json and tui.json
	getenv     func(string) string
	readLine   func(prompt string) (string, error)
	readSecret func(prompt string) (string, error)
	// isTerminal reports whether stdin and stdout are terminals. The
	// viewer needs both, so it is checked before the first prompt.
	isTerminal func() bool
	out        io.Writer
	errOut     io.Writer
	run        func(context.Context, tui.Options) error
	// here is where the command runs, for the viewer to open on that
	// project; nil or a zero Here shows every list.
	here func() view.Here
}

// runTUI runs `meat-mode tui [--hub URL]` and `meat-mode tui logout [--hub URL]`.
//
// SIGINT and SIGTERM keep their default while the prompts and logout run,
// so Ctrl+C at "Password for ..." ends the command; only the viewer
// itself turns them into a clean quit.
func runTUI(args []string) error {
	ctx := context.Background()
	paths, err := bridge.DefaultPaths()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	env := tuiEnv{
		dir:    filepath.Dir(paths.Config),
		getenv: os.Getenv,
		readLine: func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			line, err := in.ReadString('\n')
			if err != nil && (!errors.Is(err, io.EOF) || line == "") {
				return "", err
			}
			return strings.TrimSpace(line), nil
		},
		readSecret: func(prompt string) (string, error) {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return "", errors.New("meat-mode tui needs a terminal to ask for the password")
			}
			fmt.Fprint(os.Stderr, prompt)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		},
		out:    os.Stdout,
		errOut: os.Stderr,
		isTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
		here: func() view.Here {
			dir, err := os.Getwd()
			if err != nil {
				return view.Here{}
			}
			home, _ := os.UserHomeDir()
			return view.Here{Home: home, Dir: dir}
		},
		run: func(ctx context.Context, o tui.Options) error {
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			return tui.Run(ctx, o)
		},
	}
	return tuiMain(ctx, args, env)
}

// tuiArgs is the parsed command line of `meat-mode tui`.
type tuiArgs struct {
	logout bool
	hub    string
	all    bool // start on every list, not the current project's
}

func parseTUIArgs(args []string) (tuiArgs, error) {
	var a tuiArgs
	name := "tui"
	if len(args) > 0 && args[0] == "logout" {
		a.logout, name, args = true, "tui logout", args[1:]
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.hub, "hub", "", "the hub's URL")
	usage := name + " [--hub URL]"
	if !a.logout {
		fs.BoolVar(&a.all, "all", false, "show every list, not just the current project's")
		usage = name + " [--hub URL] [--all]"
	}
	if err := fs.Parse(args); err != nil {
		return a, fmt.Errorf("%s: %w; usage: meat-mode %s", name, err, usage)
	}
	if fs.NArg() > 0 {
		return a, fmt.Errorf("%s: unexpected %q; usage: meat-mode %s", name, fs.Arg(0), usage)
	}
	return a, nil
}

func tuiMain(ctx context.Context, args []string, env tuiEnv) error {
	a, err := parseTUIArgs(args)
	if err != nil {
		return err
	}
	if a.logout {
		return tuiLogout(ctx, a.hub, env)
	}
	return tuiRun(ctx, a.hub, a.all, env)
}

// pairedHub is the hub this machine's bridge is paired with, as bridge.json
// has it, and the trusted networks saved with it. Both are empty when the
// machine is not paired.
func (e tuiEnv) pairedHub() (hub, trusted string) {
	b, err := os.ReadFile(filepath.Join(e.dir, "bridge.json"))
	if err != nil {
		return "", ""
	}
	var c bridge.Config
	if json.Unmarshal(b, &c) != nil {
		return "", ""
	}
	return c.HubURL, c.TrustedNetworks
}

// savedHubs lists the hubs tui.json holds a session for.
func (e tuiEnv) savedHubs() ([]string, error) { return appclient.SavedHubs(e.dir) }

// pickHub chooses the hub without asking: --hub, else the only hub with a
// saved session, else the paired hub when it has a session or none has
// one. It returns "" when none applies.
func (e tuiEnv) pickHub(flagHub string) (string, error) {
	if flagHub != "" {
		return appclient.NormalizeHub(flagHub)
	}
	saved, err := e.savedHubs()
	if err != nil {
		return "", err
	}
	if len(saved) == 1 {
		return saved[0], nil
	}
	if paired, _ := e.pairedHub(); paired != "" {
		if hub, err := appclient.NormalizeHub(paired); err == nil && (len(saved) == 0 || slices.Contains(saved, hub)) {
			return hub, nil
		}
	}
	return "", nil
}

// allowed is the set of addresses hub may have: the private set, the
// networks in MEAT_MODE_TRUSTED_NETWORKS and, when hub is the paired hub,
// the ones saved with the pairing.
func (e tuiEnv) allowed(hub string) (netguard.Set, error) {
	trusted := e.getenv(netguard.EnvTrusted)
	if paired, saved := e.pairedHub(); paired != "" && saved != "" {
		if p, err := appclient.NormalizeHub(paired); err == nil && p == hub {
			trusted += "," + saved
		}
	}
	return netguard.ParseTrusted(trusted)
}

// saver is the client's onSession: it saves each new session and keeps the
// last failure to report.
type saver struct {
	dir string
	mu  sync.Mutex
	err error
}

func (s *saver) save(sess appclient.Session) {
	err := appclient.SaveSession(s.dir, sess)
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *saver) failed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		return nil
	}
	return fmt.Errorf("could not save the session, so the next start asks for the password again: %w", s.err)
}

// tuiRun is `meat-mode tui`: it finds or makes a session, then runs the viewer.
// It refuses to start without a terminal on both ends, before it asks for
// anything, so piping it somewhere fails at once.
func tuiRun(ctx context.Context, flagHub string, all bool, env tuiEnv) error {
	if env.isTerminal == nil || !env.isTerminal() {
		return errors.New("meat-mode tui needs a terminal")
	}
	hub, err := env.pickHub(flagHub)
	if err != nil {
		return err
	}
	if hub == "" {
		if hub, err = env.askHub(); err != nil {
			return err
		}
	}
	sess, ok, err := appclient.LoadSession(env.dir, hub)
	if err != nil {
		return err
	}
	allowed, err := env.allowed(hub)
	if err != nil {
		return err
	}
	sv := &saver{dir: env.dir}
	var s *appclient.Session
	if ok {
		s = &sess
	}
	c, err := appclient.New(hub, allowed, s, sv.save)
	if err != nil {
		return err
	}
	if !ok {
		if err := env.login(ctx, c); err != nil {
			return err
		}
		if err := sv.failed(); err != nil {
			fmt.Fprintln(env.errOut, "meat-mode:", err)
		}
	}
	o := tui.Options{Client: c, Clipboard: clipboard.System()}
	if !all && env.here != nil {
		o.Here = env.here()
	}
	err = env.run(ctx, o)
	if serr := sv.failed(); serr != nil && err == nil {
		err = serr
	}
	return err
}

// askHub asks for the hub's URL, offering the paired hub.
func (e tuiEnv) askHub() (string, error) {
	def, _ := e.pairedHub()
	prompt := "Hub URL: "
	if def != "" {
		prompt = "Hub URL [" + def + "]: "
	}
	raw, err := e.readLine(prompt)
	if err != nil {
		return "", err
	}
	if raw == "" {
		raw = def
	}
	if raw == "" {
		return "", errors.New("no hub URL given; run meat-mode tui --hub URL")
	}
	hub, err := appclient.NormalizeHub(raw)
	if err != nil {
		// Not the error itself: it quotes what was typed, which may be a
		// password typed at the wrong prompt.
		return "", errors.New("that is not a hub URL such as https://hub.example.com; run meat-mode tui --hub URL")
	}
	return hub, nil
}

// login asks for the password and logs in, up to maxPasswordTries times.
func (e tuiEnv) login(ctx context.Context, c *appclient.Client) error {
	for try := 1; ; try++ {
		pw, err := e.readSecret("Password for " + c.Hub() + ": ")
		if err != nil {
			return err
		}
		_, err = c.Login(ctx, pw)
		var retry *appclient.RetryError
		switch {
		case err == nil:
			return nil
		case errors.Is(err, appclient.WrongPassword) && try < maxPasswordTries:
			fmt.Fprintln(e.errOut, "Wrong password.")
		case errors.Is(err, appclient.WrongPassword):
			return errors.New("wrong password")
		case errors.As(err, &retry) && retry.After > 0:
			return fmt.Errorf("the hub asks to wait %d s before the next try: %s", retry.After, retry.Msg)
		default:
			return err
		}
	}
}

// tuiLogout is `meat-mode tui logout`: it ends the session on the hub and
// removes it from tui.json.
func tuiLogout(ctx context.Context, flagHub string, env tuiEnv) error {
	hub, err := env.pickHub(flagHub)
	if err != nil {
		return err
	}
	if hub == "" {
		saved, err := env.savedHubs()
		if err != nil {
			return err
		}
		if len(saved) == 0 {
			fmt.Fprintln(env.out, "No saved session; nothing to log out of.")
			return nil
		}
		return fmt.Errorf("sessions are saved for %s; pick one with --hub", strings.Join(saved, ", "))
	}
	sess, ok, err := appclient.LoadSession(env.dir, hub)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(env.out, "No saved session for %s; nothing to log out of.\n", hub)
		return nil
	}
	allowed, err := env.allowed(hub)
	if err != nil {
		return err
	}
	var lerr error
	c, err := appclient.New(hub, allowed, &sess, nil)
	if err != nil {
		lerr = err
	} else {
		lerr = c.Logout(ctx)
	}
	if err := appclient.DeleteSession(env.dir, hub); err != nil {
		return err
	}
	switch {
	case lerr == nil, errors.Is(lerr, appclient.ErrAuth):
		fmt.Fprintf(env.out, "Logged out of %s and removed the saved session.\n", hub)
	default:
		fmt.Fprintf(env.out, "Removed the saved session for %s. The hub did not confirm the logout (%v);\n"+
			"the session there ends 14 days after its last use, or when the password changes.\n", hub, lerr)
	}
	return nil
}
