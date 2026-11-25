package svc

import (
	"context"
	"encoding/json"
	"testing"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestCancelUnknownRun(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.harness-admit.unknown-run-refused")
	payload, err := json.Marshal(idv1.RunClaims{Run: "r", Job: "j", Space: "s", Owner: "o"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), principalKey{},
		httpapi.Principal{Aud: []string{"harness-runtime"}, Payload: payload})
	s := New("harness-runtime", Deps{})
	_, err = s.cancelRun(ctx, hv1.CancelRunReq{Run: "r"})
	testkit.Deny(t, "adr.harness-admit.unknown-run-refused", herrors.CodeOf(err) == herrors.CodeNotFound)
}

func TestToolCallArgumentsWireString(t *testing.T) {
	t.Parallel()
	wire := `{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"ask","arguments":"{\"question\":\"q\"}"}}]}`
	var msg assistantMsg
	if err := json.Unmarshal([]byte(wire), &msg); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(json.RawMessage(msg.ToolCalls[0].Function.Arguments), &parsed); err != nil {
		t.Fatalf("arguments must arrive as a json string carrying the object: %v", err)
	}
	if parsed.Question != "q" {
		t.Fatalf("question %q", parsed.Question)
	}
}
