// Command meat-mode runs the hub (`meat-mode hub`) or the bridge
// (`meat-mode bridge ...`).
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	_ "time/tzdata" // the hub's time zone must resolve on a machine without zone files
)

// version and commit are set at build time with -ldflags
// "-X main.version=v1.2.3 -X main.commit=abc1234". Without commit, the Git
// revision Go stamps into a build from a checkout stands in.
var (
	version = "dev"
	commit  = ""
)

// fullVersion is the version and its commit, such as "v1.2.3 (abc1234)",
// which the bridge reports to the hub and Settings shows.
func fullVersion() string {
	return versionWithCommit(version, commit, buildRevision())
}

func versionWithCommit(version, commit, revision string) string {
	if commit == "" {
		commit = revision
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit == "" || commit == version {
		return version
	}
	return version + " (" + commit + ")"
}

// buildRevision is the Git commit Go stamped into the binary, if any.
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "hub":
		err = runHub(os.Args[2:])
	case "bridge":
		err = runBridge(os.Args[2:])
	case "add", "edit", "done", "reopen", "list", "show", "projects":
		err = runPersonal(os.Args[1], os.Args[2:])
	case "hash-password":
		err = runHashPassword(os.Args[2:])
	case "version", "--version":
		fmt.Println(fullVersion())
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "meat-mode:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  meat-mode hub                       run the hub (settings from MEAT_MODE_* env)
  meat-mode bridge pair <url> <token> pair this machine with a hub and install the user service
  meat-mode bridge run                run the bridge in the foreground
  meat-mode bridge status             show config, repos and hub reachability
  meat-mode bridge uninstall          remove the user service and config
  meat-mode add "<title>" [--project NAME|.] [--parent ID] [--due TIME] [--note TEXT] [--author NAME] [--id UUID]
                                      add a to-do to the personal list, or to a project with --project
                                      ("." for this folder's repo), with this machine's bridge config;
                                      --parent makes it a sub-task of any open to-do of that list;
                                      prints its id
  meat-mode list [--project NAME|.] [--closed | --all] [--search TEXT] [--tag TAG]... [--json]
                                      list the to-dos of the personal list or a project, whoever added them
  meat-mode show <id> [--json]        show one to-do, with its description and notes
  meat-mode projects [--json]         list the projects
  meat-mode edit <id> [--title TEXT] [--description TEXT] [--due TIME | --clear-due] [--priority N]
                 [--parent ID | --no-parent]
                                      change a to-do, whoever added it
  meat-mode done <id>                 mark a to-do as done
  meat-mode reopen <id>               open a to-do again
                                      (show, edit, done and reopen take --project as add does)
  meat-mode hash-password             print the hash of a login password for MEAT_MODE_PASSWORD_HASH
  meat-mode version`)
}
