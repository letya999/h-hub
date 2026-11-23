//go:build hhube2e

package lifecycle

import (
	"context"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStoreMissingDatabaseRefusesStartup(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.store-missing")
	db := "hhub_missing_" + Nonce
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "hhub",
		"-e", "HHUB_SUB=task-engine",
		"-e", "HHUB_DSN=postgres://task_engine@postgres:5432/"+db+"?sslmode=disable",
		"-e", "HHUB_KEYS=/etc/hhub",
		"-e", "HHUB_IDENTITY_URL=http://identity-hub:9000",
		"-e", "HHUB_SPAWN_URL=http://spawn-controller:9000",
		"-e", "HHUB_CHANNEL_URL=http://channel-hub:9000",
		"hhub-task-engine:local")
	out, err := cmd.CombinedOutput()
	testkit.Deny(t, "sys.store-missing", ctx.Err() == nil && err != nil &&
		strings.Contains(string(out), db) && strings.Contains(string(out), "store:"))
}
