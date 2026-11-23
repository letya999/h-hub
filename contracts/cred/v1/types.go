// Package v1 is the credential-broker contract: short-lived leases of
// stored credential objects and the operator write path (blueprint 2.9).
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// AcquireReq asks for a short lease on one credential reference. The
// verified caller identity (service or operator sub) is taken from the
// token, not the body.
type AcquireReq struct {
	Ref string `json:"ref"`
}

// AcquireResp is the disclosed credential plus its lease. Fields is a
// credential-type-owned map (e.g. "value" for an api-key); callers
// must not log or persist it.
type AcquireResp struct {
	Lease     string            `json:"lease"`
	ExpiresAt int64             `json:"expires_at"`
	Fields    map[string]string `json:"fields"`
}

// RevokeReq ends a lease early; the caller keeps no copy.
type RevokeReq struct {
	Lease string `json:"lease"`
}
type RevokeResp struct {
	OK bool `json:"ok"`
}

// Connection is one registered credential object (never its value).
type Connection struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Ref     string `json:"ref"`
	Version int    `json:"version"`
}

type ListConnectionsReq struct{}
type ListConnectionsResp struct {
	Connections []Connection `json:"connections"`
}

// SetSecretReq creates a credential object or adds a new version
// (rotation). The value travels in the request body only.
type SetSecretReq struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type SetSecretResp struct {
	Ref     string `json:"ref"`
	Version int    `json:"version"`
}

// SnapshotReq asks the broker to VACUUM INTO a snapshot file inside
// its own volume; the backup job copies the sealed bytes out.
type SnapshotReq struct{}
type SnapshotResp struct {
	Path string `json:"path"`
}

type Handlers struct {
	Acquire         func(context.Context, AcquireReq) (AcquireResp, error)
	Revoke          func(context.Context, RevokeReq) (RevokeResp, error)
	ListConnections func(context.Context, ListConnectionsReq) (ListConnectionsResp, error)
	SetSecret       func(context.Context, SetSecretReq) (SetSecretResp, error)
	Snapshot        func(context.Context, SnapshotReq) (SnapshotResp, error)
}

func Ops(h Handlers) map[string]httpapi.Handler {
	return map[string]httpapi.Handler{
		"Acquire":         op(h.Acquire),
		"Revoke":          op(h.Revoke),
		"ListConnections": op(h.ListConnections),
		"SetSecret":       op(h.SetSecret),
		"Snapshot":        op(h.Snapshot),
	}
}

func op[Req, Resp any](fn func(context.Context, Req) (Resp, error)) httpapi.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req Req
		if err := httpapi.Decode(r, &req); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		resp, err := fn(r.Context(), req)
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, resp)
		return nil
	}
}
