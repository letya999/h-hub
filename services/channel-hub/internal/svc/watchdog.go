package svc

import (
	"context"
	"fmt"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
)

func checkSpawnWatchdog(s *Server, ctx context.Context) error {
	client, err := spawnClient(s, ctx)
	if err != nil {
		return err
	}
	status, err := client.WatchdogStatus(ctx, sv1.WatchdogStatusReq{})
	if err != nil {
		return fmt.Errorf("spawn status: %w", err)
	}
	if !status.Stale {
		return nil
	}
	_, err = (&alertOps{s}).alert(ctx, cv1.AlertReq{Key: "spawn-watchdog-" + status.Tick,
		Code: "watchdog-stale"})
	return err
}
