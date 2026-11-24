//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// TestTelegramSecretBodyNotStored pins the live leak: an intercepted
// KEY=value update must keep only the fact of interception — kind and
// message id — while the value never reaches inbound_updates.body.
func TestTelegramSecretBodyNotStored(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	_, selfKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := New("channel-hub", db, Deps{SelfKey: selfKey, Now: time.Now})

	up := normalizeUpdate(rawText(9003, 480637186, 480637186,
		"HHUB_LIVE_CHECK=canary-otter-77"))
	if up.Kind != "secret" {
		t.Fatalf("kind %q", up.Kind)
	}
	if err := s.tg.admitUpdate(ctx, up); err != nil {
		t.Fatalf("admit: %v", err)
	}
	var body string
	if err := db.QueryRowContext(ctx,
		`SELECT body FROM inbound_updates WHERE update_id='9003'`).Scan(&body); err != nil {
		t.Fatalf("intake: %v", err)
	}
	testkit.Deny(t, "sys.secret-leak", !strings.Contains(body, "canary-otter-77"))
	if strings.Contains(body, "canary-otter-77") {
		t.Fatalf("secret persisted in intake body: %q", body)
	}
}
