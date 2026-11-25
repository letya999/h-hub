package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

// HermesRunner executes the phase-3 turn: one OpenAI-compatible call
// to the scripted model, all tools disabled, then the events are
// reported to task-engine with the run token.
type HermesRunner struct {
	cfg    config.Rendered
	hc     *http.Client
	l      *log.L
	report func(ctx context.Context, a Admission, events []tv1.RunEvent) error
}

// NewRunner builds the production runner.
func NewRunner(cfg config.Rendered, l *log.L) *HermesRunner {
	hc := &http.Client{Timeout: time.Duration(cfg.TimeoutMS) * time.Millisecond}
	r := &HermesRunner{cfg: cfg, hc: hc, l: l}
	r.report = r.reportHTTP
	return r
}

// Run drives the turn: stream-start first, one model call under
// progress heartbeats, then the final event. The start report moves
// the run out of dispatched immediately; heartbeats keep the stall
// deadline armed while the model call blocks, so a killed runtime is
// detected by silence, not by a turn budget.
func (r *HermesRunner) Run(ctx context.Context, a Admission) {
	var seq atomic.Int64
	next := func(kind, data string) tv1.RunEvent {
		return tv1.RunEvent{Seq: seq.Add(1), Kind: kind, Data: data}
	}
	r.reportBest(a, next("start", ""))

	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				r.reportBest(a, next("progress", ""))
			}
		}
	}()
	text, err := r.turn(ctx, a, next)
	close(stop)

	var final tv1.RunEvent
	switch {
	case err != nil && ctx.Err() != nil:
		final = next("final-fail", "cancelled")
	case err != nil:
		final = next("final-fail", err.Error())
	default:
		final = next("final-ok", text)
	}
	r.reportBest(a, final)
}

// reportBest sends one journal event on a detached, bounded context:
// reports must land even after the run context is cancelled (the
// final-fail of an aborted turn still settles the machines), and a
// slow task-engine must not stall the turn loop.
func (r *HermesRunner) reportBest(a Admission, e tv1.RunEvent) {
	rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.report(rctx, a, []tv1.RunEvent{e}); err != nil {
		r.l.Error(rctx, "report "+e.Kind+" "+a.Run, err)
	}
}

// userContent renders the user message: plain text when the job
// carried no attachments, OpenAI content parts when it did — the
// gateway routes vision only to routes that declared it.
func userContent(a Admission) any {
	if len(a.Attachments) == 0 {
		return a.Input
	}
	parts := []map[string]any{{"type": "text", "text": a.Input}}
	for _, at := range a.Attachments {
		parts = append(parts, map[string]any{"type": "image_url",
			"image_url": map[string]string{"url": "data:" + at.Mime + ";base64," + at.Data}})
	}
	return parts
}

// maxToolRounds caps the tool-call loop: a model that never stops
// calling tools is a run failure, not an unbounded turn.
const maxToolRounds = 8

// assistantMsg is one assistant reply: content or tool calls.
type assistantMsg struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// turn drives the model conversation: the projection decides which
// tools exist for the model, each tool call goes through tool-hub
// admission, and the loop ends on plain content or the round cap.
// The request is stripped to exactly the OpenAI fields, the run
// token is the only credential, and no other caller headers ever
// propagate.
func (r *HermesRunner) turn(ctx context.Context, a Admission,
	next func(string, string) tv1.RunEvent) (string, error) {
	mc := &mv1.Client{T: mv1.HTTP{C: httpapi.NewClientTimeout(
		r.cfg.ModelURL, a.Token, time.Duration(r.cfg.TimeoutMS)*time.Millisecond)}}
	tools, toolset, err := r.projection(ctx, a)
	if err != nil {
		return "", err
	}
	messages := []map[string]any{{"role": "user", "content": userContent(a)}}
	for range maxToolRounds {
		body, err := r.modelRequest(tools, messages)
		if err != nil {
			return "", err
		}
		msg, err := r.complete(ctx, mc, a, body)
		if err != nil {
			return "", err
		}
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		messages = append(messages, map[string]any{
			"role": "assistant", "content": msg.Content, "tool_calls": msg.ToolCalls})
		for _, call := range msg.ToolCalls {
			content := r.callTool(ctx, a, toolset, call.Function.Name,
				json.RawMessage(call.Function.Arguments))
			if toolset[call.Function.Name] == "ownerreq" {
				r.reportOwnerRequest(a, next, content)
			}
			messages = append(messages, map[string]any{
				"role": "tool", "tool_call_id": call.ID,
				"content": content})
		}
	}
	return "", fmt.Errorf("tool rounds exhausted")
}

