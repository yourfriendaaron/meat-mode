// Command meat-mode runs the hub (`meat-mode hub`) or the bridge
// (`meat-mode bridge ...`).
package main

import (
	"fmt"
	"os"
	_ "time/tzdata" // the hub's time zone must resolve on a machine without zone files
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	case "add", "edit", "done", "reopen":
		err = runPersonal(os.Args[1], os.Args[2:])
	case "hash-password":
		err = runHashPassword(os.Args[2:])
	case "version", "--version":
		fmt.Println(version)
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
  meat-mode add "<title>" [--due TIME] [--note TEXT] [--author NAME] [--id UUID]
                                      add a to-do to the personal list with this machine's bridge config;
                                      prints its id
  meat-mode edit <id> [--title TEXT] [--description TEXT] [--due TIME | --clear-due] [--priority N]
                                      change a to-do this machine added
  meat-mode done <id>                 mark a to-do this machine added as done
  meat-mode reopen <id>               open a to-do this machine added again
  meat-mode hash-password             print the hash of a login password for MEAT_MODE_PASSWORD_HASH
  meat-mode version`)
}
