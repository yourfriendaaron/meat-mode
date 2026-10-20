package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourfriendaaron/meat-mode/internal/bridge"
	_ "github.com/yourfriendaaron/meat-mode/internal/tracker/all" // every tracker the bridge ships
)

// runBridge runs `meat-mode bridge <command>`.
func runBridge(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env, err := bridge.DefaultEnv(fullVersion())
	if err != nil {
		return err
	}
	return bridge.Main(ctx, args, env)
}

// runPersonal runs `meat-mode add`, `edit`, `done` or `reopen`, which
// change the personal list or a project with this machine's bridge config.
func runPersonal(cmd string, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env, err := bridge.DefaultEnv(fullVersion())
	if err != nil {
		return err
	}
	run := map[string]func(context.Context, []string, bridge.Env) error{
		"add": bridge.Add, "edit": bridge.Edit, "done": bridge.Done, "reopen": bridge.Reopen,
	}[cmd]
	return run(ctx, args, env)
}
