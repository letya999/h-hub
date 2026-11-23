//go:build hhube2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestOrphanRuntimeRemoved(t *testing.T) {
	t.Parallel()
	d := testkit.NewDocker(t)
	name := "hhub-rt-orphan-" + nonce
	id, err := d.CreateRun(context.Background(), testkit.CreateSpec{
		Image: "hhub-task-engine:local", Name: name,
		Labels:   map[string]string{"hhub.managed": "true", "hhub.runtime": name},
		Networks: []string{"hhub"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		found, err := d.List(context.Background(), "hhub.runtime="+name)
		if err != nil {
			t.Error(err)
		} else if len(found) > 0 {
			if err := d.Remove(context.Background(), id); err != nil {
				t.Error(err)
			}
		}
	})
	testkit.Poll(t, 10*time.Second, 250*time.Millisecond, func() bool {
		found, err := d.List(context.Background(), "hhub.runtime="+name)
		return err == nil && len(found) == 0
	})
}
