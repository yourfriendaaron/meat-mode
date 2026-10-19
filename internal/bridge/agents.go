package bridge

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const agentsUsage = "usage: meat-mode bridge agents [on|off <repo>]"

// agentsCommand runs `meat-mode bridge agents`: it lists the repos this bridge finds
// with their agents switch, or switches one on or off. Agents are switched on
// here only; the app can only switch them off.
func agentsCommand(args []string, env Env) error {
	s, err := LoadSettings(env)
	if err != nil {
		return err
	}
	repos, err := s.Repos()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		listAgents(env.Stdout, repos, s.Config.Agents)
		return nil
	case len(args) == 2 && (args[0] == "on" || args[0] == "off"):
		r, err := findRepo(repos, args[1], env.Paths.Home)
		if err != nil {
			return err
		}
		on := args[0] == "on"
		if on && agentOf(r.Tracker) == nil {
			return errors.New(cannot(r.Tracker, api.KindAsk).Error)
		}
		if err := SaveAgents(env.Paths.Config, r.Name, on); err != nil {
			return err
		}
		if on {
			fmt.Fprintf(env.Stdout, "Agents can work in %s.\n", r.Name)
		} else {
			fmt.Fprintf(env.Stdout, "Agents can no longer work in %s.\n", r.Name)
		}
		return nil
	}
	fmt.Fprintln(env.Stderr, agentsUsage)
	fmt.Fprintln(env.Stderr, "With no arguments it lists the repos this bridge finds. <repo> is a repo's name as listed, or the path of its folder (. inside it).")
	return errors.New(agentsUsage)
}

func listAgents(out io.Writer, repos []Repo, switches map[string]bool) {
	if len(repos) == 0 {
		fmt.Fprintln(out, "This bridge finds no repos.")
		return
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	for _, r := range repos {
		state := "off"
		if switches[r.Name] {
			state = "on"
		}
		fmt.Fprintf(out, "%-24s agents %-3s %s\n", r.Name, state, r.Dir)
	}
}

// findRepo matches arg to a repo the bridge found: by the name it reports, or
// else as the path of the repo's folder.
func findRepo(repos []Repo, arg, home string) (Repo, error) {
	for _, r := range repos {
		if r.Name == arg {
			return r, nil
		}
	}
	if abs, err := filepath.Abs(expandHome(arg, home)); err == nil {
		abs = resolvePath(abs)
		for _, r := range repos {
			if resolvePath(r.Dir) == abs {
				return r, nil
			}
		}
	}
	return Repo{}, fmt.Errorf("%q is not a repo this bridge finds with its roots; run `meat-mode bridge agents` to list them", strings.TrimSpace(arg))
}
