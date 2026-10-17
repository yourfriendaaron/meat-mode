package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourfriendaaron/meat-mode/internal/bridge"
)

// runBridge runs `meat-mode bridge <command>`.
func runBridge(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env, err := bridge.DefaultEnv(version)
	if err != nil {
		return err
	}
	return bridge.Main(ctx, args, env)
}

// runAdd runs `meat-mode add`.
func runAdd(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env, err := bridge.DefaultEnv(version)
	if err != nil {
		return err
	}
	return bridge.Add(ctx, args, env)
}
