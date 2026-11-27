package svc

import (
	"context"

	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func (o *jobOps) getRunSession(ctx context.Context, r tv1.RunSessionReq) (tv1.RunSessionResp, error) {
	var out tv1.RunSessionResp
	if err := r.Validate(); err != nil {
		return out, err //nolint:wrapcheck // contract error
	}
	row, err := getRun(ctx, o.s.db, r.Run)
	if err != nil {
		return out, err
	}
	if row.job != r.Job || row.owner != r.Owner || row.space != r.Space {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	out.Key = row.conversationKey()
	return out, nil
}
