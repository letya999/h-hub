package svc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/log"
)

func (s *Server) Loops(ctx context.Context, l *log.L) {
	if s.deps.TG != nil {
		go s.runTelegram(ctx, l, s.deps.TG)
	}
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
			s.sweepPass(ctx, l)
		}
	}
}

func (s *Server) sweepPass(ctx context.Context, l *log.L) {
	for _, m := range []*fsm.Machine{s.intake, s.delivery} {
		if _, err := m.Sweep(ctx, s.db, s.now(), 32, nil); err != nil {
			l.Error(ctx, "channel sweep", err)
		}
	}
	if err := fsm.WriteTick(ctx, s.db, "fsm", s.sub, s.now()); err != nil {
		l.Error(ctx, "watchdog tick", err)
	}
	if err := checkSpawnWatchdog(s, ctx); err != nil {
		l.Error(ctx, "spawn watchdog", err)
	}
}

func (s *Server) outboxPass(ctx context.Context, l *log.L) {
	fx, err := fsm.DueEffects(ctx, s.db, "fsm", s.now(), 32)
	if err != nil {
		l.Error(ctx, "outbox read", err)
		return
	}
	for _, f := range fx {
		done, err := s.execEffect(ctx, f)
		if err != nil {
			l.Error(ctx, "effect "+f.Topic, err)
		}
		if !done || err != nil {
			if err := fsm.DeferEffect(ctx, s.db, "fsm", f.Seq, s.now().Add(time.Second)); err != nil {
				l.Error(ctx, "effect defer", err)
			}
			continue
		}
		if err := fsm.MarkDone(ctx, s.db, "fsm", f.Seq); err != nil {
			l.Error(ctx, "effect done", err)
		}
	}
}

func (s *Server) execEffect(ctx context.Context, f fsm.OutEffect) (bool, error) {
	switch f.Topic {
	case "dispatch":
		return true, s.dispatch(ctx, f.MachineID)
	case "poll-job":
		return s.pollJob(ctx, f.MachineID)
	case "send":
		return s.send(ctx, f.MachineID)
	case "tg-delete":
		return s.tg.tgDelete(ctx, f.MachineID)
	case "tg-photo":
		return s.tg.tgPhoto(ctx, f.MachineID)
	default:
		return true, fmt.Errorf("unknown channel effect %q", f.Topic)
	}
}

type inputRow struct {
	id, channel, external, chat, topic, kind, payload, body, job, state string
}

