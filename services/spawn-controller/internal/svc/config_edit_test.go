package svc

import (
	"encoding/json"
	"errors"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestReviewedConfigRejectsRetiredKey(t *testing.T) {
	t.Parallel()
	_, err := reviewedConfigPatch("harness.native", json.RawMessage(`[]`))
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("retired key was accepted for self edit: %v", err)
	}
	if _, err := parseDocs(json.RawMessage(`{"config":{"harness":{"native":[]}}}`)); err == nil {
		t.Fatal("retired key was accepted into a passport")
	}
}

func TestReviewedConfigRangeIsShared(t *testing.T) {
	t.Parallel()
	base, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	setLeaf(base, "config.timeout_ms", float64(999))
	_, compileErr := compileSet(layerSet{{role: "platform", docs: base}})
	_, editErr := reviewedConfigPatch("timeout_ms", json.RawMessage(`999`))
	for _, rejected := range []error{editErr, compileErr} {
		var e *herrors.Error
		if !errors.As(rejected, &e) || e.Key != "err.config-value" {
			t.Fatalf("out of range timeout: %v", rejected)
		}
	}
}
