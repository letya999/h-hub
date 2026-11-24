//go:build hhubdb

package svc

import (
	"context"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/letya999/h-hub/shared/testkit"
)

// TestTelegramSecretBodyNotStored pins the live leak: an intercepted
// KEY=value update must keep only the fact of interception — kind and
// message id — while the value never reaches inbound_updates.body.
func TestTelegramSecretBodyNotStored(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")
	ctx := context.Background()
	db, selfKey := schemaDB(t)
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
}
