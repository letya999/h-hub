package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/config"
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
	text, err := r.turn(ctx, a.Input)
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

// turn is the one scripted-model call. The request is stripped to
// exactly the OpenAI fields — no caller headers ever propagate.
func (r *HermesRunner) turn(ctx context.Context, input string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":    "scripted",
		"messages": []map[string]string{{"role": "user", "content": input}},
	})
	if err != nil {
		return "", fmt.Errorf("model request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.ModelURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("model request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := r.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("model call: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("model read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model status %d", res.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("model decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("model: no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// reportHTTP posts the journal events to task-engine.
func (r *HermesRunner) reportHTTP(ctx context.Context, a Admission, events []tv1.RunEvent) error {
	c := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(r.cfg.TaskURL, a.Token)}}
	if err := c.ReportRun(ctx, tv1.ReportRunReq{Run: a.Run, Events: events}); err != nil {
		return fmt.Errorf("report run: %w", err)
	}
	return nil
}
