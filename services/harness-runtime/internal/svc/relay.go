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
			s.mu.Lock()
			if a.fault == nil {
				cause := err
				var refusal *herrors.Error
				if errors.As(err, &refusal) {
					cause = errors.New(refusal.Error())
				}
				a.fault = cause
			}
			s.mu.Unlock()
			a.stop()
			http.Error(w, "admitted request refused", http.StatusForbidden)
		}
	}
}

func (s *Server) lookupExecution(ctx context.Context, id string) *Admission {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		s.mu.Lock()
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
	if err := modelTools(request["tools"], a.allowed); err != nil {
		return err
	}
	stream := string(request["stream"]) == "true"
	delete(request, "stream")
	delete(request, "stream_options")
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode Hermes model request: %w", err)
	}
	c := &mv1.Client{T: mv1.HTTP{C: httpapi.NewClientTimeout(s.deps.Rendered.ModelURL, a.Token,
		time.Duration(s.deps.Rendered.TimeoutMS)*time.Millisecond)}}
	res, err := c.Complete(a.ctx, mv1.CompleteReq{Run: a.Run, Request: body})
	if err != nil {
		return fmt.Errorf("model gateway: %w", err)
	}
	if err := checkReturnedTools(res.Response, a.allowed); err != nil {
		return err
	}
	return writeCompletion(w, res.Response, stream)
}

func modelTools(raw json.RawMessage, allowed map[string]string) error {
	var definitions []struct{ Function struct{ Name string } }
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &definitions); err != nil {
			return fmt.Errorf("decode Hermes tool definitions: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, def := range definitions {
		name := def.Function.Name
		if _, ok := allowed[name]; !ok || seen[name] {
			return fmt.Errorf("unexpected Hermes tool %q", name)
		}
		seen[name] = true
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("hermes tool list is missing a projected tool")
	}
	return nil
}

func checkReturnedTools(raw json.RawMessage, allowed map[string]string) error {
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
			if _, ok := allowed[call.Function.Name]; !ok {
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
	if !ok {
		return fmt.Errorf("tool is outside the admitted projection")
	}
	c := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(s.deps.Rendered.ToolURL, a.Token)}}
	res, err := c.Invoke(a.ctx, thv1.InvokeReq{Run: a.Run, Digest: a.Digest, Toolset: set, Tool: call.Name, Arguments: call.Arguments})
	if err != nil {
		return fmt.Errorf("tool hub: %w", err)
	}
	if set == "ownerreq" {
		if err := s.ownerEvent(a, res.Result); err != nil {
			return err
		}
	}
	httpapi.WriteJSON(w, res.Result)
	return nil
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
