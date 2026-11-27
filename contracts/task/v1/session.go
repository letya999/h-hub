package v1

import (
	"context"

	herrors "github.com/letya999/h-hub/shared/errors"
)

type RunSessionReq struct {
	Run   string `json:"run"`
	Job   string `json:"job"`
	Owner string `json:"owner"`
	Space string `json:"space"`
}

type RunSessionResp struct {
	Key string `json:"key"`
}

func (r RunSessionReq) Validate() error {
	return herrors.Require(r.Run != "" && r.Job != "" && r.Owner != "" && r.Space != "", "err.missing-input") //nolint:wrapcheck // contract error
}

// GetRunSession resolves the durable conversation bound to a run.
func (c *Client) GetRunSession(ctx context.Context, r RunSessionReq) (RunSessionResp, error) {
	var out RunSessionResp
	err := c.call(ctx, "GetRunSession", r, &out)
	return out, err
}
