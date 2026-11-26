package svc

import (
	"errors"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestCompileNativeExecutorRequiresOwnerAcceptance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool, effect string
	}{
		{"show_tip", "nothing"},
		{"web_search", "network"},
		{"terminal", "process"},
		{"discord", "external-service"},
	} {
		t.Run(tc.effect, func(t *testing.T) {
			t.Parallel()
			checkExecutorAcceptance(t, tc.tool, tc.effect)
		})
	}
}

func checkExecutorAcceptance(t *testing.T, tool, effect string) {
	t.Helper()
	base, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	setLeaf(base, "tools.core", []string{tool})
	choice := map[string]any{"executor": "harness"}
	setLeaf(base, "tools.executors", map[string]any{tool: choice})
	chain := layerSet{{role: "platform", docs: base}}
	_, err = compileSet(chain)
	if effect == "nothing" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var rejected *herrors.Error
	if !errors.As(err, &rejected) || rejected.Key != "err.native-limits" ||
		rejected.Details["tool"] != tool || rejected.Details["effect"] != effect {
		t.Fatalf("unacknowledged %s assignment did not fail for its effect: %v", tool, err)
	}
	choice["accept_native_limits"] = true
	if _, err := compileSet(chain); err != nil {
		t.Fatalf("owner acceptance cannot compile: %v", err)
	}
}
