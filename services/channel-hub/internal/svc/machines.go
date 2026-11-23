package svc

import (
	"time"

	"github.com/letya999/h-hub/shared/fsm"
)

func intakeMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{Kind: "intake", Final: []string{"completed", "failed"},
		Edges: []fsm.Edge{{From: "queued", Event: "accept", To: "processing"},
			{From: "queued", Event: "timeout", To: "failed"},
			{From: "processing", Event: "photo-fetch", To: "processing"},
			{From: "processing", Event: "wait", To: "waiting"},
			{From: "processing", Event: "complete", To: "completed"},
			{From: "processing", Event: "timeout", To: "failed"},
			{From: "waiting", Event: "complete", To: "completed"},
			{From: "waiting", Event: "timeout", To: "failed"}},
		Deadlines: map[string]fsm.Deadline{
			"queued":     {Event: "timeout", Actor: "channel-hub", TTL: time.Minute},
			"processing": {Event: "timeout", Actor: "channel-hub", TTL: 2 * time.Minute},
			"waiting":    {Event: "timeout", Actor: "channel-hub", TTL: 10 * time.Minute}}})
	if err != nil {
		panic(err)
	}
	return m
}

func deliveryMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{Kind: "delivery", Final: []string{"sent", "failed", "ambiguous", "undeliverable"},
		Edges: []fsm.Edge{{From: "queued", Event: "send", To: "sending"},
			{From: "queued", Event: "timeout", To: "failed"},
			{From: "sending", Event: "ack", To: "sent"},
			{From: "sending", Event: "uncertain", To: "ambiguous"},
			{From: "sending", Event: "reject", To: "undeliverable"},
			{From: "sending", Event: "timeout", To: "ambiguous"}},
		Deadlines: map[string]fsm.Deadline{
			"queued":  {Event: "timeout", Actor: "channel-hub", TTL: time.Minute},
			"sending": {Event: "timeout", Actor: "channel-hub", TTL: time.Minute}}})
	if err != nil {
		panic(err)
	}
	return m
}

func threadMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{Kind: "mock_thread", LongLived: []string{"active"},
		Edges: []fsm.Edge{{From: "active", Event: "new", To: "active"}}})
	if err != nil {
		panic(err)
	}
	return m
}
