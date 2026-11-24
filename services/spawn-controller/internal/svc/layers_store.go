package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

// queryer lets chain readers run inside a tx or straight on the pool.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// specDiff computes the leaf diff and the strongest declared effect
// between the live spec and a compiled candidate.
func specDiff(sp spaceRow, cand Passports, chain layerSet) ([]sv1.LeafDiff, string, error) {
	var cur Passports
	if err := json.Unmarshal(sp.spec, &cur); err != nil {
		return nil, "", fmt.Errorf("live spec decode: %w", err)
	}
	curDocs, err := specToDocs(cur)
	if err != nil {
		return nil, "", err
	}
	candDocs, err := specToDocs(cand)
	if err != nil {
		return nil, "", err
	}
	var diff []sv1.LeafDiff
	effect := "live"
	rank := map[string]int{"live": 0, "respawn": 1, "recreate": 2}
	for _, leaf := range registry.Leaves {
		newV, _ := leafValue(candDocs, leaf.Path)
		oldV, _ := leafValue(curDocs, leaf.Path)
		if reflect.DeepEqual(oldV, newV) {
			continue
		}
		diff = append(diff, sv1.LeafDiff{Path: leaf.Path, Class: leaf.Class,
			Effect: leaf.appliedEffect(), Layer: leafLayer(chain, leaf.Path),
			From: fmt.Sprint(oldV), To: fmt.Sprint(newV)})
		if rank[leaf.appliedEffect()] > rank[effect] {
			effect = leaf.appliedEffect()
		}
	}
	sort.Slice(diff, func(i, j int) bool { return diff[i].Path < diff[j].Path })
	return diff, effect, nil
}

// leafLayer reports the last layer in the chain that sets a leaf.
func leafLayer(chain layerSet, path string) string {
	layer := "platform"
	for _, l := range chain {
		if _, ok := leafValue(l.docs, path); ok {
			layer = l.role
		}
	}
	return layer
}

// specToDocs flattens a typed spec back into the leaf map shape.
func specToDocs(p Passports) (DocSet, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("spec marshal: %w", err)
	}
	var docs DocSet
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("spec decode: %w", err)
	}
	return docs, nil
}

// nextRevSeq is the next immutable revision index for (space, role).
func nextRevSeq(ctx context.Context, tx *sql.Tx, space, role string) (int, error) {
	var seq int
	err := tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0)+1
		FROM layer_revisions WHERE space=$1 AND role=$2`, space, role).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("revision seq: %w", err)
	}
	return seq, nil
}

// candidateChain builds the compile input: the latest revision per
// chain role. Every role in the registry chain must have one.
func candidateChain(ctx context.Context, q queryer, space string) (layerSet, error) {
	chain := layerSet{}
	for _, role := range registry.Chain {
		var id string
		var docs []byte
		err := q.QueryRowContext(ctx, `SELECT id, docs FROM layer_revisions
			WHERE space=$1 AND role=$2 ORDER BY seq DESC LIMIT 1`, space, role).Scan(&id, &docs)
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", role, err)
		}
		var ds DocSet
		if err := json.Unmarshal(docs, &ds); err != nil {
			return nil, fmt.Errorf("layer %s docs: %w", role, err)
		}
		chain = append(chain, namedDocs{role: role, rev: id, docs: ds})
	}
	return chain, nil
}

// chainRevisions snapshots which revision supplied each role — the
// handle rollback later restores.
func chainRevisions(chain layerSet) map[string]string {
	out := map[string]string{}
	for _, l := range chain {
		out[l.role] = l.rev
	}
	return out
}

// activationRevisions returns the {role: revision id} map of the
// activation that produced digest.
func activationRevisions(ctx context.Context, tx *sql.Tx, space, digest string) (map[string]string, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT revisions FROM activations
		WHERE space=$1 AND digest=$2 ORDER BY seq DESC LIMIT 1`,
		space, digest).Scan(&raw)
	if err != nil {
		return nil, herrors.New(herrors.CodeNotFound, "err.not-found"). //nolint:wrapcheck // shared error form
										With("digest", digest)
	}
	var revs map[string]string
	if err := json.Unmarshal(raw, &revs); err != nil {
		return nil, fmt.Errorf("activation revisions: %w", err)
	}
	return revs, nil
}

// revisionsChain rebuilds a layerSet from stored revision ids.
func revisionsChain(ctx context.Context, tx *sql.Tx, revs map[string]string) (layerSet, error) {
	chain := layerSet{}
	for _, role := range registry.Chain {
		var docs []byte
		err := tx.QueryRowContext(ctx, `SELECT docs FROM layer_revisions
			WHERE id=$1`, revs[role]).Scan(&docs)
		if err != nil {
			return nil, fmt.Errorf("revision %s: %w", revs[role], err)
		}
		var ds DocSet
		if err := json.Unmarshal(docs, &ds); err != nil {
			return nil, fmt.Errorf("revision docs: %w", err)
		}
		chain = append(chain, namedDocs{role: role, docs: ds, rev: revs[role]})
	}
	return chain, nil
}

