package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

type nowClock struct{ now func() int64 }

func (c nowClock) Now() time.Time { return time.Unix(c.now(), 0) }

func checkOperator(ctx context.Context, owner, space, operation string) error {
	c, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return fmt.Errorf("operator scope: %w", err)
	}
	if c.Owner != owner || c.Space != space || c.Target != operation {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return nil
}

func (s *Server) createPrincipal(ctx context.Context, r idv1.CreatePrincipalReq) (idv1.CreatePrincipalResp, error) {
	if r.ID == "" || r.Space == "" {
		return idv1.CreatePrincipalResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := checkOperator(ctx, r.ID, r.Space, "CreatePrincipal"); err != nil {
		return idv1.CreatePrincipalResp{}, err
	}
	exists, err := s.principalExists(ctx, r)
	if err != nil {
		return idv1.CreatePrincipalResp{}, err
	}
	if exists {
		return idv1.CreatePrincipalResp{ID: r.ID}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return idv1.CreatePrincipalResp{}, fmt.Errorf("principal begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err = tx.ExecContext(ctx, `INSERT INTO principals(id,space) VALUES($1,$2)`, r.ID, r.Space); err != nil {
		return idv1.CreatePrincipalResp{}, fmt.Errorf("principal insert: %w", err)
	}
	if err = principalMachine().Create(ctx, tx, nowClock{s.now}, fsm.CreateReq{ID: r.ID, Initial: "active", Actor: "operator"}); err != nil {
		return idv1.CreatePrincipalResp{}, fmt.Errorf("create principal state: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return idv1.CreatePrincipalResp{}, fmt.Errorf("principal commit: %w", err)
	}
	return idv1.CreatePrincipalResp{ID: r.ID}, nil
}

func (s *Server) principalExists(ctx context.Context, r idv1.CreatePrincipalReq) (bool, error) {
	var space, state string
	err := s.db.QueryRowContext(ctx, `SELECT p.space, m.state FROM principals p JOIN fsm.machines m
		ON m.kind='principal' AND m.id=p.id WHERE p.id=$1`, r.ID).Scan(&space, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("principal lookup: %w", err)
	}
	if space != r.Space || state != "active" {
		return false, herrors.New(herrors.CodeConflict, "err.conflict")
	}
	return true, nil
}

func (s *Server) deactivateHuman(ctx context.Context, r idv1.DeactivateHumanReq) (idv1.DeactivateHumanResp, error) {
	if r.ID == "" {
		return idv1.DeactivateHumanResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := checkOperator(ctx, r.ID, "", "DeactivateHuman"); err != nil {
		return idv1.DeactivateHumanResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return idv1.DeactivateHumanResp{}, fmt.Errorf("disable begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := principalMachine().Transition(ctx, tx, fsm.Request{ID: r.ID, Event: "disable", Actor: "operator"}, nil); err != nil {
		return idv1.DeactivateHumanResp{}, fmt.Errorf("disable principal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return idv1.DeactivateHumanResp{}, fmt.Errorf("disable commit: %w", err)
	}
	return idv1.DeactivateHumanResp{OK: true}, nil
}
