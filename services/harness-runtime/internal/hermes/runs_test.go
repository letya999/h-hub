package hermes

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeToolStatusAndTerminalFailure(t *testing.T) {
	t.Parallel()
	e := nativeEvent(t, `{"event":"tool.completed","error":false}`)
	if terminal, err := e.outcome(); terminal || err != nil {
		t.Fatalf("tool completion closed the run: %v", err)
	}
	e = nativeEvent(t, `{"event":"run.failed","error":"provider refused"}`)
	if terminal, err := e.outcome(); !terminal || err == nil || !strings.Contains(err.Error(), "provider refused") {
		t.Fatalf("lost the upstream refusal: %v", err)
	}
}

func nativeEvent(t *testing.T, frame string) Event {
	t.Helper()
	var e Event
	if err := json.Unmarshal([]byte(frame), &e); err != nil {
		t.Fatal(err)
	}
	return e
}
