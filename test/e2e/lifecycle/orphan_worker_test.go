//go:build hhube2e

package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestOrphanWorkerRemoved(t *testing.T) {
	t.Parallel()
	d := testkit.NewDocker(t)
	key := "worker-orphan-" + Nonce
	generation := "test-" + Nonce
	id, err := d.CreateRun(t.Context(), testkit.CreateSpec{
		Image: "hhub-task-engine:local", Name: "orphan-" + generation, Networks: []string{"none"},
		Labels: map[string]string{
			"hhub.worker": key, "hhub.generation": generation, "hhub.installation": "hhub-space",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		found, err := d.List(ctx, "hhub.generation="+generation)
		if err != nil {
			t.Error(err)
		} else if len(found) > 0 {
			if err := d.Remove(ctx, id); err != nil {
				t.Error(err)
			}
		}
	})
	testkit.Poll(t, 10*time.Second, 250*time.Millisecond, func() bool {
		found, err := d.List(t.Context(), "hhub.generation="+generation)
		return err == nil && len(found) == 0
	})
}
