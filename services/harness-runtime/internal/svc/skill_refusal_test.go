package svc

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
)

func TestToolRefusalPreservesScannerCoordinates(t *testing.T) {
	t.Parallel()
	wireMessage := locale.T(locale.EN, "err.skill-scan")
	refusal := herrors.New(herrors.CodeInvalid, wireMessage).
		With("file", "notes.md").With("line", float64(2)).With("rule", "secret-like").
		With("internal", "excluded detail")
	response := httptest.NewRecorder()
	if err := writeToolRefusal(response, refusal); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	d := result.Details
	if len(d) != 3 || d["file"] != "notes.md" || d["line"] != float64(2) || d["rule"] != "secret-like" {
		t.Fatal("tool refusal must preserve only the scanner coordinates")
	}
}
