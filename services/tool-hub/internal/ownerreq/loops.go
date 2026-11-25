package ownerreq

import (
	"context"
	"fmt"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

// Loops runs the module's outbox executor and deadline sweeper.
// tool-hub is single-instance in v1.0 — no lease.
func (s *Server) Loops(ctx context.Context, l *log.L) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	sweep := time.NewTicker(time.Second)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.outboxPass(ctx, l)
		case <-sweep.C:
			if _, err := s.m.Sweep(ctx, s.db, s.deps.Now(), 32, s.retractOnFinal); err != nil {
				l.Error(ctx, "request sweep", err)
			}
		}
	}
}

func (s *Server) outboxPass(ctx context.Context, l *log.L) {
	fx, err := fsm.DueEffects(ctx, s.db, "fsm", s.deps.Now(), 32)
	if err != nil {
		l.Error(ctx, "request outbox read", err)
		return
	}
	for _, f := range fx {
		done, err := s.execEffect(ctx, f)
		if err != nil {
			l.Error(ctx, "request effect "+f.Topic, err)
		}
		if !done || err != nil {
			if err := fsm.DeferEffect(ctx, s.db, "fsm", f.Seq, s.deps.Now().Add(time.Second)); err != nil {
				l.Error(ctx, "request defer", err)
			}
			continue
		}
		if err := fsm.MarkDone(ctx, s.db, "fsm", f.Seq); err != nil {
			l.Error(ctx, "request done", err)
		}
	}
}

func (s *Server) execEffect(ctx context.Context, f fsm.OutEffect) (bool, error) {
	switch f.Topic {
	case "deliver-request":
		return s.deliver(ctx, f.MachineID)
	case "retract-request":
		return s.retract(ctx, f.MachineID)
	default:
		return true, fmt.Errorf("unknown request effect %q", f.Topic)
	}
}

// channelClient builds a channel-hub client with a service token.
func channelClient(ctx context.Context, deps Deps) (*cv1.Client, error) {
	tok, err := deps.Tokens.Token(ctx, "channel-hub")
	if err != nil {
		return nil, fmt.Errorf("channel token: %w", err)
	}
	return &cv1.Client{T: cv1.HTTP{
		C: httpapi.NewClient(deps.ChannelURL, tok)}}, nil
}

// deliver hands the request to channel-hub and records the delivery
// it landed as — the delivery side of the pair.
func (s *Server) deliver(ctx context.Context, id string) (bool, error) {
	rec, err := s.load(ctx, id)
	if err != nil {
		return false, err
	}
	if rec.state != "pending" {
		return true, nil
	}
	cl, err := channelClient(ctx, s.deps)
	if err != nil {
		return false, err
	}
	res, err := cl.DeliverRequest(ctx, cv1.DeliverRequestReq{
		Request: rec.id, Owner: rec.owner, Kind: rec.kind,
		Prompt: rec.prompt, Options: rec.options, Deadline: rec.deadline.Unix(),
		Conversation: rec.conv, Channel: rec.channel, External: rec.external,
		Chat: rec.chat, Topic: rec.topic})
	if err != nil {
		switch herrors.CodeOf(err) {
		case herrors.CodeNotFound, herrors.CodeInvalid, herrors.CodeForbidden:
			// permanent: retrying cannot help — the request refuses
			// itself on its own deadline (sys.seam-onesided)
			s.l.Error(ctx, "request deliver refused", err)
			return true, nil
		}
		return false, fmt.Errorf("deliver request: %w", err)
	}
	if err := s.setDelivery(ctx, id, res.Delivery); err != nil {
		return false, err
	}
	return true, nil
}

// retract tells channel-hub the request closed; the link and any
// unfinished delivery retire on the channel side.
func (s *Server) retract(ctx context.Context, id string) (bool, error) {
	cl, err := channelClient(ctx, s.deps)
	if err != nil {
		return false, err
	}
	if _, err := cl.RetractRequest(ctx, cv1.RetractRequestReq{Request: id}); err != nil {
		return false, fmt.Errorf("retract request: %w", err)
	}
	return true, nil
}
