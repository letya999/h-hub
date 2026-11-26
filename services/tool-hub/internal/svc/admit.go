package svc

import (
	"context"
	"slices"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// admission is one decided call: the bound run claims, the ledger
// receipt and the verdict.
type admission struct {
	claims     idv1.RunClaims
	receipt    thv1.Receipt
	allowed    bool
	specDigest string
}

// admit decides one call and writes its receipt: the run token binds
// the caller to one run and one spec digest, the current effective
// spec supplies the grants, and the registry says which capability
// the tool needs. The decision reads first and judges after — never
// under a lock.
func (s *Server) admit(ctx context.Context, run, digest,
	tsName, name string) (admission, error) {
	claims, err := s.runClaims(ctx, run, digest)
	if err != nil {
		return admission{}, err
	}
	spec, specDigest, err := s.spec(ctx, claims.Space)
	if err != nil {
		return admission{}, err
	}
	selected, err := selectedToolsets(spec.Tools)
	if err != nil {
		return admission{}, err
	}
	grants := map[string]bool{}
	for _, g := range spec.Grants.Allow {
		grants[g] = true
	}
	allowed, caps, reason := decide(selected, grants, tsName, name)
	receipt, err := s.record(ctx, receiptRow{
		space: claims.Space, run: claims.Run, job: claims.Job,
		reqDigest: digest, specDigest: specDigest,
		toolset: tsName, tool: name, caps: caps,
		allowed: allowed, reason: reason})
	if err != nil {
		return admission{}, err
	}
	return admission{claims: claims, receipt: receipt, allowed: allowed, specDigest: specDigest}, nil
}

// runClaims binds the run token to the request: the token must name
// this run and digest and carry tool-hub in its audience.
func (s *Server) runClaims(ctx context.Context, run, digest string) (idv1.RunClaims, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if !slices.Contains(p.Aud, s.sub) {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	c, err := idv1.ParseRunClaims(p.Payload)
	if err != nil || c.Run != run || c.Digest != digest {
		return idv1.RunClaims{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return c, nil
}
