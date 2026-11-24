//go:build hhube2e

// Package disrupt holds the acceptance proofs that mutate shared
// infrastructure (service restarts). It must run alone: the go test
// runner executes channel and lifecycle in parallel while this
// package waits its turn in the recipe.
package disrupt

import (
	"context"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"net/http"
	"testing"
	"time"
)

// TestStackRestart restarts the whole stack and proves durable state
// survives: a succeeded job still reads succeeded with its result.
//
// req: sys.stack-restart
func TestStackRestart(t *testing.T) {
	testkit.Req(t, "sys.stack-restart")
	testkit.Req(t, "adr.task-jobs.restart-durable")
	owner := "restart_" + Nonce
	space, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("link", owner), Text: "/start " + code}); err != nil {
		t.Fatal(err)
	}
	cursor := WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	res, err := TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: space, Conversation: "c1",
		Input: "restart check", Idem: Idem("j", "reba", "1")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", 3*time.Minute)
	d := testkit.NewDocker(t)
	for _, name := range []string{"task-engine", "spawn-controller",
		"identity-hub", "channel-hub", "postgres"} {
		if err := d.Restart(context.Background(), name); err != nil {
			t.Fatalf("restart %s: %v", name, err)
		}
	}
	// Wait for task-engine and channel-hub to answer again.
	for _, base := range []string{TaskURL, "http://channel-hub:9000"} {
		testkit.Poll(t, 90*time.Second, time.Second, func() bool {
			res, err := http.Get(base + "/healthz")
			if err != nil {
				return false
			}
			res.Body.Close()
			return res.StatusCode == http.StatusOK
		})
	}
	j := WaitJob(t, OwnerSpace{Owner: owner, Space: space}, res.ID, "succeeded", time.Minute)
	if j.Result != "answer: restart check" {
		t.Fatalf("lost result after restart: %q", j.Result)
	}
	_, err = TaskClient(t, OwnerSpace{Owner: owner, Space: space}).CancelJob(context.Background(), tv1.JobReq{ID: res.ID})
	testkit.Deny(t, "sys.stack-restart", herrors.CodeOf(err) == herrors.CodeConflict)
	testkit.Deny(t, "adr.task-jobs.restart-durable", herrors.CodeOf(err) == herrors.CodeConflict)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: Idem("after", owner), Text: "after restart"}); err != nil {
		t.Fatal(err)
	}
	WaitMock(t, owner, cursor, "answer: after restart", 3*time.Minute)
}
