package svc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
	"github.com/letya999/h-hub/shared/httpapi"
)

func TestNestedToolSurface(t *testing.T) {
	t.Parallel()
	const todo = `{"tools":[{"type":"function","function":{"name":"todo_list"}}]}`
	const extra = `{"tools":[{"type":"function","function":{"name":"unknown_tool"}}]}`
	for _, tc := range []struct {
		name, request, responseTool, refusal string
		startup, forwarded                   bool
	}{
		{"child may use todo", todo, "todo_list", "", false, true},
		{"parent-only tool is absent for the child", todo, "delegate_task", "model requested an unprojected tool", false, true},
		{"child can converse without any tools", "{}", "", "", false, true},
		{"missing startup tool", todo, "", "missing a projected tool", true, false},
		{"extra runtime tool", extra, "", "unexpected Hermes tool", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, reply, forwarded := nestedProvider(t, tc.responseTool)
			a := &Admission{Run: "run", Token: "service", ctx: t.Context(),
				allowed: map[string]string{"delegate_task": "", "todo_list": ""}}
			if tc.startup {
				a.probe = make(chan error, 1)
			}
			result := httptest.NewRecorder()
			err := s.serveModel(result, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.request)), a)
			if forwarded.Load() != tc.forwarded {
				t.Fatalf("provider received the request: %t", forwarded.Load())
			}
			checkNestedCompletion(t, result.Body.String(), err, reply, tc.refusal)
		})
	}
}

func nestedProvider(t *testing.T, tool string) (*Server, string, *atomic.Bool) {
	t.Helper()
	reply := `{"choices":[{"message":{"content":"ok"}}]}`
	if tool != "" {
		reply = fmt.Sprintf(`{"choices":[{"message":{"tool_calls":[{"function":{"name":%q}}]}}]}`, tool)
	}
	var forwarded atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Store(true)
		httpapi.WriteJSON(w, mv1.CompleteResp{Response: json.RawMessage(reply)})
	}))
	t.Cleanup(provider.Close)
	return New("harness-runtime", Deps{Rendered: config.Rendered{ModelURL: provider.URL, TimeoutMS: 5000}}), reply, &forwarded
}

func checkNestedCompletion(t *testing.T, body string, err error, reply, refusal string) {
	t.Helper()
	if refusal != "" {
		if err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("expected %q, got %v", refusal, err)
		}
		return
	}
	if err != nil || body != reply {
		t.Fatalf("child completion %q, error %v", body, err)
	}
}
