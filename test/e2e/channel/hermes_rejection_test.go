//go:build hhube2e

package channel

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestHermesRejectsRunawayResponse(t *testing.T) {
	t.Parallel()
	owner := "runaway_" + Nonce
	scope := OwnerSpace{Owner: owner, Space: EnsureSpace(t, owner, Idem("runaway"))}
	input := "scenario:" + owner + " long"
	output := "<script>alert(1)</script>" + strings.Repeat("Ж", 21000)
	PostScenario(t, fmt.Sprintf("id: %s\nsteps:\n  - expect: %s\n    respond: %s\n",
		owner, strconv.Quote(input), strconv.Quote(output)), http.StatusCreated)
	job, err := TaskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: scope.Space, Conversation: "runaway", Input: input, Idem: Idem(owner, "long")})
	if err != nil {
		t.Fatal(err)
	}
	got := WaitJob(t, scope, job.ID, "failed", 3*time.Minute)
	if !strings.Contains(got.Result, "run.failed") || !strings.Contains(got.Result, "repetition loop") {
		t.Fatalf("missing upstream failure reason: %q", got.Result)
	}
}
