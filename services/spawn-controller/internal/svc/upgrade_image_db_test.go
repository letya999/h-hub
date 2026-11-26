//go:build hhubdb

package svc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

func TestImageUpgradePreservesUnactivatedChanges(t *testing.T) {
	for _, tc := range []struct{ name, spaceCap, pendingCap, activeCap string }{
		{"shipped base", "", "192m", "384m"},
		{"explicit platform cap equal to the old default", "", "256m", "384m"},
		{"explicit space cap equal to the old default", "256m", "256m", "256m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, db, key := ensureSvc(t, ctx)
			f.s.deps.Hermes = "sha256:" + strings.Repeat("a", 64)
			sp := previousImageSpace(t, f.s, tc.spaceCap)
			f.s.deps.EdgeTab["spawn-controller"]["operator"] = map[string]bool{"SubmitLayers": true}
			client := drainTestClient(t, f.url, key, sp.owner, sp.id)
			doc, err := json.Marshal(map[string]any{"stack": map[string]string{"mem": tc.pendingCap}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.SubmitLayers(ctx, sv1.SubmitLayersReq{Space: sp.id, Role: "platform", Docs: doc}); err != nil {
				t.Fatal(err)
			}
			f.s.deps.Hermes = "hermes@test-release"
			for range 2 {
				if _, err := f.s.spaceForRun(ctx, sp.id); err != nil {
					t.Fatal(err)
				}
			}
			cur, err := getSpaceAny(ctx, db, sp.id)
			if err != nil {
				t.Fatal(err)
			}
			active, err := specPassports(cur)
			if err != nil || active.Stack.Image != f.s.deps.Hermes || active.Stack.Mem != tc.activeCap || cur.digest == sp.digest {
				t.Fatalf("active image/limits/digest: %+v %s %v", active.Stack, cur.digest, err)
			}
			pending, err := candidateChain(ctx, db, sp.id)
			if err != nil {
				t.Fatal(err)
			}
			c, err := compileSet(pending)
			if err != nil || c.spec.Stack.Mem != tc.pendingCap || c.spec.Stack.Image != f.s.deps.Hermes {
				t.Fatalf("pending image/limits: %+v %v", c.spec.Stack, err)
			}
		})
	}
}

func previousImageSpace(t *testing.T, s *Server, spaceCap string) spaceRow {
	t.Helper()
	ctx := context.Background()
	sp := spaceRow{id: newID("space"), owner: "image-owner", name: "test", template: "assistant"}
	chain, err := spaceLayers(s, sp.owner, s.deps.Hermes, sp.template)
	if err != nil {
		t.Fatal(err)
	}
	setLeaf(chain[0].docs, "stack.mem", "256m")
	if spaceCap != "" {
		setLeaf(chain[1].docs, "stack.mem", spaceCap)
	}
	c, err := compileSet(chain)
	if err != nil {
		t.Fatal(err)
	}
	sp.digest = c.digest
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // test rollback after commit
	if err := insertNewSpace(s, ctx, tx, sp, specApply{chain: chain, spec: c.spec, digest: c.digest,
		explain: c.explain, effect: "live", author: "create"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return sp
}
