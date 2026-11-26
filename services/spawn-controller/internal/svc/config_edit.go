package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"math"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

type reviewedSetting struct {
	path             string
	minimum, maximum float64
}

var reviewedSettings = map[string]reviewedSetting{
	"timeout_ms": {path: "config.timeout_ms", minimum: 1000, maximum: 300000},
}

func (setting reviewedSetting) validate(value any) error {
	n, ok := value.(float64)
	if !ok || n != math.Trunc(n) || n < setting.minimum || n > setting.maximum {
		return herrors.New(herrors.CodeInvalid, "err.config-value").With("leaf", setting.path) //nolint:wrapcheck // shared error form
	}
	return nil
}

func validateReviewedConfig(docs DocSet) error {
	for _, setting := range reviewedSettings {
		if v, ok := leafValue(docs, setting.path); ok {
			if err := setting.validate(v); err != nil {
				return err
			}
		}
	}
	return nil
}

func reviewedConfigPatch(key string, raw json.RawMessage) (DocSet, error) {
	setting, ok := reviewedSettings[key]
	if !ok {
		return nil, herrors.New(herrors.CodeForbidden, "err.config-setting").With("key", key) //nolint:wrapcheck // shared error form
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.config-value", err)
	}
	if err := setting.validate(value); err != nil {
		return nil, err
	}
	docs := DocSet{}
	setLeaf(docs, setting.path, value)
	return docs, nil
}

func editConfig(s *Server, ctx context.Context, r sv1.EditConfigReq) (sv1.ActivateResp, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil || p.Purpose != "service" || p.Sub != "tool-hub" {
		return sv1.ActivateResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if r.Key == "" || r.Digest == "" {
		return sv1.ActivateResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	patch, err := reviewedConfigPatch(r.Setting, r.Value)
	if err != nil {
		return sv1.ActivateResp{}, err
	}
	return applyConfigEdit(s, ctx, r, patch)
}

func applyConfigEdit(s *Server, ctx context.Context, r sv1.EditConfigReq, patch DocSet) (sv1.ActivateResp, error) {
	var out sv1.ActivateResp
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("config edit begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	space, err := spaceForUpdate(ctx, tx, r.Space)
	if err != nil {
		return out, err
	}
	if space.owner != r.Owner || space.digest != r.Digest {
		return out, staleErr("digest", r.Digest)
	}
	cand, chain, err := configCandidate(s, ctx, tx, space, patch)
	if err != nil {
		return out, err
	}
	effect, err := applyCompiled(s, ctx, tx, space, specApply{
		chain: chain, spec: cand.spec, digest: cand.digest, explain: cand.explain, author: "tool-hub",
	})
	if err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("config edit commit: %w", err)
	}
	return sv1.ActivateResp{Digest: cand.digest, Effect: effect}, nil
}

func configCandidate(s *Server, ctx context.Context, tx *sql.Tx, space spaceRow, patch DocSet) (compiled, layerSet, error) {
	active, err := activationRevisions(ctx, tx, space.id, space.digest)
	if err != nil {
		return compiled{}, nil, err
	}
	chain, err := candidateChain(ctx, tx, space.id)
	if err != nil {
		return compiled{}, nil, err
	}
	if !maps.Equal(active, chainRevisions(chain)) {
		return compiled{}, nil, herrors.New(herrors.CodeConflict, "err.config-pending")
	}
	if _, _, err := storeRevision(s, ctx, tx, revRow{space: space.id, role: "user",
		author: "tool-hub", at: s.deps.Now()}, patch); err != nil {
		return compiled{}, nil, err
	}
	chain, err = candidateChain(ctx, tx, space.id)
	if err != nil {
		return compiled{}, nil, err
	}
	cand, err := compileSet(chain)
	return cand, chain, err
}
