package svc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

func watchdogStatus(s *Server, ctx context.Context, _ sv1.WatchdogStatusReq) (sv1.WatchdogStatusResp, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `SELECT at FROM fsm.watchdog_ticks WHERE name=$1`, tickName).Scan(&at)
	if err == sql.ErrNoRows {
		return sv1.WatchdogStatusResp{Stale: true, Tick: "missing"}, nil
	}
	if err != nil {
		return sv1.WatchdogStatusResp{}, fmt.Errorf("watchdog status: %w", err)
	}
	return sv1.WatchdogStatusResp{Stale: s.deps.Now().Sub(at) > 15*time.Second,
		Tick: at.UTC().Format(time.RFC3339Nano)}, nil
}
