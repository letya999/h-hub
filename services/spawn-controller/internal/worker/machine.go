// Package worker owns the generations and leases of isolated executors.
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/letya999/h-hub/shared/fsm"
)

const (
	kind = "worker"

	absent      = "absent"
	starting    = "starting"
	leased      = "leased"
	idle        = "idle"
	stopping    = "stopping"
	quarantined = "quarantined"

	startTTL = 30 * time.Second
	leaseTTL = 30 * time.Minute
	idleTTL  = 2 * time.Minute
	stopTTL  = 30 * time.Second
	maxAge   = 2 * time.Hour

	LaunchTopic = "worker-create"
	StopTopic   = "worker-stop"
)

func machine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{
		Kind: kind, LongLived: []string{absent},
		Edges: []fsm.Edge{
			{From: absent, Event: "claim", To: starting},
			{From: starting, Event: "ready", To: leased},
			{From: starting, Event: "stop", To: stopping},
			{From: leased, Event: "renew", To: leased},
			{From: leased, Event: "release", To: idle},
			{From: leased, Event: "stop", To: stopping},
			{From: idle, Event: "acquire", To: leased},
			{From: idle, Event: "stop", To: stopping},
			{From: stopping, Event: "observe", To: absent},
			{From: stopping, Event: "quarantine", To: quarantined},
			{From: quarantined, Event: "observe", To: absent},
			{From: quarantined, Event: "recheck", To: quarantined},
		},
		Deadlines: map[string]fsm.Deadline{
			starting:    {Event: "stop", Actor: "start-deadline", TTL: startTTL},
			leased:      {Event: "stop", Actor: "lease-deadline", TTL: leaseTTL},
			idle:        {Event: "stop", Actor: "idle-deadline", TTL: idleTTL},
			stopping:    {Event: "quarantine", Actor: "stop-deadline", TTL: stopTTL},
			quarantined: {Event: "recheck", Actor: "janitor", TTL: stopTTL},
		},
	})
	if err != nil {
		panic("worker machine: " + err.Error())
	}
	return m
}

// Sweep applies due deadlines and records cleanup work in the same transaction.
func (r *Registry) Sweep(ctx context.Context) error {
	_, err := r.machine.Sweep(ctx, r.db, r.now(), 32, deadlineEffect)
	if err != nil {
		return fmt.Errorf("sweep workers: %w", err)
	}
	return nil
}

func deadlineEffect(ctx context.Context, tx *sql.Tx, ch fsm.Change) error {
	if ch.To != stopping {
		return nil
	}
	if err := saveReason(ctx, tx, ch); err != nil {
		return err
	}
	// An effect identifies its generation so replay cannot act on a replacement.
	_, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox (kind,machine_id,topic,payload)
		SELECT kind,id,$2,convert_to(to_jsonb(runtime)::text,'UTF8') FROM intents WHERE id=$1 AND kind='worker'`,
		ch.ID, StopTopic)
	if err != nil {
		return fmt.Errorf("deadline cleanup: %w", err)
	}
	return nil
}
