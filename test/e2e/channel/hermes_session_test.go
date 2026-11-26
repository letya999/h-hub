//go:build hhube2e

package channel

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestHermesConversationAfterRespawn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "continuity_" + Nonce
	space, code := AddUser(t, owner)
	after := int64(0)
	turn := func(input, answer string) {
		t.Helper()
		started := time.Now()
		_, err := MockClient(t, owner, "Inject").Inject(ctx, cv1.InjectReq{
			External: owner, UpdateID: Idem(owner, strconv.FormatInt(after, 10)), Text: input})
		if err != nil {
			t.Fatal(err)
		}
		after = WaitMock(t, owner, after, answer, 3*time.Minute)
		t.Logf("channel delivery in %s", time.Since(started))
	}
	turn("/start "+code, "Account linked")
	first, second := Idem("apricot"), Idem("plum")
	turn("remember marker "+first, "answer: remember marker "+first)
	turn("remember marker "+second, "answer: remember marker "+second)
	container := func() string {
		return strings.TrimSpace(string(DockerOutput(t, "ps", "-q", "--filter", "label=hhub.space="+space)))
	}
	old := container()
	if old == "" {
		t.Fatal("first Hermes container missing")
	}
	if err := testkit.NewDocker(t).Remove(ctx, old); err != nil {
		t.Fatal(err)
	}
	db := AdminDB(t, "spawn_controller")
	testkit.Poll(t, 30*time.Second, 250*time.Millisecond, func() bool {
		var lost bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtimes r JOIN fsm.machines m
			ON m.id=r.id AND m.kind='runtime' WHERE r.space=$1 AND m.state NOT IN ('warm','busy','starting'))`, space).Scan(&lost)
		return err == nil && lost
	})
	turn("recall my markers", "answer: markers "+first+"/"+second)
	if current := container(); current == "" || current == old {
		t.Fatalf("expected a new Hermes container, got %q after %q", current, old)
	}
}
