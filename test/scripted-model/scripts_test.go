package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolObservationRejectsAssistantText(t *testing.T) {
	t.Parallel()
	m := &model{scripts: newScriptStore()}
	m.scripts.byID["read"] = &scenario{Steps: []step{{Expect: "scenario:read todos", ToolResult: "empty", Respond: "ok"}}}
	body := `{"messages":[{"role":"user","content":"scenario:read todos"},{"role":"assistant","content":"empty"}]}`
	w := httptest.NewRecorder()
	m.completions(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("accepted a tool result from the assistant: %d", w.Code)
	}
}

func TestScenarioExactAndComplete(t *testing.T) {
	t.Parallel()
	m := &model{scripts: newScriptStore()}
	yaml := `id: scripted
steps:
  - expect: "scenario:scripted hello"
    respond: "world"
  - expect: "scenario:scripted tool"
    tool_calls:
      - name: lookup
        arguments: "{}"
  - expect: "scenario:scripted error"
    provider_error: 503
  - expect: "scenario:scripted hang"
    hang: true
`
	w := httptest.NewRecorder()
	m.loadScenario(w, httptest.NewRequest(http.MethodPost, "/v1/scenarios", strings.NewReader(yaml)))
	if w.Code != http.StatusCreated {
		t.Fatalf("load: %d %s", w.Code, w.Body.String())
	}
	verify := func() int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/scenarios/scripted/verify", nil)
		r.SetPathValue("id", "scripted")
		m.verifyScenario(w, r)
		return w.Code
	}
	if verify() != http.StatusConflict {
		t.Fatal("unused steps accepted")
	}
	for _, tc := range []struct {
		input, want string
		status      int
	}{
		{"scenario:scripted hello", "world", 200},
		{"scenario:scripted tool", "tool_calls", 200},
		{"scenario:scripted error", "scripted provider error", 503},
		{"scenario:scripted hang", "", 200},
	} {
		body := `{"model":"scripted","messages":[{"role":"user","content":"` + tc.input + `"}]}`
		ctx, cancel := context.WithCancel(context.Background())
		if strings.HasSuffix(tc.input, "hang") {
			cancel()
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		m.completions(w, r)
		cancel()
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.input, w.Code, w.Body.String())
		}
	}
	if verify() != http.StatusNoContent {
		t.Fatal("used scenario not complete")
	}
	bad := httptest.NewRecorder()
	m.completions(bad, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"scenario:scripted extra"}]}`)))
	if bad.Code != http.StatusConflict || verify() != http.StatusConflict {
		t.Fatal("unmatched request did not fail scenario")
	}
}
