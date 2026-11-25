package svc

import (
	"time"

	"github.com/letya999/h-hub/shared/fsm"
)

// Job and run state per the phase-3 brief. Every non-final state has
// a deadline and a next actor; uncertain is observed for 15 minutes
// then quarantined — it is never retried automatically.
const (
	jobQueuedTTL    = 30 * time.Minute
	jobAdmittedTTL  = 2 * time.Minute
	jobRunningTTL   = 10 * time.Minute
	jobUncertainTTL = 15 * time.Minute
	// At entry, waiting also inherits the earlier job or request deadline.
	jobWaitingTTL = 6 * time.Minute

	runDispatchedTTL = 30 * time.Second
	// streaming is a stall deadline, not a turn budget: progress
	// events from the runtime re-arm it via Touch, so ~4 missed
	// heartbeats (10s each) settle the run.
	runStreamingTTL = 45 * time.Second

	// Ceilings on admission.
	maxOwnerLive  = 32 // non-final jobs per owner
	maxGlobalLive = 64 // running machines cluster-wide
)

// JobMachine is the job lifecycle: queued -> admitted -> running ->
// settled, with uncertain/cancelled/quarantined off the happy path.
// failed and quarantined are long-lived: they wait indefinitely for an
// operator's retry or unquarantine, so no deadline arms them.
func JobMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{
		Kind:      "job",
		Final:     []string{"succeeded", "cancelled"},
		LongLived: []string{"failed", "quarantined"},
		Edges: []fsm.Edge{
			{From: "queued", Event: "admit", To: "admitted"},
			{From: "queued", Event: "cancel", To: "cancelled"},
			{From: "queued", Event: "stale", To: "quarantined"},
			{From: "admitted", Event: "dispatch", To: "running"},
			{From: "admitted", Event: "cancel", To: "cancelled"},
			{From: "admitted", Event: "lost", To: "uncertain"},
			{From: "running", Event: "settle-ok", To: "succeeded"},
			{From: "running", Event: "settle-fail", To: "failed"},
			{From: "running", Event: "cancel", To: "cancelled"},
			{From: "running", Event: "lost", To: "uncertain"},
			{From: "running", Event: "wait-owner", To: "waiting_owner"},
			{From: "waiting_owner", Event: "wait-owner", To: "waiting_owner"},
			{From: "waiting_owner", Event: "resume", To: "queued"},
			{From: "waiting_owner", Event: "settle-fail", To: "failed"},
			{From: "waiting_owner", Event: "expire", To: "failed"},
			{From: "waiting_owner", Event: "cancel", To: "cancelled"},
			{From: "uncertain", Event: "settle-ok", To: "succeeded"},
			{From: "uncertain", Event: "settle-fail", To: "failed"},
			{From: "uncertain", Event: "cancel", To: "cancelled"},
			{From: "uncertain", Event: "expire", To: "quarantined"},
			{From: "failed", Event: "retry", To: "queued"},
			{From: "quarantined", Event: "unquarantine", To: "queued"},
		},
		Deadlines: map[string]fsm.Deadline{
			"queued":        {Event: "stale", Actor: "sweep", TTL: jobQueuedTTL},
			"admitted":      {Event: "lost", Actor: "sweep", TTL: jobAdmittedTTL},
			"running":       {Event: "lost", Actor: "sweep", TTL: jobRunningTTL},
			"uncertain":     {Event: "expire", Actor: "sweep", TTL: jobUncertainTTL},
			"waiting_owner": {Event: "expire", Actor: "sweep", TTL: jobWaitingTTL},
		},
	})
	if err != nil {
		panic("job machine: " + err.Error())
	}
	return m
}

// RunMachine is the run lifecycle: dispatched -> streaming ->
// settled. A stream-start deadline bounds dispatch; a stream deadline
// bounds the turn.
func RunMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{
		Kind:  "run",
		Final: []string{"settled"},
		Edges: []fsm.Edge{
			{From: "dispatched", Event: "stream-start", To: "streaming"},
			{From: "dispatched", Event: "stream-timeout", To: "settled"},
			{From: "dispatched", Event: "settle", To: "settled"},
			{From: "streaming", Event: "stream-timeout", To: "settled"},
			{From: "streaming", Event: "settle", To: "settled"},
		},
		Deadlines: map[string]fsm.Deadline{
			"dispatched": {Event: "stream-timeout", Actor: "sweep", TTL: runDispatchedTTL},
			"streaming":  {Event: "stream-timeout", Actor: "sweep", TTL: runStreamingTTL},
		},
	})
	if err != nil {
		panic("run machine: " + err.Error())
	}
	return m
}
