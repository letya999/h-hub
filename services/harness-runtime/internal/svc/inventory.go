package svc

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"

	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
)

// Hermes supports a configured whitelist and a final model-request inventory.
const controlLevel = "B"

func approvedTools(p thv1.ProjectionResp) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range p.Native {
		out[name] = ""
	}
	for _, tool := range append(slices.Clone(p.Core), p.Extended...) {
		out[tool.Name] = tool.Toolset
	}
	if len(out) != len(p.Core)+len(p.Extended)+len(p.Native) {
		return nil, fmt.Errorf("duplicate tool in passport projection")
	}
	return out, nil
}

// Probe starts one local native turn and inspects the tools Hermes actually
// sends. This turn has no run token, provider access or executable hub tools.
func (s *Server) Probe(ctx context.Context, expected thv1.ProjectionResp) (err error) {
	allowed, err := approvedTools(expected)
	if err != nil {
		return err
	}
	key := "startup-" + rand.Text()
	native, err := s.deps.Gateway.Submit(ctx, key, key, "Inspect available tools")
	if native.ID != "" {
		defer func() {
			// A matched inventory has already received its terminal HTTP refusal.
			if err != nil {
				err = errors.Join(err, s.deps.Gateway.Cancel(ctx, native.ID))
			}
			err = errors.Join(err, s.deps.Gateway.CloseProbe(ctx, native))
			if err == nil {
				names := append([]string{}, slices.Sorted(maps.Keys(allowed))...)
				s.deps.Log.Info(ctx, "verified Hermes tools", "level", controlLevel, "tools", names)
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("submit inventory request: %w", err)
	}
	a := &Admission{native: native, allowed: allowed, probe: make(chan error, 1)}
	s.mu.Lock()
	s.probeAdmission = a
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.probeAdmission = nil
		s.mu.Unlock()
	}()
	select {
	case err := <-a.probe:
		if err != nil {
			return fmt.Errorf("hermes tool inventory: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("hermes tool inventory: %w", context.Cause(ctx))
	}
}

func (a *Admission) probeResult(err error) {
	select {
	case a.probe <- err:
	default:
	}
}
