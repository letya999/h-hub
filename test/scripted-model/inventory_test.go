package main

import "testing"

func TestInventoryRequiresExplicitEmptyList(t *testing.T) {
	t.Parallel()
	if !inventoryMatches("Space\n\nAvailable tools: []", nil) {
		t.Fatal("a declared empty tool list must be visible")
	}
	if inventoryMatches("Space", nil) || inventoryMatches("Space\n\nAvailable tools: null", nil) {
		t.Fatal("a missing tool list was counted as a verified empty list")
	}
}
