//go:build hhube2e

package lifecycle

import (
	"context"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"testing"
)

func TestOperatorAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatal(err)
	}
	issuer := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, "")}}
	tok, err := issuer.IssueServiceToken(ctx, "task-engine", "channel-hub", Idem("alert-token"), key)
	if err != nil {
		t.Fatal(err)
	}
	client := &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok.Token)}}
	r := cv1.AlertReq{Key: Idem("operator-alert"), Code: "sweep-lag"}
	for range 2 {
		if _, err := client.Alert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := MockClient(t, "operator", "ReadOutbox").Alert(ctx, r); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("operator bypassed service edge: %v", err)
	}
	WaitOperatorDelivery(t, r.Key, "Job deadlines are overdue")
}
