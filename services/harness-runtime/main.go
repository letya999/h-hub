// harness-runtime: the space-zone runtime. Strict admission, no
// database, host-rendered read-only config, exact env allowlist.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
	"github.com/letya999/h-hub/services/harness-runtime/internal/hermes"
	"github.com/letya999/h-hub/services/harness-runtime/internal/svc"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
)

const listenAddr = ":9000"

func main() {
	l := log.New("harness-runtime", os.Stderr)
	if err := run(context.Background(), l); err != nil {
		l.Error(context.Background(), "fatal", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, l *log.L) error {
	cfg, err := config.Env()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	rendered, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("rendered config: %w", err)
	}
	issuer, err := cfg.IssuerKey()
	if err != nil {
		return fmt.Errorf("issuer key: %w", err)
	}
	sigctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sctx, quit := context.WithCancelCause(sigctx)
	gateway, done, err := hermes.Start(sctx, quit)
	if err != nil {
		quit(err)
		return fmt.Errorf("upstream: %w", err)
	}
	defer func() { quit(nil); <-done }()
	s := svc.New(cfg.Sub, svc.Deps{
		Issuer:   issuer,
		Now:      time.Now,
		Gateway:  gateway,
		Log:      l,
		Rendered: rendered,
	})
	go serveBridge(sctx, s.Bridge(), quit)
	probe, cancel := context.WithTimeout(sctx, 85*time.Second)
	defer cancel()
	if err := gateway.Ready(probe); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	srv := &http.Server{Addr: listenAddr, Handler: s.Mux(), ReadHeaderTimeout: 5 * time.Second}
	l.Info(sctx, "listening", "addr", listenAddr)
	if err := lifecycle.Serve(sctx, srv, 5*time.Second); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if sigctx.Err() == nil {
		return fmt.Errorf("upstream: %w", context.Cause(sctx))
	}
	return nil
}

func serveBridge(ctx context.Context, handler http.Handler, stop context.CancelCauseFunc) {
	bridge := &http.Server{Addr: "127.0.0.1:8643", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := lifecycle.Serve(ctx, bridge, 5*time.Second); err != nil {
		stop(fmt.Errorf("model and tool bridge: %w", err))
	}
}
