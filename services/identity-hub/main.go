// identity-hub owns principals, links and sealed token issuance.
package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/services/identity-hub/internal/config"
	"github.com/letya999/h-hub/services/identity-hub/internal/svc"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/seal"
	"github.com/letya999/h-hub/shared/store"
)

const listenAddr = ":9000"

func main() {
	l := log.New("identity-hub", os.Stderr)
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
	sealer, err := seal.Load(cfg.KEK)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	pubs, err := loadPubs(filepath.Join(cfg.Keys, "pubs"))
	if err != nil {
		return fmt.Errorf("pubs: %w", err)
	}
	issuerPub, ok := pubs[cfg.Sub]
	if !ok {
		return fmt.Errorf("issuer public key missing")
	}
	key, err := svc.LoadIssuer(ctx, db, sealer, filepath.Join(filepath.Dir(cfg.KEK), "issuer.bootstrap"), issuerPub)
	if err != nil {
		return fmt.Errorf("service key: %w", err)
	}
	opPub, err := operatorPub(pubs)
	if err != nil {
		return err
	}
	edgeData, err := os.ReadFile(filepath.Join(cfg.Keys, "edges"))
	if err != nil {
		return fmt.Errorf("edges: %w", err)
	}
	tab, err := edges.Parse(string(edgeData))
	if err != nil {
		return fmt.Errorf("edges: %w", err)
	}
	pub := issuerPub
	signer := func(purpose string, payload []byte) (string, error) {
		return auth.Sign(key, purpose, payload)
	}
	s := svc.New(cfg.Sub, db, svc.Deps{
		Sign:        idv1.Signer(signer),
		Pub:         pub,
		OperatorPub: opPub,
		Kid:         idv1.Kid(pub),
		Pubs:        pubs,
		Tab:         tab.For(cfg.Sub),
		Nonce:       svc.SQLNoncer{DB: db},
		Now:         func() int64 { return time.Now().Unix() },
		Seal:        sealer,
	})
	return serveIdentity(ctx, l, s)
}

func serveIdentity(ctx context.Context, l *log.L, s *svc.Server) error {
	g := lifecycle.NewGroup(ctx)
	defer g.Stop()
	g.Go(func(ctx context.Context) { s.SweepLinks(ctx, l) })
	return serve(ctx, l, s.Mux())
}

func operatorPub(pubs map[string]ed25519.PublicKey) (ed25519.PublicKey, error) {
	pub, ok := pubs["operator"]
	if !ok {
		return nil, fmt.Errorf("operator public key missing")
	}
	return pub, nil
}

func serve(ctx context.Context, l *log.L, h http.Handler) error {
	sctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Addr: listenAddr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	l.Info(sctx, "listening", "addr", listenAddr)
	if err := lifecycle.Serve(sctx, srv, 5*time.Second); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func loadPubs(dir string) (map[string]ed25519.PublicKey, error) {
	out := map[string]ed25519.PublicKey{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("pubs: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pub" {
			continue
		}
		pk, err := auth.LoadPublicKey(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("pub %s: %w", e.Name(), err)
		}
		out[e.Name()[:len(e.Name())-len(".pub")]] = pk
	}
	return out, nil
}
