package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

type Key struct {
	Owner, Space, Profile, Digest string
}

func (k Key) id() string {
	return fmt.Sprintf("worker-%x", sha256.Sum256([]byte(strings.Join(
		[]string{k.Owner, k.Space, k.Profile, k.Digest}, "\x00"))))
}

// Claim comes from the authenticated space and an approved executor profile.
type Claim struct {
	Key
	Image, Lease string
}

func (c Claim) valid() bool {
	for _, field := range []string{c.Owner, c.Space, c.Profile, c.Digest, c.Image, c.Lease} {
		if field == "" || strings.ContainsRune(field, 0) {
			return false
		}
	}
	return true
}

// Record joins a worker's machine with the registered generation.
type Record struct {
	Key
	ID, Generation, Image, Lease, State, StopReason string
	Born                                            time.Time
	Slot                                            int
	deadline                                        sql.NullTime
}

// OwnsGeneration reports whether a generation is still registered for cleanup.
func (w Record) OwnsGeneration(generation string) bool {
	return w.State != absent && w.Generation == generation
}

func (w Record) Ready() bool { return w.State == leased }

func (w Record) holdsLease() bool { return w.State == starting || w.Ready() }

type Registry struct {
	db      *sql.DB
	machine *fsm.Machine
	now     func() time.Time
	limits  wv1.Limits
}

func New(db *sql.DB, now func() time.Time, limits wv1.Limits) *Registry {
	return &Registry{db: db, machine: machine(), now: now, limits: limits}
}

const projection = `SELECT i.id,i.owner,i.space,i.profile,i.digest,i.runtime,i.image,i.lease,
	i.stop_reason,i.created,coalesce(i.worker_slot,0),m.state,m.deadline_at
	FROM intents i JOIN fsm.machines m ON m.kind=i.kind AND m.id=i.id
	WHERE i.kind='worker' AND i.id=$1`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Record, error) {
	var w Record
	err := row.Scan(&w.ID, &w.Owner, &w.Space, &w.Profile, &w.Digest,
		&w.Generation, &w.Image, &w.Lease, &w.StopReason, &w.Born, &w.Slot, &w.State, &w.deadline)
	if err != nil {
		return Record{}, fmt.Errorf("read worker: %w", err)
	}
	return w, nil
}

// Lookup is used by the lifecycle executor to verify a durable intent.
func (r *Registry) Lookup(ctx context.Context, id string) (Record, error) {
	return scan(r.db.QueryRowContext(ctx, projection, id))
}

func lock(ctx context.Context, tx *sql.Tx, id string) (Record, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, "SELECT version FROM fsm.machines WHERE kind='worker' AND id=$1 FOR UPDATE",
		id).Scan(&version); err != nil {
		return Record{}, fmt.Errorf("lock worker: %w", err)
	}
	return scan(tx.QueryRowContext(ctx, projection, id))
}

// Reserve uses the caller's transaction so scope and effective spec can be
// checked under the owning space's row lock.
func (r *Registry) Reserve(ctx context.Context, tx *sql.Tx, c Claim) (Record, error) {
	if !c.valid() {
		return Record{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := r.ensureKey(ctx, tx, c.Key); err != nil {
		return Record{}, err
	}
	w, err := lock(ctx, tx, c.id())
	if err != nil {
		return Record{}, err
	}
	switch {
	case w.State == absent:
		return r.claim(ctx, tx, w, c)
	case w.Image != c.Image:
		return Record{}, herrors.New(herrors.CodeCapacity, "err.worker-layout-busy")
	case w.State == idle && w.currentAt(r.now()):
		return r.acquire(ctx, tx, w, c.Lease)
	case w.holdsLease() && w.Lease == c.Lease && w.currentAt(r.now()):
		return w, nil
	default:
		return Record{}, claimConflict(w.State)
	}
}

func claimConflict(state string) error {
	return herrors.New(herrors.CodeConflict, "err.machine-state").With("state", state) //nolint:wrapcheck // shared error form
}

func (w Record) currentAt(now time.Time) bool {
	return w.deadline.Valid && now.Before(w.deadline.Time)
}

func (r *Registry) ensureKey(ctx context.Context, tx *sql.Tx, key Key) error {
	id := key.id()
	res, err := tx.ExecContext(ctx, `INSERT INTO intents (id,owner,space,profile,digest,runtime,image,kind)
		VALUES ($1,$2,$3,$4,$5,$1,'','worker') ON CONFLICT (id) DO NOTHING`,
		id, key.Owner, key.Space, key.Profile, key.Digest)
	if err != nil {
		return fmt.Errorf("insert worker key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert worker key rows: %w", err)
	}
	if n == 0 {
		return nil
	}
	if err := r.machine.Create(ctx, tx, funcClock{r.now}, fsm.CreateReq{ID: id, Initial: absent, Actor: "claim"}); err != nil {
		return fmt.Errorf("create worker machine: %w", err)
	}
	return nil
}

type funcClock struct{ read func() time.Time }

func (c funcClock) Now() time.Time { return c.read() }

func (r *Registry) claim(ctx context.Context, tx *sql.Tx, w Record, c Claim) (Record, error) {
	if err := r.checkCapacity(ctx, tx, c.Key); err != nil {
		return Record{}, err
	}
	slot, err := availableSlot(ctx, tx, r.limits.MaxTotal)
	if err != nil {
		return Record{}, err
	}
	w.Slot = slot
	w.Generation = "hhub-worker-" + strings.ToLower(rand.Text())
	w.Image, w.Lease, w.Born, w.State, w.StopReason = c.Image, c.Lease, r.now(), starting, ""
	_, err = tx.ExecContext(ctx, `UPDATE intents SET runtime=$2,image=$3,lease=$4,created=$5,worker_slot=$6,stop_reason=''
		WHERE id=$1 AND kind='worker'`, w.ID, w.Generation, w.Image, w.Lease, w.Born, w.Slot)
	if err != nil {
		return Record{}, fmt.Errorf("record worker intent: %w", err)
	}
	req, err := effectRequest(w, "claim", "api", LaunchTopic)
	if err != nil {
		return Record{}, err
	}
	req.At = w.Born
	if err := r.machine.Transition(ctx, tx, req, nil); err != nil {
		return Record{}, fmt.Errorf("claim worker: %w", err)
	}
	return w, nil
}

func (r *Registry) acquire(ctx context.Context, tx *sql.Tx, w Record, lease string) (Record, error) {
	if _, err := tx.ExecContext(ctx, "UPDATE intents SET lease=$2 WHERE id=$1", w.ID, lease); err != nil {
		return Record{}, fmt.Errorf("replace worker lease: %w", err)
	}
	w.Lease, w.State = lease, leased
	if err := r.transition(ctx, tx, w, "acquire", leaseTTL); err != nil {
		return Record{}, err
	}
	return w, nil
}

func (r *Registry) transition(ctx context.Context, tx *sql.Tx, w Record, event string, ttl time.Duration) error {
	req := fsm.Request{ID: w.ID, Event: event, Actor: "controller",
		At: minTime(r.now(), w.Born.Add(maxAge-ttl))}
	if err := r.machine.Transition(ctx, tx, req, nil); err != nil {
		return fmt.Errorf("worker %s: %w", event, err)
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func effectRequest(w Record, event, actor, topic string) (fsm.Request, error) {
	payload, err := json.Marshal(w.Generation)
	if err != nil {
		return fsm.Request{}, fmt.Errorf("worker generation: %w", err)
	}
	return fsm.Request{ID: w.ID, Event: event, Actor: actor,
		Effects: []fsm.Effect{{Topic: topic, Payload: payload}}}, nil
}