func inputOf(ctx context.Context, s *Server, id string) (inputRow, error) {
	var r inputRow
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.channel,u.external_id,u.chat_id,u.topic,u.kind,
		u.payload,u.body,u.job_id,m.state FROM inbound_updates u
		JOIN fsm.machines m ON m.kind='intake' AND m.id=u.id WHERE u.id=$1`, id).
		Scan(&r.id, &r.channel, &r.external, &r.chat, &r.topic, &r.kind,
			&r.payload, &r.body, &r.job, &r.state)
	if err != nil {
		return r, fmt.Errorf("read intake: %w", err)
	}
	return r, nil
}

func (s *Server) dispatch(ctx context.Context, id string) error {
	r, err := inputOf(ctx, s, id)
	if err != nil {
		return err
	}
	if r.state != "processing" {
		return nil
	}
	if done, err := s.routeKind(ctx, r); done || err != nil {
		return err
	}
	if answered, err := (&requestOps{s}).tryAnswer(ctx, r); answered || err != nil {
		return err
	}
	client, resolved, err := s.taskClient(ctx, r.channel, r.external)
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return s.completeText(ctx, r, locale.T(locale.EN, "channel.link-required"),
			"channel.phase.refused")
	}
	if err != nil {
		return err
	}
	conv, err := (&threadOps{s}).conversation(ctx, r)
	if err != nil {
		return err
	}
	job, err := client.CreateJob(ctx, tv1.CreateJobReq{Space: resolved.Space, Conversation: conv,
		Input: r.body, Idem: r.id})
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return s.waitJob(ctx, r, job.ID)
}

// routeKind dispatches the non-job update kinds: intercepted
// secrets, photos, request callbacks and slash commands each have
// their own path; a plain text falls through to the request link
// check and the job flow.
func (s *Server) routeKind(ctx context.Context, r inputRow) (bool, error) {
	switch {
	case r.kind == "secret":
		return true, interceptSecret(ctx, s, r)
	case r.kind == "photo":
		return true, dispatchPhoto(ctx, s, r)
	case r.kind == "answer":
		return true, (&requestOps{s}).answerByID(ctx, r)
	case strings.HasPrefix(r.body, "/"):
		return true, (&commandOps{s}).command(ctx, r)
	}
	return false, nil
}

// interceptSecret answers a KEY=value-looking message with the safe
// input path and queues the provider-side delete; the body never
// reaches a job.
func interceptSecret(ctx context.Context, s *Server, r inputRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("secret begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		return enqueueDeliveries(ctx, s, tx, r, []cv1.OutMessage{{
			Kind: "text", Body: locale.T(locale.EN, "channel.secret-hint")}})
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: r.id, Event: "complete",
		Actor: "channel-hub", Effects: []fsm.Effect{{Topic: "tg-delete", Payload: []byte(r.id)}}}, hook); err != nil {
		return fmt.Errorf("secret intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("secret commit: %w", err)
	}
	return nil
}

// dispatchPhoto gates a photo on the caller's active route vision
// flag, then hands the fetch to the tg-photo effect; the refusal is
// localized text, never an opaque error.
func dispatchPhoto(ctx context.Context, s *Server, r inputRow) error {
	mc, err := (&commandOps{s}).modelClient(ctx, r.channel, r.external)
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return s.completeText(ctx, r, locale.T(locale.EN, "channel.link-required"),
			"channel.phase.refused")
	}
	if err != nil {
		return err
	}
	rt, err := mc.Routes(ctx, mv1.RoutesReq{})
	if err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	vision := false
	for _, x := range rt.Routes {
		if x.Name == rt.Default {
			vision = x.Vision
		}
	}
	if !vision {
		return s.completeText(ctx, r, locale.T(locale.EN, "channel.photo-no-vision"),
			"channel.phase.refused")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("photo begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: r.id, Event: "photo-fetch",
		Actor: "channel-hub", Effects: []fsm.Effect{{Topic: "tg-photo", Payload: []byte(r.id)}}}, nil); err != nil {
		return fmt.Errorf("photo intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("photo commit: %w", err)
	}
	return nil
}

func (s *Server) waitJob(ctx context.Context, r inputRow, job string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("wait begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE inbound_updates SET job_id=$1 WHERE id=$2`, job, r.id); err != nil {
			return fmt.Errorf("record job: %w", err)
		}
		if r.channel == "telegram" {
			return enqueueDelivery(ctx, s, tx, r, cv1.OutMessage{
				Kind: "status", Body: locale.T(locale.EN, "channel.phase.working")})
		}
		return nil
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: r.id, Event: "wait", Actor: "channel-hub",
		Effects: []fsm.Effect{{Topic: "poll-job", Payload: []byte(job)}}}, hook); err != nil {
		return fmt.Errorf("wait intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("wait commit: %w", err)
	}
	return nil
}

func (s *Server) pollJob(ctx context.Context, id string) (bool, error) {
	r, err := inputOf(ctx, s, id)
	if err != nil {
		return false, err
	}
	if r.state == "completed" || r.state == "failed" {
		return true, nil
	}
	if r.state != "waiting" {
		return false, fmt.Errorf("poll intake state %q", r.state)
	}
	client, _, err := s.taskClient(ctx, r.channel, r.external)
	if err != nil {
		return false, err
	}
	job, err := client.GetJob(ctx, tv1.GetJobReq{ID: r.job})
	if err != nil {
		return false, fmt.Errorf("get job: %w", err)
	}
	if !job.Job.Done {
		if r.channel == "telegram" {
			s.tg.tgStatusPass(ctx, r.id, locale.T(locale.EN, phaseKey(job.Job.State)), false)
		}
		return false, nil
	}
	answer := job.Job.Result
	if job.Job.State != "succeeded" || answer == "" {
		answer = locale.T(locale.EN, "channel.job-failed", "state", job.Job.State)
	}
	return true, s.completeText(ctx, r, answer, phaseKey(job.Job.State))
}

