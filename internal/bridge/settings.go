package bridge

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Settings is the bridge's effective configuration: bridge.json with every
// environment override applied, read once. Each command loads one and asks
// it, so no override is read anywhere else.
type Settings struct {
	// Saved is bridge.json as saved, without the overrides. Only it is ever
	// written back.
	Saved Config
	// Config is Saved with MEAT_MODE_HUB_URL, MEAT_MODE_ALLOW_INSECURE_HTTP
	// and MEAT_MODE_TRUSTED_NETWORKS applied.
	Config Config
	// Roots are the scan roots with ~ expanded; the home folder when unset.
	Roots []string
	// Allowed is the address set the hub client may dial.
	Allowed netguard.Set
	// Token is MEAT_MODE_BRIDGE_TOKEN, the token pair uses when it is given
	// none.
	Token string
	// Poll is MEAT_MODE_BRIDGE_POLL, a test-only override of every poll
	// interval; 0 keeps the defaults.
	Poll time.Duration
	// Agent is the agent harness, its command and its limits from
	// MEAT_MODE_AGENT_*.
	Agent AgentConfig

	// runErr is a bad MEAT_MODE_BRIDGE_POLL or MEAT_MODE_AGENT_* value. Only
	// the running bridge uses them, so only New refuses it.
	runErr error
	env    Env
}

// LoadSettings reads bridge.json and the environment of env. A missing file
// gives empty settings. It fails on a bad override the hub client depends on.
func LoadSettings(env Env) (Settings, error) {
	getenv := env.getenv()
	saved, err := readConfigFile(env.Paths.Config)
	if err != nil {
		return Settings{}, err
	}
	cfg, err := applyOverrides(saved, getenv)
	if err != nil {
		return Settings{}, err
	}
	allowed, err := cfg.Allowed()
	if err != nil {
		return Settings{}, err
	}
	s := Settings{
		Saved:   saved,
		Config:  cfg,
		Roots:   cfg.ResolveRoots(env.Paths.Home),
		Allowed: allowed,
		Token:   strings.TrimSpace(getenv("MEAT_MODE_BRIDGE_TOKEN")),
		env:     env,
	}
	var pollErr, agentErr error
	s.Poll, pollErr = pollOverride(getenv("MEAT_MODE_BRIDGE_POLL"))
	s.Agent, agentErr = AgentConfigFromEnv(getenv)
	s.runErr = errors.Join(pollErr, agentErr)
	return s, nil
}

// Paired reports why this machine cannot talk to its hub: no hub URL or
// token, or a URL the policy refuses. nil means it is paired.
func (s Settings) Paired() error {
	if s.Config.HubURL == "" || s.Config.Token == "" {
		return errors.New("this machine is not paired with a hub; run `meat-mode bridge pair <url> <token>` first")
	}
	return CheckURL(s.Config.HubURL, s.Config.AllowInsecureHTTP)
}

// Hub returns the client for the paired hub. Check Paired first.
func (s Settings) Hub() *Hub { return s.env.hub(s.Config, s.Allowed) }

// Harness builds the configured agent harness with this machine's PATH.
func (s Settings) Harness() (harness.Harness, error) {
	return s.Agent.NewHarness(s.env.getenv(), s.env.lookPath)
}

// Trackers builds every registered tracker with this machine's tool paths,
// in the order Detect is tried.
func (s Settings) Trackers() ([]tracker.Tracker, error) {
	return buildTrackers(s.Config, s.env.getenv(), s.env.lookPath)
}

// Repos discovers the repos of every registered tracker under the roots,
// with the sync switches set in the app applied. The walk logs nothing.
func (s Settings) Repos() ([]Repo, error) {
	ts, err := s.Trackers()
	if err != nil {
		return nil, err
	}
	repos := Discover(s.Roots, s.env.Paths.Home, ts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := range repos {
		repos[i].Switch = syncSwitch(s.Config.Sync, repos[i].Name)
	}
	return repos, nil
}

// pairedHub is the hub this machine's bridge config pairs it with.
func pairedHub(env Env) (*Hub, error) {
	s, err := LoadSettings(env)
	if err != nil {
		return nil, err
	}
	if err := s.Paired(); err != nil {
		return nil, err
	}
	return s.Hub(), nil
}
