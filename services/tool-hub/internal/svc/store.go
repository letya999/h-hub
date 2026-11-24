package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
)

// receiptRow is one admission decision written to the ledger. Every
// allow and every deny lands a row — the receipt is the audit.
type receiptRow struct {
	space, run, job       string
	reqDigest, specDigest string
	toolset, tool         string
	caps                  []string
	allowed               bool
	reason                string
}

// record appends the decision to the ledger and shapes the receipt
// returned to the caller.
func (s *Server) record(ctx context.Context, r receiptRow) (thv1.Receipt, error) {
	caps, err := json.Marshal(r.caps)
	if err != nil {
		return thv1.Receipt{}, fmt.Errorf("receipt caps: %w", err)
	}
	var seq int64
	var at = s.deps.Now()
	err = s.db.QueryRowContext(ctx, `INSERT INTO receipts
		(space, run, job, req_digest, spec_digest, toolset, tool, caps, allowed, reason, created)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING seq`,
		r.space, r.run, r.job, r.reqDigest, r.specDigest,
		r.toolset, r.tool, caps, r.allowed, r.reason, at.UTC()).Scan(&seq)
	if err != nil {
		return thv1.Receipt{}, fmt.Errorf("receipt insert: %w", err)
	}
	return thv1.Receipt{ID: fmt.Sprintf("rcpt_%d", seq), At: at, Caps: r.caps}, nil
}

// cachedProjection reads the projection computed for digest, if one
// was stored when that spec was resolved.
func (s *Server) cachedProjection(ctx context.Context, digest string) (thv1.ProjectionResp, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT projection FROM projection_cache WHERE digest=$1`, digest).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return thv1.ProjectionResp{}, false, nil
	}
	if err != nil {
		return thv1.ProjectionResp{}, false, fmt.Errorf("projection cache read: %w", err)
	}
	var p thv1.ProjectionResp
	if err := json.Unmarshal(raw, &p); err != nil {
		return thv1.ProjectionResp{}, false, fmt.Errorf("projection cache decode: %w", err)
	}
	return p, true, nil
}

// cacheProjection stores the projection under its spec digest; the
// row is a pure function of the spec and never needs updating.
func (s *Server) cacheProjection(ctx context.Context, space, digest string,
	p thv1.ProjectionResp) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("projection marshal: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO projection_cache
		(digest, space, projection) VALUES ($1,$2,$3)
		ON CONFLICT (digest) DO NOTHING`, digest, space, raw)
	if err != nil {
		return fmt.Errorf("projection cache write: %w", err)
	}
	return nil
}
