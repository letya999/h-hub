//go:build hhube2e

// Passport layer proofs (phase 8): narrowing refusal, preview/confirm
// CAS, the three effect classes on real containers, rollback through
// the same checks, and stored explain attribution.
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	mtv1 "github.com/letya999/h-hub/contracts/model/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

// submit writes one user-layer revision for the space.
func submit(t *testing.T, scope OwnerSpace, docs string) {
	t.Helper()
	_, err := SpawnClient(t, scope).SubmitLayers(context.Background(),
		sv1.SubmitLayersReq{Space: scope.Space, Role: "user",
			Docs: json.RawMessage(docs)})
	if err != nil {
		t.Fatalf("submit layers: %v", err)
	}
}

// apply previews and activates the candidate, returning both.
func apply(t *testing.T, scope OwnerSpace) (sv1.PreviewResp, sv1.ActivateResp) {
	t.Helper()
	c := SpawnClient(t, scope)
	prev, err := c.Preview(context.Background(),
		sv1.PreviewReq{Space: scope.Space})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	act, err := c.Activate(context.Background(), sv1.ActivateReq{
		Space: scope.Space, Base: prev.Base, Candidate: prev.Candidate})
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	return prev, act
}

// warmRuntime runs one job so a container exists for the space.
func warmRuntime(t *testing.T, scope OwnerSpace) string {
	t.Helper()
	res, err := TaskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: scope.Space, Conversation: "c1",
		Input: "hi " + Idem("w"), Idem: Idem("j")})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	WaitJob(t, scope, res.ID, "succeeded", 3*time.Minute)
	db := AdminDB(t, "spawn_controller")
	var rt, cid string
	if err := db.QueryRow(`SELECT id, container FROM runtimes
		WHERE space=$1`, scope.Space).Scan(&rt, &cid); err != nil {
		t.Fatalf("runtime row: %v", err)
	}
	return cid
}

func TestPassportNarrowRefusal(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-narrow")
	owner, space := "narrow", EnsureSpace(t, "narrow", Idem("narrow"))
	c := SpawnClient(t, OwnerSpace{Owner: owner, Space: space})
	_, err := c.SubmitLayers(context.Background(), sv1.SubmitLayersReq{
		Space: space, Role: "user",
		Docs: json.RawMessage(`{"grants":{"allow":["tools.use","ssh.exec"]}}`)})
	testkit.Deny(t, "sys.passport-narrow",
		err != nil && herrors.CodeOf(err) == herrors.CodeInvalid)
	_, err = c.SubmitLayers(context.Background(), sv1.SubmitLayersReq{
		Space: space, Role: "user",
		Docs: json.RawMessage(`{"stack":{"mem":"512m"}}`)})
	testkit.Deny(t, "sys.passport-narrow",
		err != nil && herrors.CodeOf(err) == herrors.CodeInvalid)
}

func TestPassportConfirmStaleRefused(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-activate")
	owner, space := "stale", EnsureSpace(t, "stale", Idem("stale"))
	scope := OwnerSpace{Owner: owner, Space: space}
	c := SpawnClient(t, scope)
	submit(t, scope, `{"models":{"default":"scripted-alt"}}`)
	prev, err := c.Preview(context.Background(),
		sv1.PreviewReq{Space: space})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	submit(t, scope, `{"models":{"default":"scripted-alt"},
		"config":{"timeout_ms":60000}}`)
	_, err = c.Activate(context.Background(), sv1.ActivateReq{
		Space: space, Base: prev.Base, Candidate: prev.Candidate})
	testkit.Deny(t, "sys.passport-activate",
		err != nil && herrors.CodeOf(err) == herrors.CodeStale)
}

