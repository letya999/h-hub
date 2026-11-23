package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type toolCall struct {
	Name      string `yaml:"name"`
	Arguments string `yaml:"arguments"`
}
type step struct {
	Expect        string     `yaml:"expect"`
	Respond       string     `yaml:"respond"`
	ToolCalls     []toolCall `yaml:"tool_calls"`
	ProviderError int        `yaml:"provider_error"`
	Hang          bool       `yaml:"hang"`
}
type scenario struct {
	ID      string `yaml:"id"`
	Steps   []step `yaml:"steps"`
	next    int
	failure string
}
type scriptStore struct {
	mu   sync.Mutex
	byID map[string]*scenario
}
type model struct{ scripts *scriptStore }

func newScriptStore() *scriptStore { return &scriptStore{byID: map[string]*scenario{}} }

var scenarioIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func (s *scenario) validate() error {
	if !scenarioIDRe.MatchString(s.ID) || len(s.Steps) == 0 {
		return fmt.Errorf("scenario id and steps required")
	}
	for i, st := range s.Steps {
		if err := validateStep(s.ID, i, st); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(id string, i int, st step) error {
	if !strings.HasPrefix(st.Expect, "scenario:"+id+" ") {
		return fmt.Errorf("step %d expect must name scenario", i)
	}
	actions := 0
	if st.Respond != "" {
		actions++
	}
	if len(st.ToolCalls) > 0 {
		actions++
	}
	if st.ProviderError != 0 {
		actions++
	}
	if st.Hang {
		actions++
	}
	if actions != 1 {
		return fmt.Errorf("step %d needs exactly one action", i)
	}
	if st.ProviderError != 0 && (st.ProviderError < 400 || st.ProviderError > 599) {
		return fmt.Errorf("step %d bad provider status", i)
	}
	if !validToolCalls(st.ToolCalls) {
		return fmt.Errorf("step %d bad tool call", i)
	}
	return nil
}

func validToolCalls(calls []toolCall) bool {
	for _, c := range calls {
		if c.Name == "" || c.Arguments == "" {
			return false
		}
	}
	return true
}

func (m *model) loadScenario(w http.ResponseWriter, r *http.Request) {
	dec := yaml.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.KnownFields(true)
	var sc scenario
	if err := dec.Decode(&sc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := sc.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.scripts.mu.Lock()
	defer m.scripts.mu.Unlock()
	if m.scripts.byID[sc.ID] != nil {
		http.Error(w, "scenario already loaded", http.StatusConflict)
		return
	}
	m.scripts.byID[sc.ID] = &sc
	w.WriteHeader(http.StatusCreated)
}

func (m *model) verifyScenario(w http.ResponseWriter, r *http.Request) {
	m.scripts.mu.Lock()
	defer m.scripts.mu.Unlock()
	sc := m.scripts.byID[r.PathValue("id")]
	if sc == nil {
		http.Error(w, "scenario not found", http.StatusNotFound)
		return
	}
	if sc.failure != "" {
		http.Error(w, sc.failure, http.StatusConflict)
		return
	}
	if sc.next != len(sc.Steps) {
		http.Error(w, fmt.Sprintf("unused steps: %d", len(sc.Steps)-sc.next), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *scriptStore) next(id, input string) (step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.byID[id]
	if sc == nil {
		return step{}, fmt.Errorf("unknown scenario %s", id)
	}
	if sc.failure != "" {
		return step{}, fmt.Errorf("scenario failed: %s", sc.failure)
	}
	if sc.next >= len(sc.Steps) {
		sc.failure = "unexpected request: " + input
		return step{}, fmt.Errorf("%s", sc.failure)
	}
	want := sc.Steps[sc.next]
	if want.Expect != input {
		sc.failure = fmt.Sprintf("step %d: got %q, want %q", sc.next, input, want.Expect)
		return step{}, fmt.Errorf("%s", sc.failure)
	}
	sc.next++
	return want, nil
}

func (m *model) scriptCompletion(w http.ResponseWriter, r *http.Request, input string) {
	name, _, ok := strings.Cut(strings.TrimPrefix(input, "scenario:"), " ")
	if !ok {
		http.Error(w, "scenario id missing", http.StatusBadRequest)
		return
	}
	st, err := m.scripts.next(name, input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if st.Hang {
		<-r.Context().Done()
		return
	}
	if st.ProviderError != 0 {
		http.Error(w, "scripted provider error", st.ProviderError)
		return
	}
	msg := map[string]any{"role": "assistant", "content": st.Respond}
	if len(st.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(st.ToolCalls))
		for i, c := range st.ToolCalls {
			calls = append(calls, map[string]any{"id": fmt.Sprintf("call_%d", i), "type": "function",
				"function": map[string]string{"name": c.Name, "arguments": c.Arguments}})
		}
		msg["tool_calls"] = calls
	}
	writeJSON(w, map[string]any{"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": "stop"}}})
}
