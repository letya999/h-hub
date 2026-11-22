package svc

import (
	"time"

	"github.com/letya999/h-hub/shared/fsm"
)

// Runtime lifecycle per the brief. Warm claims idle out after 15
// minutes; every active state has a deadline and a next actor.
const (
	runtimeStartingTTL = 90 * time.Second
	runtimeWarmTTL     = 15 * time.Minute
	runtimeBusyTTL     = 11 * time.Minute
	runtimeDrainingTTL = 60 * time.Second
)

// RuntimeMachine is the runtime lifecycle:
// absent -> starting -> warm -> busy -> draining -> absent, with
// failed/quarantined/missing off the happy path.
func RuntimeMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{
		Kind:      "runtime",
		LongLived: []string{"absent", "failed", "quarantined", "missing"},
		Edges: []fsm.Edge{
			{From: "absent", Event: "claim", To: "starting"},
			{From: "missing", Event: "claim", To: "starting"},
			{From: "failed", Event: "claim", To: "starting"},
			{From: "starting", Event: "ready", To: "warm"},
			{From: "starting", Event: "fail", To: "failed"},
			{From: "starting", Event: "expire", To: "failed"},
			{From: "warm", Event: "acquire", To: "busy"},
			{From: "warm", Event: "idle", To: "draining"},
			{From: "warm", Event: "lost", To: "missing"},
			{From: "busy", Event: "release", To: "warm"},
			{From: "busy", Event: "idle", To: "draining"},
			{From: "busy", Event: "lost", To: "missing"},
			{From: "draining", Event: "reaped", To: "absent"},
			{From: "draining", Event: "expire", To: "missing"},
			{From: "warm", Event: "drain", To: "draining"},
			{From: "busy", Event: "drain", To: "draining"},
			{From: "starting", Event: "drain", To: "draining"},
			{From: "starting", Event: "lost", To: "missing"},
			{From: "warm", Event: "quarantine", To: "quarantined"},
			{From: "busy", Event: "quarantine", To: "quarantined"},
		},
		Deadlines: map[string]fsm.Deadline{
			"starting": {Event: "expire", Actor: "sweep", TTL: runtimeStartingTTL},
			"warm":     {Event: "idle", Actor: "sweep", TTL: runtimeWarmTTL},
			"busy":     {Event: "idle", Actor: "sweep", TTL: runtimeBusyTTL},
			"draining": {Event: "expire", Actor: "sweep", TTL: runtimeDrainingTTL},
		},
	})
	if err != nil {
		panic("runtime machine: " + err.Error())
	}
	return m
}
