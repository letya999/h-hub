package main

import (
	"path/filepath"
	"testing"

	"github.com/letya999/h-hub/internal/descriptor"
	"github.com/letya999/h-hub/shared/edges"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRenderEdges(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.service-edges.rendered-table")
	f, err := descriptor.Load(filepath.Join("..", "..", "deploy", "descriptor.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := edges.Parse(renderEdges(f))
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range [][3]string{
		{"identity-hub", "operator", "ActAs"},
		{"task-engine", "operator", "CreateJob"},
		{"spawn-controller", "operator", "GetSpace"},
	} {
		if !tab.Allows(edge[0], edge[1], edge[2]) {
			t.Fatalf("missing rendered edge %v", edge)
		}
	}
	testkit.Deny(t, "adr.service-edges.rendered-table",
		!tab.Allows("task-engine", "operator", "ClaimNext"))
}
