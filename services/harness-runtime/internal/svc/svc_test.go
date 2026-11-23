package svc

import (
	"context"
	"encoding/json"
	"testing"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestCancelUnknownRun(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.harness-admit.unknown-run-refused")
	payload, err := json.Marshal(idv1.RunClaims{Run: "r", Job: "j", Space: "s", Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), principalKey{},
		httpapi.Principal{Aud: []string{"harness-runtime"}, Payload: payload})
	s := New("harness-runtime", Deps{})
	if _, err := s.cancelRun(ctx, hv1.CancelRunReq{Run: "r"}); herrors.CodeOf(err) != herrors.CodeNotFound {
		t.Fatalf("unknown run was not refused: %v", err)
	}
}
