package svc

import (
	"time"

	"github.com/letya999/h-hub/shared/fsm"
)

func principalMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{Kind: "principal", LongLived: []string{"active"}, Final: []string{"disabled"},
		Edges: []fsm.Edge{{From: "active", Event: "disable", To: "disabled"}}})
	if err != nil {
		panic(err)
	}
	return m
}

func linkMachine() *fsm.Machine {
	m, err := fsm.New(fsm.Spec{Kind: "link_code", Final: []string{"used", "expired"},
		Edges:     []fsm.Edge{{From: "pending", Event: "use", To: "used"}, {From: "pending", Event: "expire", To: "expired"}},
		Deadlines: map[string]fsm.Deadline{"pending": {Event: "expire", Actor: "identity-hub", TTL: 24 * time.Hour}}})
	if err != nil {
		panic(err)
	}
	return m
}
