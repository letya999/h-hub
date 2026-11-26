package v1

import (
	"context"
)

// WorkspaceReq carries the admitted file call to the owner of the workspace.
type WorkspaceReq struct {
	Owner     string `json:"owner"`
	Space     string `json:"space"`
	Digest    string `json:"digest"`
	Key       string `json:"key"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Text      string `json:"text,omitempty"`
	Before    string `json:"before,omitempty"`
	Query     string `json:"query,omitempty"`
}

type WorkspaceResp struct {
	Entries []string `json:"entries,omitempty"`
	Text    string   `json:"text,omitempty"`
	OK      bool     `json:"ok"`
}

func (c *Client) Workspace(ctx context.Context, req WorkspaceReq) (WorkspaceResp, error) {
	var out WorkspaceResp
	err := c.call(ctx, "Workspace", req, &out)
	return out, err
}
