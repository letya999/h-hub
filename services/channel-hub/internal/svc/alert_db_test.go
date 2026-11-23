//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestOperatorAlertDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, client, opKey, url := alertFixture(t, ctx)
	key, err := randomID("lag-")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := client.Alert(ctx, cv1.AlertReq{Key: key, Code: "sweep-lag"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Alert(ctx, cv1.AlertReq{Key: key, Code: "watchdog-stale"}); herrors.CodeOf(err) != herrors.CodeConflict {
		t.Fatalf("alert key reused for another signal: %v", err)
	}
	if _, err := client.Alert(ctx, cv1.AlertReq{Key: "bad", Code: "unknown"}); herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("unknown alert accepted: %v", err)
	}
	if _, err := (&cv1.Client{T: cv1.HTTP{C: httpapi.NewClient(url, "")}}).Alert(ctx,
		cv1.AlertReq{Key: "no-token", Code: "sweep-lag"}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("unauthenticated alert accepted: %v", err)
	}
	if err := sendMockDelivery(ctx, s, "alert-"+key); err != nil {
		t.Fatal(err)
	}
	res, err := mockClient(t, url, opKey, "operator", "ReadOutbox").ReadOutbox(ctx,
		cv1.ReadOutboxReq{External: "operator"})
	if err != nil || len(res.Messages) != 1 {
		t.Fatalf("operator alert: %+v %v", res, err)
	}
}

func alertFixture(t *testing.T, ctx context.Context) (*Server, *cv1.Client, ed25519.PrivateKey, string) {
	t.Helper()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	issuer, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	opPub, opKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := New("channel-hub", db, Deps{Issuer: issuer, OperatorPub: opPub,
		Edges: edges.Table{"channel-hub": {"task-engine": {"Alert": true},
			"operator": {"ReadOutbox": true}}}, Now: time.Now})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	claims, err := json.Marshal(idv1.ServiceClaims{Sub: "task-engine", Aud: []string{"channel-hub"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "service", claims)
	if err != nil {
		t.Fatal(err)
	}
	client := &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient(srv.URL, tok)}}
	return s, client, opKey, srv.URL
}
