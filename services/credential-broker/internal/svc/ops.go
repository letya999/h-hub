package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// sealPurpose and kid bind the envelope ciphertext to the broker's
// credential role: a blob copied anywhere else will not open.
const sealPurpose = "credential"

// handlers implements the contract's operation table.
func (s *Server) handlers() credv1.Handlers {
	return credv1.Handlers{
		Acquire:         s.acquire,
		Revoke:          s.revoke,
		ListConnections: s.list,
		SetSecret:       s.setSecret,
		Snapshot:        s.snapshot,
	}
}

func actorOf(ctx context.Context) string {
	if p := httpapi.PrincipalFrom(ctx); p != nil && p.Sub != "" {
		return p.Sub
	}
	return "unknown"
}

// acquire discloses one credential's current version under a lease
// shorter than two minutes; the audit row lands in the same
// transaction as the version read.
func (s *Server) acquire(ctx context.Context, r credv1.AcquireReq) (credv1.AcquireResp, error) {
	var out credv1.AcquireResp
	if _, err := refName(r.Ref); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	cv, err := currentVersion(ctx, tx, r.Ref)
	if err != nil {
		return out, err
	}
	plain, err := s.deps.Sealer.Open(sealPurpose, cv.kid, cv.blob)
	if err != nil {
		return out, fmt.Errorf("open %s: %w", r.Ref, err)
	}
	var fields map[string]string
	if err := json.Unmarshal(plain, &fields); err != nil {
		return out, fmt.Errorf("fields %s v%d: %w", r.Ref, cv.v, err)
	}
	id, exp := s.leases.issue(r.Ref, actorOf(ctx), s.deps.Now())
	if err := audit(ctx, tx, auditEvent{
		actor: actorOf(ctx), event: "acquire", ref: r.Ref, detail: id}); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit: %w", err)
	}
	return credv1.AcquireResp{Lease: id, ExpiresAt: exp, Fields: fields}, nil
}

func (s *Server) revoke(ctx context.Context, r credv1.RevokeReq) (credv1.RevokeResp, error) {
	l, ok := s.leases.take(r.Lease, s.deps.Now())
	if !ok {
		return credv1.RevokeResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return credv1.RevokeResp{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	if err := audit(ctx, tx, auditEvent{
		actor: actorOf(ctx), event: "revoke", ref: l.ref, detail: r.Lease}); err != nil {
		return credv1.RevokeResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return credv1.RevokeResp{}, fmt.Errorf("commit: %w", err)
	}
	return credv1.RevokeResp{OK: true}, nil
}

func (s *Server) list(ctx context.Context, _ credv1.ListConnectionsReq) (credv1.ListConnectionsResp, error) {
	rows, err := listConnections(ctx, s.db)
	if err != nil {
		return credv1.ListConnectionsResp{}, err
	}
	out := credv1.ListConnectionsResp{}
	for _, r := range rows {
		out.Connections = append(out.Connections, credv1.Connection{
			Name: r.name, Type: r.typ, Ref: r.ref, Version: r.version})
	}
	return out, nil
}

// setSecret creates the credential or appends a rotated version.
// Version history keeps every sealed blob; readers always get the
// newest one.
func (s *Server) setSecret(ctx context.Context, r credv1.SetSecretReq) (credv1.SetSecretResp, error) {
	var out credv1.SetSecretResp
	if r.Value == "" {
		return out, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	ref, err := credRef(r.Name)
	if err != nil {
		return out, err
	}
	fields, err := json.Marshal(map[string]string{"value": r.Value})
	if err != nil {
		return out, fmt.Errorf("fields: %w", err)
	}
	blob, err := s.deps.Sealer.Seal(sealPurpose, ref, fields)
	if err != nil {
		return out, fmt.Errorf("seal %s: %w", ref, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close
	ver, err := putVersion(ctx, tx, putReq{
		ref: ref, name: r.Name, typ: "api-key", blob: blob, kid: ref})
	if err != nil {
		return out, err
	}
	if err := audit(ctx, tx, auditEvent{
		actor: actorOf(ctx), event: "set", ref: ref}); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit: %w", err)
	}
	return credv1.SetSecretResp{Ref: ref, Version: ver}, nil
}

// snapshot writes a consistent encrypted copy inside the volume via
// VACUUM INTO; the backup job copies the sealed file out. VACUUM INTO
// takes no bound parameter, so the server-owned path goes in as a
// quoted literal.
func (s *Server) snapshot(ctx context.Context, _ credv1.SnapshotReq) (credv1.SnapshotResp, error) {
	dst := filepath.Join(filepath.Dir(s.deps.DBPath), "broker-snapshot.db")
	if strings.Contains(dst, `'`) {
		return credv1.SnapshotResp{}, fmt.Errorf("snapshot path %q: invalid", dst)
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return credv1.SnapshotResp{}, fmt.Errorf("snapshot clear: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO '`+dst+`'`); err != nil {
		return credv1.SnapshotResp{}, fmt.Errorf("snapshot: %w", err)
	}
	return credv1.SnapshotResp{Path: dst}, nil
}