func TestPassportLiveAppliesWithoutRestart(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-live", "sys.passport-explain")
	owner, space := "live", EnsureSpace(t, "live", Idem("live"))
	scope := OwnerSpace{Owner: owner, Space: space}
	cid := warmRuntime(t, scope)
	submit(t, scope, `{"models":{"default":"scripted-alt"}}`)
	prev, act := apply(t, scope)
	if act.Digest == prev.Base {
		t.Fatalf("digest did not change: %s", act.Digest)
	}
	rt, err := ModelClient(t, scope).Routes(context.Background(), mtv1.RoutesReq{})
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	if rt.Default != "scripted-alt" {
		t.Fatalf("live leaf not consumed: %s", rt.Default)
	}
	ex, err := SpawnClient(t, scope).Explain(context.Background(),
		sv1.ExplainReq{Space: space})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	testkit.Deny(t, "sys.passport-explain", ex.Layers["models.default"] == "user")
	_, execErr := ExecDocker(cid, "true")
	testkit.Deny(t, "sys.passport-live", act.Effect == "live" && execErr == nil)
}

func TestPassportRespawnRestartsRuntime(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-respawn")
	owner, space := "respawn", EnsureSpace(t, "respawn", Idem("respawn"))
	scope := OwnerSpace{Owner: owner, Space: space}
	cid := warmRuntime(t, scope)
	submit(t, scope, `{"config":{"timeout_ms":60000}}`)
	_, act := apply(t, scope)
	if act.Effect != "respawn" {
		t.Fatalf("effect %s", act.Effect)
	}
	testkit.Poll(t, 90*time.Second, time.Second, func() bool {
		_, err := ExecDocker(cid, "true")
		return err != nil
	})
	_, execErr := ExecDocker(cid, "true")
	testkit.Deny(t, "sys.passport-respawn", execErr != nil)
}

func TestPassportRecreateRebuildsWorld(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-recreate")
	owner, space := "recreate", EnsureSpace(t, "recreate", Idem("recreate"))
	scope := OwnerSpace{Owner: owner, Space: space}
	submit(t, scope, `{"workspace":{"files":{"soul.md":"# v2\nrecreate me\n"}}}`)
	_, act := apply(t, scope)
	if act.Effect != "recreate" {
		t.Fatalf("effect %s", act.Effect)
	}
	cid := warmRuntime(t, scope)
	out, err := ExecDocker(cid, "cat",
		fmt.Sprintf("/etc/hhub/render/%s/files/soul.md", space))
	if err != nil {
		t.Fatalf("read workspace file: %v: %s", err, out)
	}
	testkit.Deny(t, "sys.passport-recreate", string(out) == "# v2\nrecreate me\n")
}

func TestPassportRollbackRestoresDigest(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.passport-rollback")
	owner, space := "rollback", EnsureSpace(t, "rollback", Idem("rollback"))
	scope := OwnerSpace{Owner: owner, Space: space}
	c := SpawnClient(t, scope)
	info, err := c.GetSpace(context.Background(), sv1.GetSpaceReq{ID: space})
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	submit(t, scope, `{"models":{"default":"scripted-alt"}}`)
	_, act := apply(t, scope)
	if act.Digest == info.Space.Digest {
		t.Fatal("activation did not change the digest")
	}
	rb, err := c.Rollback(context.Background(),
		sv1.RollbackReq{Space: space, Digest: info.Space.Digest})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rb.Digest != info.Space.Digest {
		t.Fatalf("rollback digest %s want %s", rb.Digest, info.Space.Digest)
	}
	after, err := c.GetSpace(context.Background(), sv1.GetSpaceReq{ID: space})
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	if after.Space.Digest != info.Space.Digest {
		t.Fatalf("digest after rollback: %s", after.Space.Digest)
	}
	// A digest that was never activated must be refused: the stored
	// chain either does not exist or recompiles to a different digest.
	_, err = c.Rollback(context.Background(), sv1.RollbackReq{
		Space: space, Digest: act.Digest + "0"})
	testkit.Deny(t, "sys.passport-rollback", err != nil)
}
