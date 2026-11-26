package hermes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Run names both records Hermes owns: the stream handle and the execution claim.
type Run struct {
	ID        string `json:"run_id"`
	Admission string `json:"admission_id"`
	Session   string `json:"session_id"`
	Status    string `json:"status"`
}

type Event struct {
	Kind      string `json:"event"`
	Delta     string `json:"delta"`
	Output    string `json:"output"`
	Completed bool   `json:"completed"`
	Partial   bool   `json:"partial"`
	Exit      string `json:"turn_exit_reason"`
	Error     any    `json:"error"`
}

// Submit sends the original message to Hermes. Its claim binds later callbacks
// to this submission even when a conversation is continued or runs overlap.
func (g *Gateway) Submit(ctx context.Context, key, session string, input any) (Run, error) {
	body, err := json.Marshal(map[string]any{"input": input})
	if err != nil {
		return Run{}, fmt.Errorf("encode Hermes run: %w", err)
	}
	resp, err := g.call(ctx, http.MethodPost, "/v1/runs", body, headers{key: key, session: session})
	if err != nil {
		return Run{}, err
	}
	submitted, err := readRun(resp)
	if err != nil {
		return Run{}, err
	}
	run, err := g.state(ctx, submitted.ID)
	if err != nil {
		return submitted, err
	}
	if run.ID != submitted.ID || run.Admission == "" {
		return submitted, fmt.Errorf("hermes returned an unbound admission")
	}
	return run, nil
}

func readRun(resp *http.Response) (Run, error) {
	defer resp.Body.Close() //nolint:errcheck // response is consumed
	var run Run
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return Run{}, fmt.Errorf("read Hermes run: %w", err)
	}
	if run.ID == "" {
		return Run{}, fmt.Errorf("hermes returned an empty run id")
	}
	return run, nil
}

func runPath(id string) string { return "/v1/runs/" + url.PathEscape(id) }

func (g *Gateway) state(ctx context.Context, id string) (Run, error) {
	resp, err := g.call(ctx, http.MethodGet, runPath(id), nil, headers{})
	if err != nil {
		return Run{}, err
	}
	return readRun(resp)
}

type headers struct{ key, session string }

func (g *Gateway) call(ctx context.Context, method, path string, data []byte, h headers) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, gatewayURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build Hermes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.key)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.key != "" {
		req.Header.Set("Idempotency-Key", h.key)
		req.Header.Set("X-Hermes-Session-Key", h.session)
	}
	resp, err := g.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach Hermes: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close() //nolint:errcheck // only the status is reported
		return nil, fmt.Errorf("hermes %s %s: %w", method, path, statusCode(resp.StatusCode))
	}
	return resp, nil
}

type statusCode int

func (s statusCode) Error() string { return fmt.Sprintf("HTTP %d", int(s)) }

// CloseProbe retires the new run-scoped session created by the private boot probe.
func (g *Gateway) CloseProbe(ctx context.Context, run Run) error {
	if run.Session == "" {
		return nil
	}
	if run.Session != run.ID {
		return fmt.Errorf("inventory session was not created for this probe")
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := g.state(ctx, run.ID)
		if err != nil {
			return err
		}
		switch state.Status {
		case "completed", "cancelled", "failed", "interrupted":
			return g.retireSession(ctx, run.ID)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("retire inventory session: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func (g *Gateway) retireSession(ctx context.Context, id string) error {
	err := finish(g.call(ctx, http.MethodDelete, "/api/sessions/"+url.PathEscape(id), nil, headers{}))
	if errors.Is(err, statusCode(http.StatusNotFound)) {
		return nil
	}
	return err
}

// Events consumes the native SSE stream until Hermes records a terminal result.
func (g *Gateway) Events(ctx context.Context, id string, deliver func(Event) error) error {
	resp, err := g.call(ctx, http.MethodGet, runPath(id)+"/events", nil, headers{})
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // event stream is closed on return
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 4096), 8<<20)
	for scan.Scan() {
		payload, ok := strings.CutPrefix(scan.Text(), "data:")
		if !ok {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			return fmt.Errorf("decode Hermes event: %w", err)
		}
		if err := deliver(e); err != nil {
			return fmt.Errorf("deliver Hermes event: %w", err)
		}
		if terminal, err := e.outcome(); terminal {
			return err
		}
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("read Hermes events: %w", err)
	}
	return fmt.Errorf("hermes event stream closed before completion")
}

func (e Event) outcome() (bool, error) {
	switch e.Kind {
	case "run.completed":
		if !e.Completed || e.Partial {
			return true, fmt.Errorf("hermes ended with an incomplete answer")
		}
		return true, nil
	case "run.failed", "run.cancelled", "run.interrupted", "run.unknown":
		reason := strings.TrimSpace(e.Kind + " " + e.Exit)
		if detail, ok := e.Error.(string); ok && detail != "" {
			reason += " " + detail
		}
		return true, fmt.Errorf("hermes terminated the run: %s", reason)
	default:
		return false, nil
	}
}

func (g *Gateway) Cancel(ctx context.Context, id string) error {
	return finish(g.call(ctx, http.MethodPost, runPath(id)+"/stop", []byte("{}"), headers{}))
}

func finish(resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close Hermes response: %w", err)
	}
	return nil
}
