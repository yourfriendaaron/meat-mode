package bridge

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/beads"
)

// Env is what the bridge commands touch outside the process. Tests replace
// the paths, the service manager and the executable.
type Env struct {
	Paths      Paths
	Service    Service // nil: the platform's user service manager
	Executable string  // "": this process's executable
	LookPath   func(string) (string, error)
	HTTP       *http.Client
	Resolver   Resolver  // nil: the system resolver
	Stdin      io.Reader // nil: no input
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
	return Env{Paths: p, LookPath: exec.LookPath, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Version: version}, nil
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

// hub returns the client for c's hub. c comes from LoadConfig, which has
// checked trustedNetworks; a bad value here leaves only the private set.
func (e Env) hub(c Config) *Hub {
	allowed, _ := c.Allowed()
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
	fs.Var(&roots, "root", "a directory to scan for beads repos instead of the home folder (repeatable)")
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
	if len(pos) == 1 {
		if t := strings.TrimSpace(os.Getenv("MEAT_MODE_BRIDGE_TOKEN")); t != "" {
			pos = append(pos, t)
		}
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
	cfg, err := readConfigFile(env.Paths.Config)
	if err != nil {
		return err
	}
	effective, err := LoadConfig(env.Paths.Config)
	if err != nil {
		return err
	}
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
	bd, ver, err := findBD(ctx, cfg.BD, env)
	if err != nil {
		return err
	}
	cfg.BD = bd
	reply, err := env.hub(cfg).Hello(ctx, api.Hello{Version: env.Version, BdVersion: ver})
	if err != nil {
		return fmt.Errorf("cannot reach the hub with that token: %w", err)
	}
	if err := cfg.Save(env.Paths.Config); err != nil {
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
	if err := svc.Install(exe, ServicePath(bd, agentPath(env))); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Installed and started the user service (%s).\n", svc.File())
	return nil
}

// agentPath is the agent command's absolute path as this shell finds it, so
// the service finds it too; "" when it is not found.
func agentPath(env Env) string {
	c, err := AgentConfigFromEnv(os.Getenv)
	if err != nil {
		return ""
	}
	look := env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	p, err := look(c.Command[0])
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return abs
}

// findBD resolves bd to an absolute path and checks its version.
func findBD(ctx context.Context, configured string, env Env) (path, version string, err error) {
	path = configured
	if path == "" {
		look := env.LookPath
		if look == nil {
			look = exec.LookPath
		}
		if path, err = look("bd"); err != nil {
			return "", "", errors.New("bd is not on PATH; install beads 1.3.0 or newer first")
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	version, err = (&beads.CLI{Bin: path}).Version(ctx)
	if err != nil {
		return "", "", fmt.Errorf("cannot run bd: %w", err)
	}
	if err := beads.CheckVersion(version); err != nil {
		return "", "", err
	}
	return path, version, nil
}

// New builds a bridge from a config.
func New(cfg Config, env Env, log *slog.Logger) (*Bridge, error) {
	if err := CheckURL(cfg.HubURL, cfg.AllowInsecureHTTP); err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, errors.New("no token is set; run `meat-mode bridge pair <url> <token>`")
	}
	poll, err := pollOverride(os.Getenv("MEAT_MODE_BRIDGE_POLL"))
	if err != nil {
		return nil, err
	}
	agents, err := AgentConfigFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	look := env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look(agents.Command[0]); err != nil {
		// A service written before the agent was installed has a PATH
		// without it, and every ask would end as failed with no other hint.
		log.Warn("the agent command is not on this bridge's PATH, so asks will fail; see \"Wake an agent on a task\" in docs/deploy.md",
			"command", agents.Command[0])
	}
	j, err := OpenJournal(env.Paths.State)
	if err != nil {
		return nil, fmt.Errorf("open replay journal: %w", err)
	}
	bd := &beads.CLI{Bin: cfg.BD}
	// The runner opens after the journal, whose lock keeps a second bridge
	// from ending this one's runs as left behind.
	runner, err := OpenAgentRunner(filepath.Join(env.Paths.State, "agents"), agents, bd, log)
	if err != nil {
		j.Close()
		return nil, fmt.Errorf("open agent journal: %w", err)
	}
	return &Bridge{
		Hub:      env.hub(cfg),
		Beads:    bd,
		Journal:  j,
		Roots:    cfg.ResolveRoots(env.Paths.Home),
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
		Poll:    poll,
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
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		return err
	}
	b, err := New(cfg, env, slog.New(slog.NewTextHandler(env.Stderr, nil)))
	if err != nil {
		return err
	}
	defer b.Journal.Close()
	return b.Run(ctx)
}

func status(ctx context.Context, env Env) error {
	out := env.Stdout
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Config:   %s\n", env.Paths.Config)
	if cfg.HubURL == "" {
		fmt.Fprintln(out, "Hub:      not paired; run `meat-mode bridge pair <url> <token>`")
	} else {
		fmt.Fprintf(out, "Hub:      %s\n", cfg.HubURL)
	}
	bd, bdVersion, err := findBD(ctx, cfg.BD, env)
	if err != nil {
		fmt.Fprintf(out, "bd:       %v\n", err)
	} else {
		fmt.Fprintf(out, "bd:       %s (%s)\n", bdVersion, bd)
	}
	if svc, err := env.service(); err != nil {
		fmt.Fprintf(out, "Service:  %v\n", err)
	} else {
		fmt.Fprintf(out, "Service:  %s\n", svc.Status())
	}
	roots := cfg.ResolveRoots(env.Paths.Home)
	fmt.Fprintf(out, "Roots:    %s\n", orNone(strings.Join(roots, ", ")))
	repos := Discover(roots, env.Paths.Home, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fmt.Fprintf(out, "Repos:    %d found\n", len(repos))
	for _, r := range repos {
		if on, ok := cfg.Sync[r.Name]; ok {
			r.Switch = &on
		}
		state := "synced"
		switch {
		case r.Switch != nil && *r.Switch:
			state = "synced (switched on in Settings)"
		case r.Switch != nil:
			state = "not synced (switched off in Settings)"
		case r.OptedIn:
			state = "synced (meat-mode.sync: true in .beads/config.yaml)"
		default:
			state = "not synced (switch it on in Settings)"
		}
		fmt.Fprintf(out, "  %-24s %s\n  %-24s %s\n", r.Name, state, "", r.Dir)
	}
	if cfg.HubURL == "" {
		return nil
	}
	if err := CheckURL(cfg.HubURL, cfg.AllowInsecureHTTP); err != nil {
		fmt.Fprintf(out, "Hub check: %v\n", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if r, err := env.hub(cfg).Hello(ctx, api.Hello{Version: env.Version, BdVersion: bdVersion}); err != nil {
		fmt.Fprintf(out, "Hub check: unreachable: %v\n", err)
	} else {
		fmt.Fprintf(out, "Hub check: reachable, this bridge is %q\n", r.Bridge)
	}
	return nil
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
