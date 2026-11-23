//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/edges"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/seal"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestPrincipalLinkLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	issuerPub, issuerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	opPub, opKey, err := ed25519.GenerateKey(rand.Reader)
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
	tab := edges.Table{"identity-hub": {
		"operator":    {"CreatePrincipal": true, "LinkBegin": true, "DeactivateHuman": true},
		"channel-hub": {"LinkConfirm": true, "Resolve": true}}}
	s := New("identity-hub", db, Deps{Sign: func(p string, b []byte) (string, error) { return auth.Sign(issuerKey, p, b) },
		Pub: issuerPub, OperatorPub: opPub, Kid: idv1.Kid(issuerPub), Tab: tab, Seal: sealer, Now: func() int64 { return time.Now().Unix() }})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	owner := fmt.Sprintf("owner_%d", time.Now().UnixNano())
	op := func(target, space string) *idv1.Client {
		return operatorTestClient(t, srv.URL, opKey, owner, space, target)
	}
	if _, err := op("CreatePrincipal", "other").CreatePrincipal(ctx, idv1.CreatePrincipalReq{ID: owner, Space: "space"}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("cross-space create: %v", err)
	}
	if _, err := op("CreatePrincipal", "space").CreatePrincipal(ctx, idv1.CreatePrincipalReq{ID: owner, Space: "space"}); err != nil {
		t.Fatal(err)
	}
	code, err := op("LinkBegin", "").LinkBegin(ctx, idv1.LinkBeginReq{Owner: owner})
	if err != nil || code.Code == "" {
		t.Fatalf("link begin: %+v %v", code, err)
	}
	again, err := op("LinkBegin", "").LinkBegin(ctx, idv1.LinkBeginReq{Owner: owner})
	if err != nil || again.Code != code.Code {
		t.Fatalf("retry link: %+v %v", again, err)
	}
	channel := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(srv.URL, serviceToken(t, issuerKey, "channel-hub", "identity-hub"))}}
	link, err := channel.LinkConfirm(ctx, idv1.LinkConfirmReq{Channel: "mock", External: "123", Code: code.Code})
	if err != nil || link.Owner != owner {
		t.Fatalf("confirm: %+v %v", link, err)
	}
	if _, err := channel.LinkConfirm(ctx, idv1.LinkConfirmReq{Channel: "mock", External: "456", Code: code.Code}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("replay: %v", err)
	}
	resolved, err := channel.Resolve(ctx, idv1.ResolveReq{Channel: "mock", External: "123"})
	if err != nil || resolved.Owner != owner || resolved.Space != "space" {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	next, err := op("LinkBegin", "").LinkBegin(ctx, idv1.LinkBeginReq{Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := linkMachine().Sweep(ctx, db, time.Now().Add(25*time.Hour), 32, nil); err != nil {
		t.Fatal(err)
	}
	fresh, err := op("LinkBegin", "").LinkBegin(ctx, idv1.LinkBeginReq{Owner: owner})
	if err != nil || fresh.Code == next.Code {
		t.Fatalf("expired code reused: %+v %v", fresh, err)
	}
	if _, err := op("DeactivateHuman", "").DeactivateHuman(ctx, idv1.DeactivateHumanReq{ID: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := channel.Resolve(ctx, idv1.ResolveReq{Channel: "mock", External: "123"}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("disabled resolve: %v", err)
	}
}

func operatorTestClient(t *testing.T, url string, key ed25519.PrivateKey, owner, space, target string) *idv1.Client {
	t.Helper()
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: owner, Space: space, Target: target, Aud: []string{"identity-hub"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(url, tok)}}
}