// phaseKey maps a job state onto the status-message vocabulary; the
// status text carries a phase word only, never content.
func phaseKey(state string) string {
	switch state {
	case "succeeded":
		return "channel.phase.done"
	case "failed", "cancelled", "quarantined":
		return "channel.phase.failed"
	case "uncertain":
		return "channel.phase.uncertain"
	default:
		return "channel.phase.working"
	}
}

func (s *Server) completeText(ctx context.Context, r inputRow, answer string, phase ...string) error {
	final := "channel.phase.done"
	if len(phase) > 0 {
		final = phase[0]
	}
	parts := renderAnswer(answer)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("complete begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if err := enqueueDeliveries(ctx, s, tx, r, parts); err != nil {
			return err
		}
		if r.channel == "telegram" {
			return enqueueDelivery(ctx, s, tx, r, cv1.OutMessage{
				Kind: "status", Body: locale.T(locale.EN, final)})
		}
		return nil
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: r.id, Event: "complete", Actor: "channel-hub"}, hook); err != nil {
		return fmt.Errorf("complete intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("complete commit: %w", err)
	}
	return nil
}

func enqueueDeliveries(ctx context.Context, s *Server, tx *sql.Tx, r inputRow, parts []cv1.OutMessage) error {
	for _, p := range parts {
		if err := enqueueDelivery(ctx, s, tx, r, p); err != nil {
			return err
		}
	}
	return nil
}

func enqueueDelivery(ctx context.Context, s *Server, tx *sql.Tx, r inputRow, p cv1.OutMessage) error {
	deliveryID, err := randomID("d-")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,external_id,kind,body,channel,chat_id,topic,intake_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, deliveryID, r.external, p.Kind, p.Body,
		r.channel, r.chat, r.topic, r.id); err != nil {
		return fmt.Errorf("delivery insert: %w", err)
	}
	if err := s.delivery.Create(ctx, tx, clock{s.deps.Now}, fsm.CreateReq{ID: deliveryID, Initial: "queued", Actor: "channel-hub"}); err != nil {
		return fmt.Errorf("delivery create: %w", err)
	}
	if err := s.delivery.Transition(ctx, tx, fsm.Request{ID: deliveryID, Event: "send", Actor: "channel-hub",
		Effects: []fsm.Effect{{Topic: "send", Payload: []byte(deliveryID)}}}, nil); err != nil {
		return fmt.Errorf("delivery send: %w", err)
	}
	return nil
}

// send routes the "send" effect to the channel's sender. The mock
// sender appends to mock_outbox; telegram performs a Bot API call.
func (s *Server) send(ctx context.Context, id string) (bool, error) {
	var channel string
	err := s.db.QueryRowContext(ctx,
		`SELECT channel FROM deliveries WHERE id=$1`, id).Scan(&channel)
	if err != nil {
		return false, fmt.Errorf("send channel: %w", err)
	}
	if channel == "telegram" {
		return s.tg.sendTelegram(ctx, id)
	}
	return true, sendMockDelivery(ctx, s, id)
}

func sendMockDelivery(ctx context.Context, s *Server, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("send begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines WHERE kind='delivery' AND id=$1`, id).Scan(&state); err != nil {
		return fmt.Errorf("delivery state: %w", err)
	}
	if state == "sent" {
		return nil
	}
	if state != "sending" {
		return herrors.New(herrors.CodeConflict, "err.machine-state")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mock_outbox(delivery_id,external_id,kind,body)
		SELECT id,external_id,kind,body FROM deliveries WHERE id=$1 ON CONFLICT(delivery_id) DO NOTHING`, id); err != nil {
		return fmt.Errorf("mock send: %w", err)
	}
	if err := s.delivery.Transition(ctx, tx, fsm.Request{ID: id, Event: "ack", Actor: "mock"}, nil); err != nil {
		return fmt.Errorf("delivery ack: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("send commit: %w", err)
	}
	return nil
}
