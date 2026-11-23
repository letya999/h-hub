package svc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/log"
)

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
		return true, s.sendMock(ctx, f.MachineID)
	default:
		return true, fmt.Errorf("unknown channel effect %q", f.Topic)
	}
}

type inputRow struct{ id, external, body, job, state string }

func (s *Server) input(ctx context.Context, id string) (inputRow, error) {
	var r inputRow
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.external_id,u.body,u.job_id,m.state FROM inbound_updates u
		JOIN fsm.machines m ON m.kind='intake' AND m.id=u.id WHERE u.id=$1`, id).Scan(&r.id, &r.external, &r.body, &r.job, &r.state)
	if err != nil {
		return r, fmt.Errorf("read intake: %w", err)
	}
	return r, nil
}

func (s *Server) dispatch(ctx context.Context, id string) error {
	r, err := s.input(ctx, id)
	if err != nil {
		return err
	}
	if r.state != "processing" {
		return nil
	}
	if strings.HasPrefix(r.body, "/") {
		return (&commandOps{s}).command(ctx, r)
	}
	client, resolved, err := s.taskClient(ctx, r.external)
	if herrors.CodeOf(err) == herrors.CodeNotFound {
		return s.completeText(ctx, r.id, r.external, locale.T(locale.EN, "channel.link-required"))
	}
	if err != nil {
		return err
	}
	conv, err := s.conversation(ctx, r.external)
	if err != nil {
		return err
	}
	job, err := client.CreateJob(ctx, tv1.CreateJobReq{Space: resolved.Space, Conversation: conv,
		Input: r.body, Idem: r.id})
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return s.waitJob(ctx, r.id, job.ID)
}

func (s *Server) waitJob(ctx context.Context, id, job string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("wait begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		if _, err := tx.ExecContext(ctx, `UPDATE inbound_updates SET job_id=$1 WHERE id=$2`, job, id); err != nil {
			return fmt.Errorf("record job: %w", err)
		}
		return nil
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: id, Event: "wait", Actor: "channel-hub",
		Effects: []fsm.Effect{{Topic: "poll-job", Payload: []byte(job)}}}, hook); err != nil {
		return fmt.Errorf("wait intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("wait commit: %w", err)
	}
	return nil
}

func (s *Server) pollJob(ctx context.Context, id string) (bool, error) {
	r, err := s.input(ctx, id)
	if err != nil {
		return false, err
	}
	if r.state == "completed" || r.state == "failed" {
		return true, nil
	}
	if r.state != "waiting" {
		return false, fmt.Errorf("poll intake state %q", r.state)
	}
	client, _, err := s.taskClient(ctx, r.external)
	if err != nil {
		return false, err
	}
	job, err := client.GetJob(ctx, tv1.GetJobReq{ID: r.job})
	if err != nil {
		return false, fmt.Errorf("get job: %w", err)
	}
	if !job.Job.Done {
		return false, nil
	}
	answer := job.Job.Result
	if answer == "" {
		answer = locale.T(locale.EN, "channel.job-failed", "state", job.Job.State)
	}
	return true, s.completeText(ctx, r.id, r.external, answer)
}

func (s *Server) completeText(ctx context.Context, id, external, answer string) error {
	parts := renderAnswer(answer)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("complete begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	hook := func(ctx context.Context, tx *sql.Tx, _ fsm.Change) error {
		return s.enqueue(ctx, tx, id, external, parts)
	}
	if err := s.intake.Transition(ctx, tx, fsm.Request{ID: id, Event: "complete", Actor: "channel-hub"}, hook); err != nil {
		return fmt.Errorf("complete intake: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("complete commit: %w", err)
	}
	return nil
}

func (s *Server) enqueue(ctx context.Context, tx *sql.Tx, id, external string, parts []cv1.OutMessage) error {
	for i, p := range parts {
		deliveryID := fmt.Sprintf("%s-d-%d", id, i)
		if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,external_id,kind,body) VALUES($1,$2,$3,$4)`, deliveryID, external, p.Kind, p.Body); err != nil {
			return fmt.Errorf("delivery insert: %w", err)
		}
		if err := s.delivery.Create(ctx, tx, clock{s.deps.Now}, fsm.CreateReq{ID: deliveryID, Initial: "queued", Actor: "channel-hub"}); err != nil {
			return fmt.Errorf("delivery create: %w", err)
		}
		if err := s.delivery.Transition(ctx, tx, fsm.Request{ID: deliveryID, Event: "send", Actor: "channel-hub",
			Effects: []fsm.Effect{{Topic: "send", Payload: []byte(deliveryID)}}}, nil); err != nil {
			return fmt.Errorf("delivery send: %w", err)
		}
	}
	return nil
}

func (s *Server) sendMock(ctx context.Context, id string) error {
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
