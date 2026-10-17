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

func (e Env) hub(c Config) *Hub { return &Hub{URL: c.HubURL, Token: c.Token, HTTP: e.HTTP} }

// Main runs `meat-mode bridge <command>`.
func Main(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		return errors.New("usage: meat-mode bridge pair|run|status|uninstall")
	}
	switch args[0] {
	case "pair":
		return pair(ctx, args[1:], env)
	case "run":
		return run(ctx, env)
	case "status":
		return status(ctx, env)
	case "uninstall":
		return uninstall(env)
	}
	return fmt.Errorf("unknown bridge command %q; use pair, run, status or uninstall", args[0])
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
	personal := fs.String("personal", "", "the personal beads repo directory")
	insecure := fs.Bool("allow-insecure-http", false, "allow plain HTTP to a hub that is not on this machine")
	noService := fs.Bool("no-service", false, "save the config without installing the user service")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: meat-mode bridge pair <hub url> [<token> | -] [--root DIR]... [--personal DIR] [--allow-insecure-http] [--no-service]")
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
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		return err
	}
	cfg.HubURL, cfg.Token = strings.TrimRight(pos[0], "/"), pos[1]
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
	if *personal != "" {
		abs, err := filepath.Abs(expandHome(*personal, env.Paths.Home))
		if err != nil {
			return err
		}
		cfg.Personal = abs
	}
	if err := CheckURL(cfg.HubURL, cfg.AllowInsecureHTTP); err != nil {
		return err
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
	if err := svc.Install(exe, ServicePath(bd)); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Installed and started the user service (%s).\n", svc.File())
	return nil
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
	j, err := OpenJournal(env.Paths.State)
	if err != nil {
		return nil, fmt.Errorf("open replay journal: %w", err)
	}
	return &Bridge{
		Hub:      env.hub(cfg),
		Beads:    &beads.CLI{Bin: cfg.BD},
		Journal:  j,
		Roots:    cfg.ResolveRoots(env.Paths.Home),
		Home:     env.Paths.Home,
		Personal: cfg.ResolvePersonal(env.Paths.Home),
		Version:  env.Version,
		Log:      log,
		Poll:     poll,
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
	fmt.Fprintf(out, "Personal: %s\n", orNone(cfg.ResolvePersonal(env.Paths.Home)))
	repos := Discover(roots, env.Paths.Home, cfg.ResolvePersonal(env.Paths.Home), slog.New(slog.NewTextHandler(io.Discard, nil)))
	fmt.Fprintf(out, "Repos:    %d found\n", len(repos))
	for _, r := range repos {
		state := "synced"
		if !r.Synced() {
			state = "not opted in (add `meat-mode.sync: true` to .beads/config.yaml)"
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
