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

	"github.com/letya999/h-hub/services/channel-hub/internal/config"
	"github.com/letya999/h-hub/services/channel-hub/internal/svc"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/store"
)

func main() {
	l := log.New("channel-hub", os.Stderr)
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
	deps, err := loadDeps(cfg)
	if err != nil {
		return err
	}
	s := svc.New(cfg.Sub, db, deps)
	sctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	g := lifecycle.NewGroup(sctx)
	defer g.Stop()
	g.Go(func(ctx context.Context) { s.Loops(ctx, l) })
	srv := &http.Server{Addr: ":9000", Handler: s.Mux(), ReadHeaderTimeout: 5 * time.Second}
	if err := lifecycle.Serve(sctx, srv, 5*time.Second); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func loadDeps(cfg config.C) (svc.Deps, error) {
	key, err := auth.LoadPrivateKey(filepath.Join(cfg.Keys, "service.key"))
	if err != nil {
		return svc.Deps{}, fmt.Errorf("service key: %w", err)
	}
	issuer, err := auth.LoadPublicKey(filepath.Join(cfg.Keys, "pubs", "identity-hub.pub"))
	if err != nil {
		return svc.Deps{}, fmt.Errorf("issuer: %w", err)
	}
	opPub, err := auth.LoadPublicKey(filepath.Join(cfg.Keys, "pubs", "operator.pub"))
	if err != nil {
		return svc.Deps{}, fmt.Errorf("operator: %w", err)
	}
	edgeData, err := os.ReadFile(filepath.Join(cfg.Keys, "edges"))
	if err != nil {
		return svc.Deps{}, fmt.Errorf("edges: %w", err)
	}
	tab, err := edges.Parse(string(edgeData))
	if err != nil {
		return svc.Deps{}, fmt.Errorf("edge table: %w", err)
	}
	return svc.Deps{IdentityURL: cfg.IdentityURL, TaskURL: cfg.TaskURL, SpawnURL: cfg.SpawnURL, Issuer: issuer,
		OperatorPub: opPub, SelfKey: key, Edges: tab.For(cfg.Sub), Now: time.Now}, nil
}
