// credential-broker: the installation's secret store. The whole
// database is SQLCipher; values are additionally sealed with the
// envelope KEK held outside the volume (blueprint 2.9).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/letya999/h-hub/services/credential-broker/internal/config"
	"github.com/letya999/h-hub/services/credential-broker/internal/svc"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
	"github.com/letya999/h-hub/shared/seal"
)

const listenAddr = ":9000"

var hexKeyRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func main() {
	l := log.New("credential-broker", os.Stderr)
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
	dbKey, err := readDBKey(cfg.DBKey)
	if err != nil {
		return fmt.Errorf("db key: %w", err)
	}
	db, err := svc.Open(ctx, cfg.DBPath, dbKey)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer db.Close() //nolint:errcheck // best-effort close
	sealer, err := seal.Load(cfg.KEKPath)
	if err != nil {
		return fmt.Errorf("seal kek: %w", err)
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
		return fmt.Errorf("edges: %w", err)
	}
	opPub, err := auth.LoadPublicKey(filepath.Join(cfg.Keys, "pubs", "operator.pub"))
	if err != nil {
		return fmt.Errorf("operator key: %w", err)
	}
	s := svc.New(cfg.Sub, db, svc.Deps{
		Issuer:      issuer,
		OperatorPub: opPub,
		EdgeTab:     tab.For(cfg.Sub),
		Sealer:      sealer,
		DBPath:      cfg.DBPath,
		Now:         time.Now,
	}, l)
	sctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return serve(sctx, l, s.Mux())
}

// readDBKey reads the hex database key installed outside the Docker
// volumes (next to the seal KEK). Anything else refuses startup.
func readDBKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("db key %s: %w", path, err)
	}
	key := string(b)
	if len(key) > 0 && key[len(key)-1] == '\n' {
		key = key[:len(key)-1]
	}
	if !hexKeyRe.MatchString(key) {
		return "", fmt.Errorf("db key file %s: not 64 lowercase hex chars", path)
	}
	return key, nil
}

func serve(ctx context.Context, l *log.L, h http.Handler) error {
	srv := &http.Server{Addr: listenAddr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	l.Info(ctx, "listening", "addr", listenAddr)
	if err := lifecycle.Serve(ctx, srv, 5*time.Second); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
