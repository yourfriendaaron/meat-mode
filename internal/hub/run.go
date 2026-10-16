package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Options plug in the parts other packages build.
type Options struct {
	// Push builds the Web Push registry once the store is open. Nil turns
	// the push routes off.
	Push func(cfg Config, st *store.Store) (PushRegistry, error)
	// OnStart runs once the store is open, before the hub serves. Start
	// background work here (the reminder scheduler) and stop it when ctx
	// ends; ctx ends when the hub shuts down.
	OnStart func(ctx context.Context, st *store.Store) error
}

// Run opens the store, serves until ctx ends or the process gets SIGINT or
// SIGTERM, then shuts down gracefully.
func Run(ctx context.Context, cfg Config, opts Options) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	var push PushRegistry
	if opts.Push != nil {
		if push, err = opts.Push(cfg, st); err != nil {
			return fmt.Errorf("push notifications: %w", err)
		}
	}
	if opts.OnStart != nil {
		if err := opts.OnStart(ctx, st); err != nil {
			return err
		}
	}

	h := NewServer(cfg, st, push)
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	srv.RegisterOnShutdown(h.Close)

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	slog.Info("hub listening", "addr", ln.Addr().String())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("hub shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
