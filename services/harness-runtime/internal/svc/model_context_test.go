package svc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
	"github.com/letya999/h-hub/shared/httpapi"
)

func TestModelReceivesOnlyCurrentToolNamesAndConversation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, tools, inventory string }{
		{"empty passport", "[]", "[]"},
		{"narrower child", `[{"type":"function","function":{"name":"todo_list"}}]`, `["todo_list"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"instructions":"internal-control-marker","metadata":{"internal":"host-marker"},"messages":[
				{"role":"system","content":"host-marker /mnt/c upstream-profile"},
				{"role":"developer","content":"internal-control-marker"},
				{"role":"user","content":[{"type":"text","text":"my own /mnt/c note"}]},
				{"role":"assistant","reasoning_content":"internal-control-marker","tool_calls":[{"id":"call-1","type":"function","function":{"name":"todo_list","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"call-1","content":"a previous result"}],"tools":` + tc.tools + `}`
			observed := make(chan json.RawMessage, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var call mv1.CompleteReq
				if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
					t.Error(err)
				}
				observed <- call.Request
				httpapi.WriteJSON(w, mv1.CompleteResp{Response: json.RawMessage(`{"choices":[{"message":{"content":"ok"}}]}`)})
			}))
			t.Cleanup(provider.Close)
			s := New("runtime", Deps{Rendered: config.Rendered{ModelURL: provider.URL, TimeoutMS: 5000}})
			a := &Admission{Run: "run", Token: "scoped", ctx: t.Context(), allowed: map[string]string{"delegate_task": "", "todo_list": ""}}
			if err := s.serveModel(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), a); err != nil {
				t.Fatal(err)
			}
			select {
			case raw := <-observed:
				checkModelContext(t, raw, body, tc.inventory)
			default:
				t.Fatal("admitted conversation never reached the provider")
			}
		})
	}
}

func checkModelContext(t *testing.T, raw json.RawMessage, original, inventory string) {
	t.Helper()
	for _, private := range []string{"host-marker", "internal-control-marker", "upstream-profile", "delegate_task"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("provider received private context %q", private)
		}
	}
	var got, before struct{ Messages []map[string]any }
	if err := json.Unmarshal(raw, &got); err != nil || len(got.Messages) != 4 {
		t.Fatalf("provider messages: count=%d, err=%v", len(got.Messages), err)
	}
	system, ok := got.Messages[0]["content"].(string)
	if !ok || got.Messages[0]["role"] != "system" || !strings.Contains(system, "Available tools: "+inventory) {
		t.Fatalf("provider system message lacks the current inventory %s: %q", inventory, system)
	}
	if err := json.Unmarshal([]byte(original), &before); err != nil {
		t.Fatal(err)
	}
	delete(before.Messages[3], "reasoning_content")
	if !reflect.DeepEqual(got.Messages[1:], before.Messages[2:]) {
		t.Fatal("the model lost a conversation message or tool result")
	}
}

func TestModelRefusesMissingConversation(t *testing.T) {
	t.Parallel()
	_, err := conversationMessages(json.RawMessage(`[{"role":"system","content":"cached setup"}]`), "space")
	if err == nil {
		t.Fatal("a request containing only native setup reached the model")
	}
}

func TestModelRefusesUnrecognizedHistoryRole(t *testing.T) {
	t.Parallel()
	_, err := conversationMessages(json.RawMessage(`[{"role":"user","content":"hello"},{"role":"control","content":"private"}]`), "space")
	if err == nil {
		t.Fatal("an unrecognized control message reached the model")
	}
}
