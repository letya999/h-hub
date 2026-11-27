// tool-hub: the admission point for tool calls — admits, projects and
// invokes tools under the caller's effective spec (blueprint 2.5).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	"github.com/letya999/h-hub/services/tool-hub/internal/config"
	"github.com/letya999/h-hub/services/tool-hub/internal/svc"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/store"
)

const listenAddr = ":9000"

func main() {
	l := log.New("tool-hub", os.Stderr)
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
	if err := wv1.PrepareSlots(cfg.WorkerSlots); err != nil {
		return fmt.Errorf("initialize worker socket volume: %w", err)
	}
	db, err := store.Connect(ctx, cfg.DSN)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		return fmt.Errorf("fsm schema: %w", err)
	}
	if err := store.Migrate(ctx, db, svc.Migrations(), "migrations"); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	key, err := auth.LoadPrivateKey(filepath.Join(cfg.Keys, "service.key"))
	if err != nil {
		return fmt.Errorf("service key: %w", err)
	}
	issuer, err := auth.LoadPublicKey(filepath.Join(cfg.Keys, "pubs", "identity-hub.pub"))
	if err != nil {
		return fmt.Errorf("issuer key: %w", err)
	}
	edgeData, err := os.ReadFile(filepath.Join(cfg.Keys, "edges"))
	if err != nil {
		return fmt.Errorf("edges: %w", err)
	}
	tab, err := edges.Parse(string(edgeData))
	if err != nil {
		return fmt.Errorf("edge table: %w", err)
	}
	s := svc.New(cfg.Sub, db, svc.Deps{
		IdentityURL: cfg.IdentityURL,
		SpawnURL:    cfg.SpawnURL,
		ChannelURL:  cfg.ChannelURL,
		TaskURL:     cfg.TaskURL,
		Issuer:      issuer,
		SelfKey:     key,
		Edges:       tab.For(cfg.Sub),
		Now:         time.Now,
	}, l)
	sctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	g := lifecycle.NewGroup(sctx)
	defer g.Stop()
	g.Go(func(ctx context.Context) { s.Loops(ctx, l) })
	return serve(sctx, l, s.Mux())
}

func serve(ctx context.Context, l *log.L, h http.Handler) error {
	srv := &http.Server{Addr: listenAddr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	l.Info(ctx, "listening", "addr", listenAddr)
	if err := lifecycle.Serve(ctx, srv, 5*time.Second); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
