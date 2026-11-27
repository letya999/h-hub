package svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	hv1 "github.com/letya999/h-hub/contracts/harness/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/services/harness-runtime/internal/hermes"
	"github.com/letya999/h-hub/shared/httpapi"
)

func (s *Server) runAdmission(a *Admission, request hv1.SubmitRunReq) {
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(s.deps.Rendered.TimeoutMS)*time.Millisecond)
	defer cancel()
	defer a.stop()
	receipt, err := s.reportReceipt(a, "start", "")
	if err == nil && receipt.ConversationKey == "" {
		err = errors.New("task-engine did not bind the run to a conversation")
	}
	var answer string
	if err == nil {
		done := make(chan struct{})
		go s.heartbeat(ctx, a, done)
		answer, err = s.runHermes(ctx, a, request, receipt.ConversationKey)
		cancel()
		<-done
	}
	s.mu.Lock()
	switch {
	case a.fault != nil:
		err = a.fault
	case a.ctx.Err() != nil:
		err = errors.New("cancelled")
	}
	s.mu.Unlock()
	kind := "final-ok"
	if err != nil {
		kind, answer = "final-fail", err.Error()
	}
	if err := s.report(a, kind, answer); err != nil {
		s.deps.Log.Error(context.Background(), "report "+kind+" "+a.Run, err)
	}
	s.mu.Lock()
	a.done = true
	s.mu.Unlock()
}

func (s *Server) runHermes(ctx context.Context, a *Admission, request hv1.SubmitRunReq, session string) (answer string, err error) {
	allowed, skills, err := s.projection(ctx, a)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("before Hermes submission: %w", err)
	}
	// Obtain the receipt even if cancellation races a committed upstream submission.
	submit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	run, err := s.deps.Gateway.Submit(submit, a.Run, session, upstreamInput(request))
	if run.ID != "" {
		defer func() {
			if err != nil {
				stop, finish := context.WithTimeout(context.Background(), 5*time.Second)
				defer finish()
				err = errors.Join(err, s.deps.Gateway.Cancel(stop, run.ID))
			}
		}()
	}
	if err != nil {
		return "", fmt.Errorf("submit Hermes run: %w", err)
	}
	s.mu.Lock()
	a.native, a.allowed, a.skills = run, allowed, skills
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	err = s.deps.Gateway.Events(ctx, run.ID, func(e hermes.Event) error {
		switch e.Kind {
		case "message.delta":
			return s.report(a, "delta", e.Delta)
		case "run.completed":
			answer = e.Output
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("observe Hermes run: %w", err)
	}
	return answer, nil
}

func upstreamInput(r hv1.SubmitRunReq) any {
	if len(r.Attachments) == 0 {
		return r.Input
	}
	parts := []map[string]any{{"type": "text", "text": r.Input}}
	for _, at := range r.Attachments {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + at.Mime + ";base64," + at.Data}})
	}
	return []map[string]any{{"role": "user", "content": parts}}
}

func (s *Server) heartbeat(ctx context.Context, a *Admission, done chan<- struct{}) {
	defer close(done)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.report(a, "progress", ""); err != nil {
				s.deps.Log.Error(ctx, "report progress "+a.Run, err)
			}
		}
	}
}

func (s *Server) report(a *Admission, kind, data string) error {
	_, err := s.reportReceipt(a, kind, data)
	return err
}

func (s *Server) reportReceipt(a *Admission, kind, data string) (tv1.ReportRunResp, error) {
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	a.sequence++
	request := tv1.ReportRunReq{Run: a.Run, Events: []tv1.RunEvent{{Seq: a.sequence, Kind: kind, Data: data}}}
	c := &tv1.Client{T: tv1.HTTP{C: httpapi.NewClient(s.deps.Rendered.TaskURL, a.Token)}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var receipt tv1.ReportRunResp
	var err error
	for range 2 {
		receipt, err = c.ReportRunReceipt(ctx, request)
		if err == nil {
			return receipt, nil
		}
	}
	return tv1.ReportRunResp{}, fmt.Errorf("journal Hermes event: %w", err)
}

func (s *Server) projection(ctx context.Context, a *Admission) (map[string]string, string, error) {
	allowed := map[string]string{}
	if s.deps.Rendered.ToolURL == "" {
		return allowed, "", nil
	}
	c := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(s.deps.Rendered.ToolURL, a.Token)}}
	p, err := c.Projection(ctx, thv1.ProjectionReq{Run: a.Run, Digest: a.Digest})
	if err != nil {
		if thv1.IsEmptyProjection(err) {
			return allowed, "", nil
		}
		return nil, "", fmt.Errorf("project admitted tools: %w", err)
	}
	allowed, err = approvedTools(p)
	if err != nil {
		return nil, "", err
	}
	skills, err := loadSkillPrompt(ctx, c, a, allowed)
	return allowed, skills, err
}

func loadSkillPrompt(ctx context.Context, c *thv1.Client, a *Admission, allowed map[string]string) (string, error) {
	if allowed["skills_search"] != "skills" {
		return "", nil
	}
	catalog, err := c.SkillPrompt(ctx, thv1.SkillPromptReq{Run: a.Run, Digest: a.Digest})
	if err != nil {
		return "", fmt.Errorf("resolve admitted skill catalog: %w", err)
	}
	return catalog.Text, nil
}