// spaceForUpdate locks the space row — the CAS base for activation.
func spaceForUpdate(ctx context.Context, tx *sql.Tx, id string) (spaceRow, error) {
	var s spaceRow
	err := tx.QueryRowContext(ctx, `SELECT id, owner, name, template,
		spec, digest FROM spaces WHERE id=$1 FOR UPDATE`, id).Scan(
		&s.id, &s.owner, &s.name, &s.template, &s.spec, &s.digest)
	if err != nil {
		return s, fmt.Errorf("space lock: %w", err)
	}
	return s, nil
}

// ensureLayers backfills a phase-3 space into the revision world the
// first time its layers are touched: the stored spec becomes the
// platform revision, an empty user layer sits above it.
func ensureLayers(s *Server, ctx context.Context, sp spaceRow) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM activations
		WHERE space=$1`, sp.id).Scan(&n); err != nil {
		return fmt.Errorf("activations: %w", err)
	}
	if n > 0 {
		return nil
	}
	p, err := specPassports(sp)
	if err != nil {
		return err
	}
	p.normalize(s.deps.ModelURL)
	docs, err := specToDocs(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := migrateLayers(s, ctx, tx, sp, docs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// migrateLayers seeds the two initial revisions for a phase-3 space
// and activates their compiled spec.
func migrateLayers(s *Server, ctx context.Context, tx *sql.Tx, sp spaceRow, docs DocSet) error {
	at := s.deps.Now()
	for _, role := range []string{"platform", "user"} {
		rd := docs
		if role == "user" {
			rd = DocSet{}
		}
		if _, _, err := insertRevision(ctx, tx, revRow{
			space: sp.id, role: role, docs: rd, author: "migrate", at: at,
		}); err != nil {
			return err
		}
	}
	chain, err := candidateChain(ctx, tx, sp.id)
	if err != nil {
		return err
	}
	cand, err := compileSet(chain)
	if err != nil {
		return err
	}
	return applySpecTx(s, ctx, tx, sp, specApply{
		chain: chain, spec: cand.spec, digest: cand.digest,
		explain: cand.explain, effect: "live", author: "migrate",
	})
}

// latestUserDocs reads the space's current user-layer revision.
func latestUserDocs(ctx context.Context, db *sql.DB, space string) (DocSet, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT docs FROM layer_revisions
		WHERE space=$1 AND role='user' ORDER BY seq DESC LIMIT 1`, space).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("user layer: %w", err)
	}
	var docs DocSet
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("user layer docs: %w", err)
	}
	return docs, nil
}

// latestRoleDocs reads the newest revision of one role inside a tx;
// a SubmitLayers call overlays its leaves on this base.
func latestRoleDocs(ctx context.Context, tx *sql.Tx, space, role string) (DocSet, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT docs FROM layer_revisions
		WHERE space=$1 AND role=$2 ORDER BY seq DESC LIMIT 1`, space, role).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DocSet{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("role layer: %w", err)
	}
	var docs DocSet
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("role layer docs: %w", err)
	}
	return docs, nil
}

// revRow is one immutable revision's insert payload.
type revRow struct {
	space  string
	role   string
	docs   DocSet
	author string
	at     time.Time
}

// insertRevision writes one immutable revision row.
func insertRevision(ctx context.Context, tx *sql.Tx, r revRow) (string, int, error) {
	raw, err := json.Marshal(r.docs)
	if err != nil {
		return "", 0, fmt.Errorf("docs marshal: %w", err)
	}
	seq, err := nextRevSeq(ctx, tx, r.space, r.role)
	if err != nil {
		return "", 0, err
	}
	rev := newID("rev")
	_, err = tx.ExecContext(ctx, `INSERT INTO layer_revisions
		(id, space, role, seq, docs, author, created) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		rev, r.space, r.role, seq, raw, r.author, r.at)
	if err != nil {
		return "", 0, fmt.Errorf("revision: %w", err)
	}
	return rev, seq, nil
}

// insertSpec stores the compiled effective spec keyed by digest.
func insertSpec(ctx context.Context, tx *sql.Tx, space string, a specApply, at time.Time) error {
	raw, err := json.Marshal(a.spec)
	if err != nil {
		return fmt.Errorf("spec marshal: %w", err)
	}
	ex, err := json.Marshal(a.explain)
	if err != nil {
		return fmt.Errorf("explain marshal: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO effective_specs
		(space, digest, spec, explain, created) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (space, digest) DO NOTHING`,
		space, a.digest, raw, ex, at)
	if err != nil {
		return fmt.Errorf("effective spec: %w", err)
	}
	return nil
}

// insertActivation logs the activation with its revision set.
func insertActivation(ctx context.Context, tx *sql.Tx, space string, a specApply, at time.Time) error {
	revs, err := json.Marshal(chainRevisions(a.chain))
	if err != nil {
		return fmt.Errorf("revisions marshal: %w", err)
	}
	var seq int
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0)+1
		FROM activations WHERE space=$1`, space).Scan(&seq); err != nil {
		return fmt.Errorf("activation seq: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO activations
		(id, space, seq, digest, revisions, effect, author, created)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		newID("act"), space, seq, a.digest, revs, a.effect, a.author, at)
	if err != nil {
		return fmt.Errorf("activation: %w", err)
	}
	return nil
}

// resetEffect is the outbox payload that wipes a space's rendered dir.
type resetEffect struct {
	Space string `json:"space"`
}
