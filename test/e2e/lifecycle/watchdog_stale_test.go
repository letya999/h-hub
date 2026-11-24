//go:build hhube2e

package lifecycle

import (
	"context"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"net/http"
	"testing"
	"time"
)

func TestStaleSpawnTickRefusesHealth(t *testing.T) {
	testkit.Req(t, "sys.watchdog-stale")
	db := AdminDB(t, "spawn_controller")
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE FUNCTION fsm.freeze_spawn_tick() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN IF NEW.name='spawn-controller' THEN
		NEW.at='2020-01-01T00:00:00Z'; END IF; RETURN NEW; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS freeze_spawn_tick ON fsm.watchdog_ticks`); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(ctx, `DROP FUNCTION IF EXISTS fsm.freeze_spawn_tick()`); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE fsm.watchdog_ticks SET at=now() WHERE name='spawn-controller'`); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER freeze_spawn_tick BEFORE INSERT OR UPDATE
		ON fsm.watchdog_ticks FOR EACH ROW EXECUTE FUNCTION fsm.freeze_spawn_tick()`); err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(ctx, `UPDATE fsm.watchdog_ticks SET at=now() WHERE name='spawn-controller'`)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("spawn tick missing: rows=%d err=%v", n, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(SpawnURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	testkit.Deny(t, "sys.watchdog-stale", res.StatusCode == http.StatusServiceUnavailable)
	_, err = (&sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(SpawnURL, "")}}).WatchdogStatus(ctx,
		sv1.WatchdogStatusReq{})
	testkit.Deny(t, "sys.watchdog-stale", herrors.CodeOf(err) == herrors.CodeUnauthorized)
	WaitOperatorDelivery(t, "spawn-watchdog-2020-01-01T00:00:00Z", "Spawn controller watchdog is stale")
}
