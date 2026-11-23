//go:build hhubdb

package svc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/seal"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestIssuerSealedImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kek := make([]byte, seal.KeyLen)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	sealer, err := seal.New(kek)
	if err != nil {
		t.Fatal(err)
	}
	kid := idv1.Kid(pub)
	sealed, err := sealer.Seal("signing-key", kid, key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(issuerBootstrap{Kid: kid, Pub: pub, Sealed: sealed})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "issuer.bootstrap")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIssuer(ctx, db, sealer, path, pub)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("import issuer: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("bootstrap remains: %v", err)
	}
	got, err = LoadIssuer(ctx, db, sealer, path, pub)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("reload issuer: %v", err)
	}
	wrong, err := seal.New(bytes.Repeat([]byte{1}, seal.KeyLen))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIssuer(ctx, db, wrong, path, pub); err == nil {
		t.Fatal("wrong KEK accepted")
	}
}
