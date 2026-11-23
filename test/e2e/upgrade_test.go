//go:build hhube2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

// TestUpgradePin walks the live upgrade/rollback seam: drain the
// runtime, refuse an unreadable pin, activate the pinned digest, then
// drain and re-pin the previous digest as the rollback path.
func TestUpgradePin(t *testing.T) {
	testkit.Req(t, "adr.spawn-runtime.digest-pins-image")
	owner := "upg_" + nonce
	space := ensureSpace(t, owner, idem("upg-space"))
	ctx := context.Background()

	raw, err := operator.Token(owner, space, "spawn-controller", "identity-hub",
		"/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	identity := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, raw)}}
	delegated, err := identity.ActAs(ctx, idv1.ActAsReq{
		Owner: owner, Space: space, Aud: "spawn-controller"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(spawnURL, delegated.Token)}}
	sp, err := scoped.GetSpace(ctx, sv1.GetSpaceReq{ID: space})
	if err != nil || sp.Space.Digest == "" {
		t.Fatalf("space pin: %v %q", err, sp.Space.Digest)
	}
	pin := sp.Space.Digest

	service := taskServiceSpawnClient(t, space)
	_, err = service.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{
		Space: space, Digest: "sha256:not-a-real-pin"})
	testkit.Deny(t, "adr.spawn-runtime.digest-pins-image",
		herrors.CodeOf(err) == herrors.CodeStale)

	// Pin activation: the recorded digest starts the runtime (this is
	// also the new-image smoke — the container must reach warm).
	testkit.Poll(t, 60*time.Second, 250*time.Millisecond, func() bool {
		res, err := service.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{
			Space: space, Digest: pin})
		return err == nil && res.Ready
	})
	// Drain for the upgrade swap; a drained runtime refuses a blind
	// re-activate — rollback goes through the operator script, not a
	// quiet re-ensure.
	if _, err := scoped.DrainRuntime(ctx, sv1.DrainRuntimeReq{Space: space}); err != nil {
		t.Fatal(err)
	}
	_, err = service.EnsureRuntime(ctx, sv1.EnsureRuntimeReq{
		Space: space, Digest: pin})
	testkit.Deny(t, "adr.spawn-runtime.digest-pins-image",
		herrors.CodeOf(err) == herrors.CodeConflict)
}