// ownerToolResult is the ownerreq toolset's result shape: identity
// and state, never links or nonces.
type ownerToolResult struct {
	Request  string `json:"request_id"`
	State    string `json:"state"`
	Deadline int64  `json:"deadline"`
}

// reportOwnerRequest journals the wait: when an ownerreq tool comes
// back pending, task-engine must bind the job to the request — the
// conversation stays free while the human decides. Anything
// unparseable is not a wait and rides on as a plain tool result.
func (r *HermesRunner) reportOwnerRequest(a Admission,
	next func(string, string) tv1.RunEvent, content string) {
	var res ownerToolResult
	if err := json.Unmarshal([]byte(content), &res); err != nil ||
		res.State != "pending" || res.Request == "" {
		return
	}
	data, err := json.Marshal(map[string]any{
		"request": res.Request, "deadline": res.Deadline})
	if err != nil {
		r.l.Error(context.Background(), "owner-request marshal", err)
		return
	}
	r.reportBest(a, next("owner-request", string(data)))
}

// modelRequest strips the request to the OpenAI fields the gateway
// relays; the tools array appears only when the projection is live.
func (r *HermesRunner) modelRequest(tools []map[string]any,
	messages []map[string]any) (json.RawMessage, error) {
	req := map[string]any{
		"model":    "scripted",
		"messages": messages,
	}
	if len(tools) > 0 {
		req["tools"] = tools
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("model request: %w", err)
	}
	return body, nil
}

// projection asks tool-hub which tools the model may see under this
// run's digest. A refused projection means no tools for the model;
// any other failure refuses the turn. A spec without tool_url —
// activated before tool calling existed — has no admission edge and
// likewise means no tools. The name -> toolset map lets the call
// path find the selected set again.
func (r *HermesRunner) projection(ctx context.Context, a Admission) ([]map[string]any,
	map[string]string, error) {
	if r.cfg.ToolURL == "" {
		return nil, nil, nil
	}
	tc := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClientTimeout(
		r.cfg.ToolURL, a.Token, time.Duration(r.cfg.TimeoutMS)*time.Millisecond)}}
	p, err := tc.Projection(ctx, thv1.ProjectionReq{Run: a.Run, Digest: a.Digest})
	if err != nil {
		if herrors.CodeOf(err) == herrors.CodeForbidden {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("tool projection: %w", err)
	}
	var tools []map[string]any
	toolset := map[string]string{}
	for _, t := range append(append([]thv1.ToolInfo{}, p.Core...), p.Extended...) {
		tools = append(tools, map[string]any{"type": "function",
			"function": map[string]any{"name": t.Name,
				"description": t.Description, "parameters": t.Parameters}})
		toolset[t.Name] = t.Toolset
	}
	return tools, toolset, nil
}

// callTool runs one model-asked call through tool-hub admission; the
// result — echo or refusal — is what the model reads back.
func (r *HermesRunner) callTool(ctx context.Context, a Admission,
	toolset map[string]string, name string, args json.RawMessage) string {
	tc := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClientTimeout(
		r.cfg.ToolURL, a.Token, time.Duration(r.cfg.TimeoutMS)*time.Millisecond)}}
	res, err := tc.Invoke(ctx, thv1.InvokeReq{
		Run: a.Run, Digest: a.Digest, Toolset: toolset[name], Tool: name,
		Arguments: args})
	if err != nil {
		return herrors.New(herrors.CodeOf(err), "err.tool-denied").Error()
	}
	return string(res.Result)
}

// complete sends one turn request and decodes the assistant message.
func (r *HermesRunner) complete(ctx context.Context, mc *mv1.Client,
	a Admission, body json.RawMessage) (assistantMsg, error) {
	var out assistantMsg
	res, err := mc.Complete(ctx, mv1.CompleteReq{Run: a.Run, Request: body})
	if err != nil {
		var he *herrors.Error
		if errors.As(err, &he) {
			return out, errors.New(he.Error())
		}
		return out, fmt.Errorf("model call: %w", err)
	}
	var wire struct {
		Choices []struct {
			Message assistantMsg `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(res.Response, &wire); err != nil {
		return out, fmt.Errorf("model decode: %w", err)
	}
	if len(wire.Choices) == 0 {
		return out, fmt.Errorf("model: no choices")
	}
	return wire.Choices[0].Message, nil
}

// reportHTTP posts the journal events to task-engine.
func (r *HermesRunner) reportHTTP(ctx context.Context, a Admission, events []tv1.RunEvent) error {
	c := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(r.cfg.TaskURL, a.Token)}}
	if err := c.ReportRun(ctx, tv1.ReportRunReq{Run: a.Run, Events: events}); err != nil {
		return fmt.Errorf("report run: %w", err)
	}
	return nil
}
