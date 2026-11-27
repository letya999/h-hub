package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Fake is an in-memory spawn-controller for tests: spaces, persisted
// digests and a pretend warm runtime, with no Docker. The conformance
// suite pins the shared behavior.
type Fake struct {
	owner  string
	mu     sync.Mutex
	spaces map[string]*SpaceInfo
	next   int
}

// NewFake builds an empty in-memory controller.
func NewFake(owner string) *Fake {
	return &Fake{owner: owner, spaces: map[string]*SpaceInfo{}}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		CreateSpace:    f.create,
		GetSpace:       f.get,
		DestroySpace:   f.destroy,
		EnsureRuntime:  f.ensure,
		ResolveSpec:    f.resolve,
		SkillInventory: f.skillInventory,
		ReleaseRuntime: f.release,
		SetModel:       f.setModel,
	}
}

func (f *Fake) skillInventory(_ context.Context, r SkillInventoryReq) (SkillInventoryResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	space, ok := f.spaces[r.Space]
	if !ok || space.Owner != r.Owner {
		return SkillInventoryResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if r.Digest != space.Digest {
		return SkillInventoryResp{}, herrors.ErrStale
	}
	return SkillInventoryResp{Entries: map[string]string{}}, nil
}

func digestOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
}

func (f *Fake) create(_ context.Context, r CreateSpaceReq) (CreateSpaceResp, error) {
	var out CreateSpaceResp
	if err := herrors.Require(r.Name != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := spaceID(f.next)
	f.spaces[id] = &SpaceInfo{ID: id, Owner: f.owner, Name: r.Name,
		Digest: digestOf(id, r.Template)}
	return CreateSpaceResp{ID: id, Digest: f.spaces[id].Digest}, nil
}

func (f *Fake) get(_ context.Context, r GetSpaceReq) (GetSpaceResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[r.ID]
	if !ok || s.Owner != f.owner {
		return GetSpaceResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.ID) //nolint:wrapcheck // shared error form
	}
	return GetSpaceResp{Space: *s}, nil
}

func (f *Fake) destroy(_ context.Context, r DestroySpaceReq) (DestroySpaceResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[r.ID]
	if !ok || s.Owner != f.owner {
		return DestroySpaceResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.ID) //nolint:wrapcheck // shared error form
	}
	delete(f.spaces, r.ID)
	return DestroySpaceResp{OK: true}, nil
}

func (f *Fake) ensure(_ context.Context, r EnsureRuntimeReq) (EnsureRuntimeResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[r.Space]
	if !ok {
		return EnsureRuntimeResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.Space) //nolint:wrapcheck // shared error form
	}
	s.Runtime = "rt_fake"
	s.Endpoint = "http://fake-runtime:9000"
	return EnsureRuntimeResp{Runtime: s.Runtime, Endpoint: s.Endpoint,
		Digest: s.Digest, Ready: true}, nil
}

func (f *Fake) resolve(_ context.Context, r ResolveSpecReq) (ResolveSpecResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[r.Space]
	if !ok {
		return ResolveSpecResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.Space) //nolint:wrapcheck // shared error form
	}
	return ResolveSpecResp{Spec: "spec:" + s.Name, Digest: s.Digest, Owner: s.Owner}, nil
}

func (f *Fake) release(_ context.Context, _ ReleaseRuntimeReq) (ReleaseRuntimeResp, error) {
	return ReleaseRuntimeResp{OK: true}, nil
}

// modelRoutes is the fixed route set the fake's SetModel validates
// against — the same names the default passport template declares.
var modelRoutes = map[string]bool{"scripted": true, "scripted-alt": true, "devin": true}

func (f *Fake) setModel(_ context.Context, r SetModelReq) (SetModelResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.spaces[r.Space]
	if !ok || s.Owner != f.owner {
		return SetModelResp{}, herrors.New(herrors.CodeNotFound, "err.not-found").With("id", r.Space) //nolint:wrapcheck // shared error form
	}
	if !modelRoutes[r.Default] {
		return SetModelResp{}, herrors.New(herrors.CodeInvalid, "err.invalid-arg")
	}
	s.Digest = digestOf(s.Digest, r.Default)
	return SetModelResp{Digest: s.Digest}, nil
}

func spaceID(n int) string {
	digits := []byte{'0', '0', '0', '0', '0', '0'}
	for i := len(digits) - 1; i >= 0 && n > 0; i-- {
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return "sp_" + string(digits)
}
