package svc

import (
	"context"
	"database/sql"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
)

func drainRuntimeOp(s *Server, ctx context.Context, r sv1.DrainRuntimeReq) (sv1.DrainRuntimeResp, error) {
	scope, err := idv1.ScopeFrom(ctx)
	if err != nil {
		return sv1.DrainRuntimeResp{}, fmt.Errorf("scope: %w", err)
	}
	if r.Space == "" || scope.Space != r.Space {
		return sv1.DrainRuntimeResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if _, err := getSpace(ctx, s.db, scope.Owner, r.Space); err != nil {
		return sv1.DrainRuntimeResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sv1.DrainRuntimeResp{}, fmt.Errorf("drain begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	state, err := drainRuntimeTx(s, ctx, tx, r.Space, "operator")
	if err != nil {
		return sv1.DrainRuntimeResp{}, err
	}
	if err := tx.Commit(); err != nil {
		return sv1.DrainRuntimeResp{}, fmt.Errorf("drain commit: %w", err)
	}
	return sv1.DrainRuntimeResp{State: state}, nil
}

func drainRuntimeTx(s *Server, ctx context.Context, tx *sql.Tx, space, actor string) (string, error) {
	rt, ok, err := runtimeForSpace(ctx, tx, space)
	if err != nil {
		return "", err
	}
	if !ok {
		return "absent", nil
	}
	// The last job can still be releasing the runtime when the next passport is activated.
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM fsm.machines
		WHERE kind='runtime' AND id=$1 FOR UPDATE`, rt.id).Scan(&state); err != nil {
		return "", fmt.Errorf("lock for drain: %w", err)
	}
	if state == "absent" || state == "draining" && actor == "passport" {
		return state, nil
	}
	if err := s.runM.Transition(ctx, tx, fsm.Request{ID: rt.id, Event: "drain", Actor: actor,
		At: s.deps.Now(), Effects: []fsm.Effect{{Topic: "docker-rm",
			Payload: mustJSON(rmEffect{Runtime: rt.id, Container: rt.container})}}}, nil); err != nil {
		return "", fmt.Errorf("drain runtime: %w", err)
	}
	return "draining", nil
}
