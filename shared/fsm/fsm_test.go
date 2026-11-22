package fsm

import (
	"testing"
	"time"
)

func testSpec() Spec {
	return Spec{
		Kind:      "test-machine",
		Final:     []string{"done", "failed"},
		LongLived: []string{"active"},
		Edges: []Edge{
			{From: "queued", Event: "start", To: "active"},
			{From: "queued", Event: "timeout", To: "failed"},
			{From: "active", Event: "finish", To: "done"},
			{From: "active", Event: "fail", To: "failed"},
		},
		Deadlines: map[string]Deadline{
			"queued": {Event: "timeout", Actor: "sweeper", TTL: time.Minute},
		},
	}
}

func TestSpecValidation(t *testing.T) {
	t.Parallel()
	if _, err := New(testSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Spec)
	}{
		{"no kind", func(s *Spec) { s.Kind = "" }},
		{"duplicate edge", func(s *Spec) {
			s.Edges = append(s.Edges, Edge{From: "queued", Event: "start", To: "done"})
		}},
		{"edge out of final", func(s *Spec) {
			s.Edges = append(s.Edges, Edge{From: "done", Event: "again", To: "queued"})
		}},
		{"state without deadline", func(s *Spec) {
			s.Edges = append(s.Edges, Edge{From: "active", Event: "pause", To: "paused"})
		}},
		{"deadline on final", func(s *Spec) {
			s.Deadlines["done"] = Deadline{Event: "x", Actor: "a", TTL: time.Second}
		}},
		{"deadline on long-lived", func(s *Spec) {
			s.Deadlines["active"] = Deadline{Event: "x", Actor: "a", TTL: time.Second}
		}},
		{"deadline without edge", func(s *Spec) {
			s.Deadlines["queued"] = Deadline{Event: "nonexistent", Actor: "a", TTL: time.Second}
		}},
		{"bad ttl", func(s *Spec) {
			s.Deadlines["queued"] = Deadline{Event: "timeout", Actor: "a", TTL: 0}
		}},
		{"unsafe schema", func(s *Spec) { s.Schema = "a;drop" }},
		{"unknown deadline edge target", func(s *Spec) {
			s.Edges = append(s.Edges, Edge{From: "active", Event: "ghost", To: "void"})
		}},
	}
	for _, c := range cases {
		s := testSpec()
		c.mut(&s)
		if _, err := New(s); err == nil {
			t.Errorf("%s: spec must be rejected", c.name)
		}
	}
}
