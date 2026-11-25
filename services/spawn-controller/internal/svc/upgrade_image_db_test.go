//go:build hhubdb

package svc

import (
	"context"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

func TestImageUpgradePreservesUnactivatedChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, db, key := ensureSvc(t, ctx)
	f.s.deps.EdgeTab["spawn-controller"]["operator"] = map[string]bool{"CreateSpace": true}
	f.s.deps.Hermes = "sha256:" + strings.Repeat("a", 64)
	client := drainTestClient(t, f.url, key, "image-owner", "")
	sp, err := client.CreateSpace(ctx, sv1.CreateSpaceReq{Name: "test", Template: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // closing an already committed transaction
	patch := DocSet{}
	setLeaf(patch, "stack.mem", "192m")
	if _, _, err := storeRevision(f.s, ctx, tx, revRow{space: sp.ID, role: "platform",
		author: "operator", at: f.s.deps.Now()}, patch); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	f.s.deps.Hermes = "hermes@test-release"
	for range 2 {
		if _, err := f.s.spaceForRun(ctx, sp.ID); err != nil {
			t.Fatal(err)
		}
	}
	cur, err := getSpaceAny(ctx, db, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := specPassports(cur)
	if err != nil || active.Stack.Image != f.s.deps.Hermes || active.Stack.Mem != "256m" || cur.digest == sp.Digest {
		t.Fatalf("active image/limits/digest: %+v %s %v", active.Stack, cur.digest, err)
	}
	pending, err := candidateChain(ctx, db, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := compileSet(pending)
	if err != nil || c.spec.Stack.Mem != "192m" || c.spec.Stack.Image != f.s.deps.Hermes {
		t.Fatalf("pending image/limits: %+v %v", c.spec.Stack, err)
	}
}
