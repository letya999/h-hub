//go:build hhube2e

package lifecycle

import (
	"context"
	"github.com/letya999/h-hub/cmd/hubctl/operator"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"strings"
	"testing"
	"time"
)

func TestHubctlUnblocksJobAndRuntime(t *testing.T) {
	testkit.Req(t, "sys.wedge-unblock")
	owner := "wedge_" + Nonce
	space := EnsureSpace(t, owner, Idem("wedge-space"))
	scope := OwnerSpace{Owner: owner, Space: space}
	ctx := context.Background()
	if _, err := TaskClient(t, scope).SetHold(ctx, tv1.SetHoldReq{Space: space, Active: true,
		Note: "wedge test"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := TaskClient(t, scope).SetHold(context.Background(),
			tv1.SetHoldReq{Space: space, Active: false}); err != nil {
			t.Error(err)
		}
	})
	job, err := TaskClient(t, scope).CreateJob(ctx, tv1.CreateJobReq{Space: space,
		Conversation: "c1", Input: "held", Idem: Idem("wedge-job")})
	if err != nil {
		t.Fatal(err)
	}
	if out := HubctlExit(t, "job", "cancel", "-owner", owner, "-space", space,
		"-id", job.ID); !strings.Contains(out, "cancelled") {
		t.Fatalf("cancel output: %s", out)
	}
	WaitJob(t, scope, job.ID, "cancelled", 10*time.Second)

	var runtime string
	service := TaskServiceSpawnClient(t, space)
	testkit.Poll(t, 60*time.Second, 250*time.Millisecond, func() bool {
		res, err := service.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{Space: space})
		runtime = res.Runtime
		return err == nil && res.Ready && runtime != ""
	})
	foreign, err := operator.Token("foreign-owner", space, "spawn-controller", "identity-hub", "/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	identity := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, foreign)}}
	delegated, err := identity.ActAs(ctx, idv1.ActAsReq{Owner: "foreign-owner", Space: space, Aud: "spawn-controller"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, delegated.Token)}}).DrainRuntime(ctx,
		sv1.DrainRuntimeReq{Space: space})
	testkit.Deny(t, "sys.wedge-unblock", herrors.CodeOf(err) == herrors.CodeNotFound)
	if out := HubctlExit(t, "user", "runtime-drain", owner, space); !strings.Contains(out, "draining") {
		t.Fatalf("drain output: %s", out)
	}
	db := AdminDB(t, "spawn_controller")
	d := testkit.NewDocker(t)
	testkit.Poll(t, 15*time.Second, 250*time.Millisecond, func() bool {
		var state string
		if db.QueryRowContext(ctx, `SELECT state FROM fsm.machines WHERE kind='runtime' AND id=$1`,
			runtime).Scan(&state) != nil {
			return false
		}
		cs, err := d.List(ctx, "hhub.runtime=hhub-"+runtime)
		return err == nil && state == "absent" && len(cs) == 0
	})
}
