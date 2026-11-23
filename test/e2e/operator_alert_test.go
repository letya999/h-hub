//go:build hhube2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestOperatorAlert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatal(err)
	}
	issuer := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, "")}}
	tok, err := issuer.IssueServiceToken(ctx, "task-engine", "channel-hub", idem("alert-token"), key)
	if err != nil {
		t.Fatal(err)
	}
	client := &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok.Token)}}
	r := cv1.AlertReq{Key: idem("operator-alert"), Code: "sweep-lag"}
	for range 2 {
		if _, err := client.Alert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mockClient(t, "operator", "ReadOutbox").Alert(ctx, r); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("operator bypassed service edge: %v", err)
	}
	waitOperatorDelivery(t, r.Key, "Job deadlines are overdue")
}

func waitOperatorDelivery(t *testing.T, key, want string) {
	t.Helper()
	ctx := context.Background()
	db := adminDB(t, "channel_hub")
	var seq int64
	testkit.Poll(t, 10*time.Second, 100*time.Millisecond, func() bool {
		return db.QueryRowContext(ctx, "SELECT seq FROM mock_outbox WHERE delivery_id=$1", "alert-"+key).Scan(&seq) == nil
	})
	out, err := mockClient(t, "operator", "ReadOutbox").ReadOutbox(ctx,
		cv1.ReadOutboxReq{External: "operator", After: seq - 1})
	if err != nil || len(out.Messages) == 0 || out.Messages[0].Seq != seq ||
		!strings.Contains(out.Messages[0].Body, want) {
		t.Fatalf("operator did not receive alert %d: %+v %v", seq, out, err)
	}
}
