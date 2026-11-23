//go:build hhube2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestHubctlUnblocksJobAndRuntime(t *testing.T) {
	testkit.Req(t, "sys.wedge-unblock")
	owner := "wedge_" + nonce
	space := ensureSpace(t, owner, idem("wedge-space"))
	scope := ownerSpace{owner, space}
	ctx := context.Background()
	if _, err := taskClient(t, scope).SetHold(ctx, tv1.SetHoldReq{Space: space, Active: true,
		Note: "wedge test"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := taskClient(t, scope).SetHold(context.Background(),
			tv1.SetHoldReq{Space: space, Active: false}); err != nil {
			t.Error(err)
		}
	})
	job, err := taskClient(t, scope).CreateJob(ctx, tv1.CreateJobReq{Space: space,
		Conversation: "c1", Input: "held", Idem: idem("wedge-job")})
	if err != nil {
		t.Fatal(err)
	}
	if out := hubctlExit(t, "job", "cancel", "-owner", owner, "-space", space,
		"-id", job.ID); !strings.Contains(out, "cancelled") {
		t.Fatalf("cancel output: %s", out)
	}
	waitJob(t, scope, job.ID, "cancelled", 10*time.Second)

	var runtime string
	service := taskServiceSpawnClient(t, space)
	testkit.Poll(t, 60*time.Second, 250*time.Millisecond, func() bool {
		res, err := service.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{Space: space})
		runtime = res.Runtime
		return err == nil && res.Ready && runtime != ""
	})
	foreign, err := operator.Token("foreign-owner", space, "spawn-controller", "identity-hub", "/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	identity := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, foreign)}}
	delegated, err := identity.ActAs(ctx, idv1.ActAsReq{Owner: "foreign-owner", Space: space, Aud: "spawn-controller"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, delegated.Token)}}).DrainRuntime(ctx,
		sv1.DrainRuntimeReq{Space: space})
	testkit.Deny(t, "sys.wedge-unblock", herrors.CodeOf(err) == herrors.CodeNotFound)
	if out := hubctlExit(t, "user", "runtime-drain", owner, space); !strings.Contains(out, "draining") {
		t.Fatalf("drain output: %s", out)
	}
	db := adminDB(t, "spawn_controller")
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

func taskServiceSpawnClient(t *testing.T, space string) *sv1.Client {
	t.Helper()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatal(err)
	}
	id := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, "")}}
	tok, err := id.IssueServiceToken(context.Background(), "task-engine", "spawn-controller",
		idem("wedge-spawn-token", space), key)
	if err != nil {
		t.Fatal(err)
	}
	return &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, tok.Token)}}
}

func hubctlExit(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/hubctl"}, args...)...)
	cmd.Dir = "/src"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hubctl %v: %v: %s", args, err, out)
	}
	return string(out)
}
