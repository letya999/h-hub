package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

var errNoClaim = errors.New("svc: nothing claimable")

// createJob inserts the job domain row and bumps the conversation
// cursor in one transaction; the caller creates the machine row in the
// same tx.
func createJob(ctx context.Context, tx *sql.Tx, j jobRow) error {
	var seq int64
	err := tx.QueryRowContext(ctx, `INSERT INTO conversations
		(owner, space, conversation, next_seq) VALUES ($1,$2,$3,2)
		ON CONFLICT (owner, space, conversation) DO UPDATE
		SET next_seq = conversations.next_seq + 1
		RETURNING next_seq - 1`, j.owner, j.space, j.conv).Scan(&seq)
	if err != nil {
		return fmt.Errorf("conversation cursor: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO jobs
		(id, owner, space, conversation, input, idem, conv_seq, attachments)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::jsonb)`,
		j.id, j.owner, j.space, j.conv, j.input, j.idem, seq, j.attach)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

// jobByIdem finds an existing job for a replayed idempotency key.
func jobByIdem(ctx context.Context, db *sql.DB, owner, idem string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx,
		"SELECT id FROM jobs WHERE owner=$1 AND idem=$2", owner, idem).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("idem lookup: %w", err)
	}
	return id, nil
}

// jobRow is the domain record (state lives in fsm.machines).
type jobRow struct {
	id     string
	owner  string
	space  string
	conv   string
	input  string
	idem   string
	result string
	attach string // raw jsonb of [{mime,data}], '' when absent
}

func getJob(ctx context.Context, db *sql.DB, owner, space, id string) (jobRow, error) {
	var j jobRow
	err := db.QueryRowContext(ctx, `SELECT id, owner, space, conversation,
		input, idem, result, COALESCE(attachments::text,'') FROM jobs WHERE owner=$1 AND space=$2 AND id=$3`,
		owner, space, id).Scan(&j.id, &j.owner, &j.space, &j.conv, &j.input, &j.idem, &j.result, &j.attach)
	if errors.Is(err, sql.ErrNoRows) {
		return j, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("job", id)
	}
	if err != nil {
		return j, fmt.Errorf("get job: %w", err)
	}
	return j, nil
}

func listJobs(ctx context.Context, db *sql.DB, owner, space string) ([]jobRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, owner, space, conversation,
		input, idem, result, COALESCE(attachments::text,'') FROM jobs WHERE owner=$1 AND space=$2 ORDER BY created`, owner, space)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []jobRow
	for rows.Next() {
		var j jobRow
		if err := rows.Scan(&j.id, &j.owner, &j.space, &j.conv,
			&j.input, &j.idem, &j.result, &j.attach); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// ownerLiveJobs counts an owner's jobs still in a non-final state —
// the admission ceiling joins domain rows to machine state.
func ownerLiveJobs(ctx context.Context, db *sql.DB, owner string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM jobs j
		JOIN fsm.machines m ON m.kind='job' AND m.id=j.id
		WHERE j.owner=$1 AND m.state <> ALL
		('{succeeded,failed,cancelled,quarantined}'::text[])`, owner).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("live jobs: %w", err)
	}
	return n, nil
}

// globalRunning counts running-state job machines cluster-wide.
func globalRunning(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM fsm.machines
		WHERE kind='job' AND state='running'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("running jobs: %w", err)
	}
	return n, nil
}

// claimable finds one conversation whose queued job may dispatch:
// no active run, admitted job or active hold on its space.
func claimable(ctx context.Context, tx *sql.Tx) (jobRow, error) {
	var j jobRow
	err := tx.QueryRowContext(ctx, `SELECT j.id, j.owner, j.space,
		j.conversation, j.input, j.idem, j.result, COALESCE(j.attachments::text,'')
		FROM jobs j
		JOIN fsm.machines m ON m.kind='job' AND m.id=j.id AND m.state='queued'
		JOIN conversations c ON c.owner=j.owner AND c.space=j.space
			AND c.conversation=j.conversation
		LEFT JOIN holds h ON h.space=j.space AND h.active
		WHERE h.space IS NULL
			AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.owner=j.owner
				AND r.space=j.space AND r.conversation=j.conversation
				AND r.claim_released_at IS NULL)
			AND NOT EXISTS (SELECT 1 FROM jobs prior
				JOIN fsm.machines pm ON pm.kind='job' AND pm.id=prior.id
				WHERE prior.owner=j.owner AND prior.space=j.space
					AND prior.conversation=j.conversation
					AND pm.state IN ('admitted','running'))
		ORDER BY j.created LIMIT 1
		FOR UPDATE OF c SKIP LOCKED`).Scan(
		&j.id, &j.owner, &j.space, &j.conv, &j.input, &j.idem, &j.result, &j.attach)
	if errors.Is(err, sql.ErrNoRows) {
		return j, errNoClaim
	}
	if err != nil {
		return j, fmt.Errorf("claimable: %w", err)
	}
	return j, nil
}

// insertRun claims the conversation through the partial unique index.
func insertRun(ctx context.Context, tx *sql.Tx, r runRow) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runs
		(id, job, owner, space, conversation, runtime, endpoint, digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.id, r.job, r.owner, r.space, r.conv, r.runtime, r.endpoint, r.digest)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// runRow is the run domain record.
type runRow struct {
	id       string
	job      string
	owner    string
	space    string
	conv     string
	runtime  string
	endpoint string
	digest   string
}

func getRun(ctx context.Context, db *sql.DB, id string) (runRow, error) {
	var r runRow
	err := db.QueryRowContext(ctx, `SELECT id, job, owner, space, conversation,
		runtime, endpoint, digest FROM runs WHERE id=$1`, id).Scan(
		&r.id, &r.job, &r.owner, &r.space, &r.conv, &r.runtime, &r.endpoint, &r.digest)
	if errors.Is(err, sql.ErrNoRows) {
		return r, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("run", id)
	}
	if err != nil {
		return r, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// activeRunForJob returns the job's still claimed run id.
func activeRunForJob(ctx context.Context, tx *sql.Tx, job string) (string, error) {
	var run string
	err := tx.QueryRowContext(ctx, `SELECT id FROM runs
		WHERE job=$1 AND claim_released_at IS NULL`, job).Scan(&run)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("active run: %w", err)
	}
	return run, nil
}

// releaseRun frees the indexed claim inside the final FSM transition.
func releaseRun(ctx context.Context, tx *sql.Tx, run string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE runs SET claim_released_at=now() WHERE id=$1 AND claim_released_at IS NULL", run)
	if err != nil {
		return fmt.Errorf("release run: %w", err)
	}
	return nil
}

// appendEvent writes one journal row; the run/seq pair is the idem key.
func appendEvent(ctx context.Context, tx *sql.Tx, run string, e tv1.RunEvent) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO run_events (run, seq, kind, data)
		VALUES ($1,$2,$3,$4) ON CONFLICT (run, seq) DO NOTHING`, run, e.Seq, e.Kind, e.Data)
	if err != nil {
		return false, fmt.Errorf("append event: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("event rows: %w", err)
	}
	return n == 1, nil
}

// runEvents reads the journal after a cursor position.
func runEvents(ctx context.Context, db *sql.DB, run string, from int64) ([]eventRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT seq, kind, data FROM run_events
		WHERE run=$1 AND seq>$2 ORDER BY seq`, run, from)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	defer rows.Close() //nolint:errcheck // best-effort close
	var out []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.seq, &e.kind, &e.data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

type eventRow struct {
	seq  int64
	kind string
	data string
}

// lastEventData returns the newest journal payload for a run.
func lastEventData(ctx context.Context, tx *sql.Tx, run string) (string, error) {
	var data string
	err := tx.QueryRowContext(ctx, `SELECT data FROM run_events
		WHERE run=$1 ORDER BY seq DESC LIMIT 1`, run).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("last event: %w", err)
	}
	return data, nil
}

// setHold upserts the hold row and bumps its epoch.
func setHold(ctx context.Context, db *sql.DB, space string, active bool, note string) (int64, error) {
	var epoch int64
	err := db.QueryRowContext(ctx, `INSERT INTO holds (space, active, note, epoch)
		VALUES ($1,$2,$3,1)
		ON CONFLICT (space) DO UPDATE
		SET active=$2, note=$3, epoch=holds.epoch+1
		RETURNING epoch`, space, active, note).Scan(&epoch)
	if err != nil {
		return 0, fmt.Errorf("set hold: %w", err)
	}
	return epoch, nil
}

// appendOutbox writes one effect row inside the caller's transaction —
// the hook-side counterpart of transition Effects.
func appendOutbox(ctx context.Context, tx *sql.Tx, topic string, payload []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO fsm.outbox
		(kind, machine_id, topic, payload) VALUES ('job','', $1,$2)`,
		topic, payload)
	if err != nil {
		return fmt.Errorf("outbox %q: %w", topic, err)
	}
	return nil
}

// setResult records a terminal result payload on the job row inside
// the settle transition's transaction.
func setResult(ctx context.Context, tx *sql.Tx, job, result string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE jobs SET result=$2 WHERE id=$1", job, result)
	if err != nil {
		return fmt.Errorf("set result: %w", err)
	}
	return nil
}
