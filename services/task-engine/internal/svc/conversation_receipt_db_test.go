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

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

func TestStartReceiptUsesBoundConversation(t *testing.T) {
	t.Parallel()
	s, _, job, run := waitingFixture(t)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s.deps.Issuer = pub
	server := httptest.NewServer(s.Mux())
	t.Cleanup(server.Close)
	start := tv1.ReportRunReq{Run: run, Events: []tv1.RunEvent{{Seq: 1, Kind: "start"}}}
	report := func(owner string) (string, error) {
		t.Helper()
		// The pinned rollback issuer signs the run scope without a conversation field.
		claims, err := json.Marshal(idv1.RunClaims{Sub: "run:" + run, Owner: owner, Job: job, Run: run,
			Space: "s", Digest: "digest", Aud: []string{"task-engine"}, Exp: time.Now().Add(time.Minute).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		token, err := auth.Sign(key, "run", claims)
		if err != nil {
			t.Fatal(err)
		}
		c := tv1.HTTP{C: httpapi.NewClient(server.URL, token)}
		var receipt map[string]string
		err = c.Call(context.Background(), "ReportRun", start, &receipt)
		return receipt["conversation_key"], err
	}
	const want = "1bdcb1d392af7958d35b7fa90060f53bc118a5fd70aa9981290395c8db7af42d"
	if got, err := report("o"); err != nil || got != want {
		t.Fatalf("start receipt for the bound conversation: %q, %v", got, err)
	}
	if got, err := report("other"); got != "" || herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("start receipt for another owner: %q, %v", got, err)
	}
}
