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

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Options plug in the parts other packages build.
type Options struct {
	// Push builds the Web Push registry once the store is open. Nil turns
	// the push routes off.
	Push func(cfg Config, st *store.Store) (PushRegistry, error)
	// OnStart runs once the store is open, before the hub serves. Start
	// background work here (the reminder scheduler) and stop it when ctx
	// ends; ctx ends when the hub shuts down. cfg is the validated config
	// Run serves with.
	OnStart func(ctx context.Context, cfg Config, st *store.Store) error

	// lookup replaces public DNS in the public DNS check, for tests.
	lookup dnsLookup
}

// newHTTPServer wraps h with the hub's timeouts. ReadTimeout bounds reading
// a whole request, body included; there is no WriteTimeout, which would cut
// off the event stream.
func newHTTPServer(ctx context.Context, h *Server) *http.Server {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// Requests keep ctx's values but not its end, so a signal lets
		// Shutdown drain them instead of cancelling them all at once. Event
		// streams end through Close.
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	srv.RegisterOnShutdown(h.Close)
	return srv
}

// Run opens the store, serves until ctx ends or the process gets SIGINT or
// SIGTERM, then shuts down gracefully.
func Run(ctx context.Context, cfg Config, opts Options) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cfg.LoadPasswordHash(); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	settings, err := st.Settings()
	if err != nil {
		return err
	}
	switch {
	case cfg.PasswordHash != "":
		slog.Info("login is on, with the password from the environment")
	case settings.PasswordHash != "":
		slog.Info("login is on, with the password set in the app")
	default:
		slog.Warn("login is off: keep the hub behind Tailscale or another private network")
	}
	if err := mintSetupCode(cfg, st); err != nil {
		return err
	}

	var push PushRegistry
	if opts.Push != nil {
		if push, err = opts.Push(cfg, st); err != nil {
			return fmt.Errorf("push notifications: %w", err)
		}
	}
	// Old logins end before OnStart starts the reminders, so none reach a
	// device that is logged out.
	h := NewServer(cfg, st, push)
	if opts.lookup != nil {
		h.lookup = opts.lookup
	}
	// The public DNS check comes before anything starts that the hub
	// would have to stop again.
	switch d := h.runPublicDNSCheck(ctx); d.State {
	case api.DNSPublic:
		return publicDNSError(cfg.PublicURL, d)
	case api.DNSPrivate:
		slog.Info("public DNS has no public address for MEAT_MODE_PUBLIC_URL", "host", hostOf(cfg.PublicURL))
	}
	if h.publicDNS().State != api.DNSSkipped {
		go h.watchPublicDNS(ctx)
	}
	if err := h.endOtherLogins(ctx); err != nil {
		return err
	}
	if opts.OnStart != nil {
		if err := opts.OnStart(ctx, cfg, st); err != nil {
			return err
		}
	}

	// Removed attachments are purged and stray files cleared at start and
	// every few minutes after. The sweep ends before the store closes.
	sweepCtx, stopSweep := context.WithCancel(ctx)
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		h.sweepAttachments(sweepCtx, store.AttachmentSweepEvery)
	}()
	defer func() {
		stopSweep()
		<-swept
	}()

	srv := newHTTPServer(ctx, h)

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
