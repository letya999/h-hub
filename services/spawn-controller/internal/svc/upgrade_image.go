package svc

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
)

var executablePin = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (s *Server) checkImagePatch(docs DocSet) error {
	raw, changed := leafValue(docs, "stack.image")
	if !changed {
		return nil
	}
	pin, valid := raw.(string)
	if !valid {
		return s.requireImage("")
	}
	return s.requireImage(pin)
}

// spaceForRun upgrades a pre-Hermes platform revision before granting a runtime.
// A refused passport affects only the space that owns it.
func (s *Server) spaceForRun(ctx context.Context, space string) (spaceRow, error) {
	sp, err := getSpaceAny(ctx, s.db, space)
	if err != nil {
		return spaceRow{}, err
	}
	p, err := specPassports(sp)
	if err != nil {
		return spaceRow{}, err
	}
	if !executablePin.MatchString(p.Stack.Image) {
		return sp, nil
	}
	if err := ensureLayers(s, ctx, sp); err != nil {
		return spaceRow{}, err
	}
	if err := s.upgradeImage(ctx, space); err != nil {
		return spaceRow{}, err
	}
	return getSpaceAny(ctx, s.db, space)
}

func (s *Server) upgradeImage(ctx context.Context, space string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin image upgrade: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // closing an already committed transaction
	cur, err := spaceForUpdate(ctx, tx, space)
	if err != nil {
		return err
	}
	if err := s.upgradeImageLayers(ctx, tx, cur); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit image upgrade: %w", err)
	}
	return nil
}

func (s *Server) upgradeImageLayers(ctx context.Context, tx *sql.Tx, cur spaceRow) error {
	space := cur.id
	revs, err := activationRevisions(ctx, tx, space, cur.digest)
	if err != nil {
		return err
	}
	active, err := revisionsChain(ctx, tx, revs)
	if err != nil {
		return err
	}
	if image, _ := leafValue(active[0].docs, "stack.image"); image == s.deps.Hermes {
		return nil
	}
	pinned, err := hasPinnedImage(ctx, tx, space)
	if err != nil {
		return err
	}
	if pinned {
		return s.requireImage("")
	}
	pending, err := candidateChain(ctx, tx, space)
	if err != nil {
		return err
	}
	if err := s.pinLayer(ctx, tx, space, &active[0]); err != nil {
		return err
	}
	if pending[0].rev != revs["platform"] {
		if err := s.pinLayer(ctx, tx, space, &pending[0]); err != nil {
			return err
		}
	}
	c, err := compileSet(active)
	if err != nil {
		return err
	}
	if err := applySpecTx(s, ctx, tx, cur, specApply{chain: active, spec: c.spec, digest: c.digest,
		explain: c.explain, effect: "respawn", author: "hermes-image"}); err != nil {
		return err
	}
	return applyEffectTx(s, ctx, tx, cur, "respawn")
}

func hasPinnedImage(ctx context.Context, tx *sql.Tx, space string) (bool, error) {
	var pinned bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM effective_specs WHERE space=$1
		AND spec #>> '{stack,image}' LIKE 'docker.io/nousresearch/hermes-agent:%@sha256:%')`, space).Scan(&pinned); err != nil {
		return false, fmt.Errorf("check space image history: %w", err)
	}
	return pinned, nil
}

func (s *Server) pinLayer(ctx context.Context, tx *sql.Tx, space string, layer *namedDocs) error {
	if err := upgradeBaseMemory(ctx, tx, layer); err != nil {
		return err
	}
	setLeaf(layer.docs, "stack.image", s.deps.Hermes)
	rev, _, err := insertRevision(ctx, tx, revRow{space: space, role: "platform",
		docs: layer.docs, at: s.deps.Now(), author: "hermes-image"})
	layer.rev = rev
	return err
}

// Only the original shipped layer proves that the old memory ceiling was a default.
func upgradeBaseMemory(ctx context.Context, tx *sql.Tx, layer *namedDocs) error {
	if mem, _ := leafValue(layer.docs, "stack.mem"); mem != "256m" {
		return nil
	}
	var stock bool
	if err := tx.QueryRowContext(ctx, `SELECT seq=1 AND author='create' FROM layer_revisions
		WHERE id=$1 AND role='platform'`, layer.rev).Scan(&stock); err != nil {
		return fmt.Errorf("read platform revision origin: %w", err)
	}
	if !stock {
		return nil
	}
	base, err := layerDocs("platform/layer")
	if err != nil {
		return err
	}
	mem, ok := leafValue(base, "stack.mem")
	if !ok {
		return fmt.Errorf("shipped platform layer requires stack.mem")
	}
	setLeaf(layer.docs, "stack.mem", mem)
	return nil
}
