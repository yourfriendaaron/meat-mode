package bridge

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Env is what the bridge commands touch outside the process. Tests replace
// the paths, the service manager and the executable.
type Env struct {
	Paths      Paths
	Service    Service                      // nil: the platform's user service manager
	Executable string                       // "": this process's executable
	LookPath   func(string) (string, error) // nil: exec.LookPath
	Getenv     func(string) string          // nil: os.Getenv
	HTTP       *http.Client
	Resolver   Resolver               // nil: the system resolver
	Getwd      func() (string, error) // nil: os.Getwd
	Stdin      io.Reader              // nil: no input
	Stdout     io.Writer
	Stderr     io.Writer
	Version    string
}

// DefaultEnv returns the real environment.
func DefaultEnv(version string) (Env, error) {
	p, err := DefaultPaths()
	if err != nil {
		return Env{}, err
	}
	return Env{Paths: p, LookPath: exec.LookPath, Getenv: os.Getenv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Version: version}, nil
}

func (e Env) service() (Service, error) {
	if e.Service != nil {
		return e.Service, nil
	}
	return NewService(runtime.GOOS, e.Paths.Home, ExecRunner)
}

func (e Env) executable() (string, error) {
	if e.Executable != "" {
		return e.Executable, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func (e Env) lookPath(name string) (string, error) {
	if e.LookPath != nil {
		return e.LookPath(name)
	}
	return exec.LookPath(name)
}

func (e Env) getwd() (string, error) {
	if e.Getwd != nil {
		return e.Getwd()
	}
	return os.Getwd()
}

func (e Env) getenv() func(string) string {
	if e.Getenv != nil {
		return e.Getenv
	}
	return os.Getenv
}

// hub returns the client for c's hub, which may dial the addresses in allowed.
func (e Env) hub(c Config, allowed netguard.Set) *Hub {
	return &Hub{URL: c.HubURL, Token: c.Token, HTTP: e.HTTP, Allowed: allowed, Resolver: e.Resolver}
}

// Main runs `meat-mode bridge <command>`.
func Main(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		return errors.New("usage: meat-mode bridge pair|run|status|agents|uninstall")
	}
	switch args[0] {
	case "pair":
		return pair(ctx, args[1:], env)
	case "run":
		return run(ctx, env)
	case "status":
		return status(ctx, env)
	case "agents":
		return agentsCommand(args[1:], env)
	case "uninstall":
		return uninstall(env)
	}
	return fmt.Errorf("unknown bridge command %q; use pair, run, status, agents or uninstall", args[0])
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseInterleaved parses flags that may come before, between or after the
// positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// readToken reads one line of r as the bridge token.
func readToken(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("no token on standard input")
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read the token from standard input: %w", err)
	}
	if t := strings.TrimSpace(line); t != "" {
		return t, nil
	}
	return "", errors.New("no token on standard input")
}

func pair(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var roots multiFlag
	fs.Var(&roots, "root", "a directory to scan for repos instead of the home folder (repeatable)")
	insecure := fs.Bool("allow-insecure-http", false, "allow plain HTTP to a hub that is not on this machine")
	noService := fs.Bool("no-service", false, "save the config without installing the user service")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: meat-mode bridge pair <hub url> [<token> | -] [--root DIR]... [--allow-insecure-http] [--no-service]")
		fmt.Fprintln(env.Stderr, "The token is the argument, one line of standard input when it is -, or MEAT_MODE_BRIDGE_TOKEN when it is left out.")
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	s, err := LoadSettings(env)
	if err != nil {
		return err
	}
	if len(pos) == 1 && s.Token != "" {
		pos = append(pos, s.Token)
	}
	if len(pos) != 2 {
		fs.Usage()
		return errors.New("pair needs a hub URL and a token")
	}
	if pos[1] == "-" {
		if pos[1], err = readToken(env.Stdin); err != nil {
			return err
		}
	}
	// Start from the file as saved: environment overrides apply to this
	// command's own checks but are never written back.
	cfg, effective := s.Saved, s.Config
	cfg.HubURL, cfg.Token = strings.TrimRight(pos[0], "/"), pos[1]
	// The trusted networks the hub is checked against are saved, so the
	// service, which does not get this shell's environment, checks the same.
	cfg.TrustedNetworks = effective.TrustedNetworks
	allowInsecure := effective.AllowInsecureHTTP || *insecure
	if *insecure {
		cfg.AllowInsecureHTTP = true
	}
	if len(roots) > 0 {
		abs := make([]string, 0, len(roots))
		for _, r := range roots {
			a, err := filepath.Abs(expandHome(r, env.Paths.Home))
			if err != nil {
				return err
			}
			abs = append(abs, a)
		}
		cfg.Roots = abs
	}
	if err := CheckURL(cfg.HubURL, allowInsecure); err != nil {
		return err
	}
	if !*noService && CheckURL(cfg.HubURL, cfg.AllowInsecureHTTP) != nil {
		return errors.New("the service does not get MEAT_MODE_ALLOW_INSECURE_HTTP from this shell; " +
			"pass --allow-insecure-http to save it, or --no-service")
	}
	// The service gets a short PATH, so it holds the folder of every tool a
	// tracker found on this shell's PATH or in the config.
	var tools []string
	look := func(name string) (string, error) {
		p, err := env.lookPath(name)
		if err == nil {
			if abs, aerr := filepath.Abs(p); aerr == nil {
				tools = append(tools, abs)
			}
		}
		return p, err
	}
	ts, err := buildTrackers(cfg, env.getenv(), look)
	if err != nil {
		return err
	}
	checks := checkTrackers(ctx, ts)
	_, versions, err := passed(checks)
	if err != nil {
		return err
	}
	for _, c := range checks {
		if c.Err != nil {
			fmt.Fprintf(env.Stdout, "%s is not available, so its repos are skipped: %v\n", c.Tracker.Info().Title, c.Err)
		}
	}
	tools = append(tools, configuredBins(cfg)...)
	hello := api.Hello{Version: env.Version, BdVersion: beadsVersion(versions), Trackers: versions}
	reply, err := env.hub(cfg, s.Allowed).Hello(ctx, hello)
	if err != nil {
		return fmt.Errorf("cannot reach the hub with that token: %w", err)
	}
	// The switches may have changed since the file was read, from
	// `meat-mode bridge agents` or a running bridge; keep theirs.
	err = WithConfigLock(env.Paths.Config, func() error {
		cur, err := readConfigFile(env.Paths.Config)
		if err != nil {
			return err
		}
		cfg.Agents, cfg.Sync = cur.Agents, cur.Sync
		return cfg.Save(env.Paths.Config)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Paired with %s as bridge %q. Config saved to %s.\n", cfg.HubURL, reply.Bridge, env.Paths.Config)
	if *noService {
		fmt.Fprintln(env.Stdout, "Service not installed; start it with `meat-mode bridge run`.")
		return nil
	}
	svc, err := env.service()
	if err != nil {
		return err
	}
	exe, err := env.executable()
	if err != nil {
		return err
	}
	if err := svc.Install(exe, ServicePath(append(tools, agentPath(s))...)); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Installed and started the user service (%s).\n", svc.File())
	return nil
}

// agentPath is the agent command's absolute path as this shell finds it, so
// the service finds it too; "" when it is not found.
func agentPath(s Settings) string {
	p, err := s.env.lookPath(s.Agent.Command[0])
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return abs
}

// configuredBins are the absolute tool paths the config names.
func configuredBins(cfg Config) []string {
	var out []string
	for _, p := range append([]string{cfg.BD}, slices.Sorted(maps.Values(cfg.TrackerBins))...) {
		if abs, err := filepath.Abs(p); p != "" && err == nil {
			out = append(out, abs)
		}
	}
	return out
}

// New builds a bridge from the settings.
func New(s Settings, log *slog.Logger) (*Bridge, error) {
	if err := s.Paired(); err != nil {
		return nil, err
	}
	if s.runErr != nil {
		return nil, s.runErr
	}
	env, cfg, agents := s.env, s.Config, s.Agent
	if _, err := env.lookPath(agents.Command[0]); err != nil {
		// A service written before the agent was installed has a PATH
		// without it, and every ask would end as failed with no other hint.
		log.Warn("the agent command is not on this bridge's PATH, so asks will fail; see \"Wake an agent on a task\" in docs/deploy.md",
			"command", agents.Command[0])
	}
	j, err := OpenJournal(env.Paths.State)
	if err != nil {
		return nil, fmt.Errorf("open replay journal: %w", err)
	}
	ts, err := s.Trackers()
	if err != nil {
		j.Close()
		return nil, err
	}
	// The runner opens after the journal, whose lock keeps a second bridge
	// from ending this one's runs as left behind.
	runner, err := OpenAgentRunner(filepath.Join(env.Paths.State, "agents"), agents, log)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("open agent journal: %w", err)
	}
	return &Bridge{
		Hub:      s.Hub(),
		Trackers: ts,
		Journal:  j,
		Roots:    s.Roots,
		Home:     env.Paths.Home,
		Switches: cfg.Sync,
		SaveSwitch: func(repo string, on bool) error {
			return SaveSwitch(env.Paths.Config, repo, on)
		},
		Agents:     cfg.Agents,
		LoadAgents: func() (map[string]bool, error) { return LoadAgents(env.Paths.Config) },
		SaveAgents: func(repo string, on bool) error {
			return SaveAgents(env.Paths.Config, repo, on)
		},
		Runner:  runner,
		Version: env.Version,
		Log:     log,
		Poll:    s.Poll,
	}, nil
}

// pollOverride parses MEAT_MODE_BRIDGE_POLL, a test-only override of the
// poll intervals. Empty means the defaults.
func pollOverride(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("MEAT_MODE_BRIDGE_POLL must be a positive duration such as 200ms, not %q", v)
	}
	return d, nil
}

func run(ctx context.Context, env Env) error {
	s, err := LoadSettings(env)
	if err != nil {
		return err
	}
	b, err := New(s, slog.New(slog.NewTextHandler(env.Stderr, nil)))
	if err != nil {
		return err
	}
	defer b.Journal.Close()
	return b.Run(ctx)
}

func status(ctx context.Context, env Env) error {
	out := env.Stdout
	s, err := LoadSettings(env)
	if err != nil {
		return err
	}
	cfg := s.Config
	fmt.Fprintf(out, "Config:   %s\n", env.Paths.Config)
	if cfg.HubURL == "" {
		fmt.Fprintln(out, "Hub:      not paired; run `meat-mode bridge pair <url> <token>`")
	} else {
		fmt.Fprintf(out, "Hub:      %s\n", cfg.HubURL)
	}
	ts, err := s.Trackers()
	if err != nil {
		return err
	}
	checks := checkTrackers(ctx, ts)
	_, versions, _ := passed(checks)
	fmt.Fprintln(out, "Trackers:")
	for _, c := range checks {
		if c.Err != nil {
			fmt.Fprintf(out, "  %-24s not available: %v\n", c.Tracker.Info().Name, c.Err)
		} else {
			fmt.Fprintf(out, "  %-24s %s\n", c.Tracker.Info().Name, orNone(c.Version))
		}
	}
	if svc, err := env.service(); err != nil {
		fmt.Fprintf(out, "Service:  %v\n", err)
	} else {
		fmt.Fprintf(out, "Service:  %s\n", svc.Status())
	}
	fmt.Fprintf(out, "Roots:    %s\n", orNone(strings.Join(s.Roots, ", ")))
	repos, err := s.Repos()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Repos:    %d found\n", len(repos))
	for _, r := range repos {
		fmt.Fprintf(out, "  %-24s %s\n  %-24s %s (%s)\n", r.Name, syncState(r), "", r.Dir, r.Tracker.Info().Title)
	}
	if cfg.HubURL == "" {
		return nil
	}
	if err := s.Paired(); err != nil {
		fmt.Fprintf(out, "Hub check: %v\n", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if r, err := s.Hub().Hello(ctx, api.Hello{Version: env.Version, BdVersion: beadsVersion(versions), Trackers: versions}); err != nil {
		fmt.Fprintf(out, "Hub check: unreachable: %v\n", err)
	} else {
		fmt.Fprintf(out, "Hub check: reachable, this bridge is %q\n", r.Bridge)
	}
	return nil
}

// syncState says whether r is synced and which switch or file decides it.
func syncState(r Repo) string {
	why := "switch it on in Settings"
	switch {
	case r.Switch != nil && *r.Switch:
		why = "switched on in Settings"
	case r.Switch != nil:
		why = "switched off in Settings"
	case r.OptedIn && tracker.RepoOptIn(r.Dir):
		why = "sync: true in " + tracker.OptInFile
	case r.OptedIn:
		why = "opted in by its " + r.Tracker.Info().Title + " config"
	}
	if r.Synced() {
		return "synced (" + why + ")"
	}
	return "not synced (" + why + ")"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func uninstall(env Env) error {
	svc, err := env.service()
	if err != nil {
		return err
	}
	if err := svc.Uninstall(); err != nil {
		return err
	}
	if err := os.Remove(env.Paths.Config); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(env.Stdout, "Removed the user service and %s. The replay journal in %s is kept.\n", env.Paths.Config, env.Paths.State)
	return nil
}
