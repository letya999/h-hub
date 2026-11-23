//go:build hhube2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestStaleSpawnTickRefusesHealth(t *testing.T) {
	db := adminDB(t, "spawn_controller")
	result, err := db.ExecContext(context.Background(), `UPDATE fsm.watchdog_ticks
		SET at=now()-interval '1 minute' WHERE name='spawn-controller'`)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("spawn tick missing: rows=%d err=%v", n, err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `UPDATE fsm.watchdog_ticks
			SET at=now() WHERE name='spawn-controller'`); err != nil {
			t.Error(err)
		}
	})
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(spawnURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stale watchdog accepted: %d", res.StatusCode)
	}
}
