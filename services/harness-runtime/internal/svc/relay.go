package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
)

// Bridge is served exclusively on loopback. Its callers supply only an upstream
// execution handle. The signed run token and all downstream identity come from SubmitRun.
func (s *Server) Bridge() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools", s.scoped(func(w http.ResponseWriter, _ *http.Request, a *Admission) error {
		httpapi.WriteJSON(w, a.allowed)
		return nil
	}))
	mux.HandleFunc("POST /tool", s.scoped(s.serveTool))
	mux.HandleFunc("POST /v1/chat/completions", s.scoped(s.serveModel))
	return mux
}

func (s *Server) scoped(call func(http.ResponseWriter, *http.Request, *Admission) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-HHub-Admission")
		if !s.deps.Gateway.Authentic(id, r.Header.Get("X-HHub-Proof")) {
			http.Error(w, "unbound admission", http.StatusForbidden)
			return
		}
		a := s.lookupExecution(r.Context(), id)
		if a == nil {
			http.Error(w, "unknown admission", http.StatusForbidden)
			return
		}
		if err := call(w, r, a); err != nil {
			if a.probe != nil {
				a.probeResult(err)
				http.Error(w, "inventory request finished", http.StatusForbidden)
				return
			}
			s.refuse(a, err)
			http.Error(w, "admitted request refused", http.StatusForbidden)
		}
	}
}

func (s *Server) refuse(a *Admission, err error) {
	var refusal *herrors.Error
	if errors.As(err, &refusal) {
		err = errors.New(refusal.Error())
	}
	s.mu.Lock()
	if a.fault == nil {
		a.fault = err
	}
	s.mu.Unlock()
	a.stop()
}

func (s *Server) lookupExecution(ctx context.Context, id string) *Admission {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		s.mu.Lock()
		if a := s.probeAdmission; a != nil && a.native.Admission == id {
			s.mu.Unlock()
			return a
		}
		for _, a := range s.admit {
			if a.native.Admission == id && !a.done && a.ctx.Err() == nil {
				s.mu.Unlock()
				return a
			}
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
		}
	}
}

func (s *Server) serveModel(w http.ResponseWriter, r *http.Request, a *Admission) error {
	var request map[string]json.RawMessage
	if err := httpapi.Decode(r, &request); err != nil {
		return fmt.Errorf("decode Hermes model request: %w", err)
	}
	offered, err := modelTools(request["tools"], a.allowed)
	if err != nil {
		return err
	}
	if a.probe != nil {
		if len(offered) != len(a.allowed) {
			return fmt.Errorf("hermes tool list is missing a projected tool")
		}
		a.probeResult(nil)
		http.Error(w, "startup inventory complete", http.StatusForbidden)
		return nil
	}
	stream := string(request["stream"]) == "true"
	body, err := spaceModelRequest(request, offered, a.instructions(s.deps.Rendered.Instructions))
	if err != nil {
		return fmt.Errorf("encode Hermes model request: %w", err)
	}
	c := &mv1.Client{T: mv1.HTTP{C: httpapi.NewClientTimeout(s.deps.Rendered.ModelURL, a.Token,
		time.Duration(s.deps.Rendered.TimeoutMS)*time.Millisecond)}}
	res, err := c.Complete(a.ctx, mv1.CompleteReq{Run: a.Run, Request: body})
	if err != nil {
		return fmt.Errorf("model gateway: %w", err)
	}
	if err := checkReturnedTools(res.Response, offered); err != nil {
		return err
	}
	return writeCompletion(w, res.Response, stream)
}

func (a *Admission) instructions(rendered string) string {
	if a.skills == "" {
		return rendered
	}
	return rendered + "\n\n" + a.skills
}

// A child may offer fewer tools than its parent. Each response is bound to its own request.
func modelTools(raw json.RawMessage, allowed map[string]string) (map[string]bool, error) {
	var definitions []struct{ Function struct{ Name string } }
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &definitions); err != nil {
			return nil, fmt.Errorf("decode Hermes tool definitions: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, def := range definitions {
		name := def.Function.Name
		if _, ok := allowed[name]; !ok || seen[name] {
			return nil, fmt.Errorf("unexpected Hermes tool %q", name)
		}
		seen[name] = true
	}
	return seen, nil
}

func checkReturnedTools(raw json.RawMessage, offered map[string]bool) error {
	var res struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct{ Function struct{ Name string } } `json:"tool_calls"`
			}
		}
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("read model gateway response: %w", err)
	}
	for _, choice := range res.Choices {
		for _, call := range choice.Message.ToolCalls {
			if !offered[call.Function.Name] {
				return fmt.Errorf("model requested an unprojected tool %q", call.Function.Name)
			}
		}
	}
	return nil
}

func (s *Server) serveTool(w http.ResponseWriter, r *http.Request, a *Admission) error {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := httpapi.Decode(r, &call); err != nil {
		return fmt.Errorf("decode Hermes tool request: %w", err)
	}
	set, ok := a.allowed[call.Name]
	if !ok || set == "" || a.probe != nil {
		return fmt.Errorf("tool is outside the admitted projection")
	}
	c := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(s.deps.Rendered.ToolURL, a.Token)}}
	res, err := c.Invoke(a.ctx, thv1.InvokeReq{Run: a.Run, Digest: a.Digest, Toolset: set, Tool: call.Name, Arguments: call.Arguments})
	if err != nil {
		return writeToolRefusal(w, err)
	}
	if set == "ownerreq" || set == "files" {
		if err := s.ownerEvent(a, res.Result); err != nil {
			return err
		}
	}
	httpapi.WriteJSON(w, res.Result)
	return nil
}

func writeToolRefusal(w http.ResponseWriter, err error) error {
	var refusal *herrors.Error
	if !errors.As(err, &refusal) || refusal.Code == herrors.CodeUnavailable {
		return fmt.Errorf("tool hub: %w", err)
	}
	result := map[string]any{"reason": refusal.Key, "error": locale.T(locale.EN, refusal.Key)}
	if finding := skillFindingDetails(refusal.Details); finding != nil {
		result["details"] = finding
	}
	httpapi.WriteJSON(w, result)
	return nil
}

func skillFindingDetails(details map[string]any) map[string]any {
	file, hasFile := details["file"].(string)
	rule, hasRule := details["rule"].(string)
	line, hasLine := details["line"].(float64)
	if !hasFile || !hasRule || !hasLine {
		return nil
	}
	return map[string]any{"file": file, "line": line, "rule": rule}
}

func (s *Server) ownerEvent(a *Admission, result json.RawMessage) error {
	var res struct {
		Request  string `json:"request_id"`
		State    string `json:"state"`
		Deadline int64  `json:"deadline"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return fmt.Errorf("decode owner tool result: %w", err)
	}
	if res.State != "pending" || res.Request == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{"request": res.Request, "deadline": res.Deadline})
	if err != nil {
		return fmt.Errorf("encode owner request event: %w", err)
	}
	return s.report(a, "owner-request", string(body))
}
