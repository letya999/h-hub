//go:build hhube2e

package channel

import (
	"context"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"testing"
	"time"
)

func TestMockChannelThreeUsers(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.tenant-isolation")
	testkit.Req(t, "sys.forged-token")
	testkit.Req(t, "sys.service-edge")
	owners := []string{"mock_alice_" + Nonce, "mock_boris_" + Nonce, "mock_cy_" + Nonce}
	for _, o := range owners {
		space, code := AddUser(t, o)
		_, err := MockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: Idem("link", o), Text: "/start " + code})
		if err != nil {
			t.Fatalf("link inject %s: %v", o, err)
		}
		cursor := WaitMock(t, o, 0, "Account linked", 10*time.Second)
		_, err = MockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: Idem("unknown", o), Text: "/unknown"})
		if err != nil {
			t.Fatal(err)
		}
		cursor = WaitMock(t, o, cursor, "Commands:", 10*time.Second)
		jobs, err := TaskClient(t, OwnerSpace{Owner: o, Space: space}).ListJobs(context.Background(), tv1.ListJobsReq{})
		if err != nil || len(jobs.Jobs) != 0 {
			t.Fatalf("unknown command created a job: %+v %v", jobs, err)
		}
		_, err = MockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: Idem("msg", o), Text: "hello " + o})
		if err != nil {
			t.Fatalf("message inject %s: %v", o, err)
		}
		WaitMock(t, o, cursor, "answer: hello "+o, 5*time.Minute)
	}
	_, err := MockClient(t, owners[0], "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[1]})
	testkit.Deny(t, "sys.tenant-isolation", herrors.CodeOf(err) == herrors.CodeForbidden)
	_, err = MockClient(t, owners[0], "Inject").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[0]})
	testkit.Deny(t, "sys.service-edge", herrors.CodeOf(err) == herrors.CodeForbidden)
	forgedClient := ForgedMock(t, owners[0])
	_, err = forgedClient.ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[0]})
	testkit.Deny(t, "sys.forged-token", herrors.CodeOf(err) == herrors.CodeUnauthorized)
}
