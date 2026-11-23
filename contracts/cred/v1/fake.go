package v1

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Fake is an in-memory credential-broker for tests: the same handlers
// and lease semantics, no SQLCipher underneath. The conformance suite
// pins the shared behavior.
type Fake struct {
	mu      sync.Mutex
	secrets map[string]map[string]string
	leases  map[string]int64
	now     func() time.Time
}

func NewFake() *Fake {
	return &Fake{
		secrets: map[string]map[string]string{},
		leases:  map[string]int64{},
		now:     time.Now,
	}
}

func (f *Fake) Handlers() Handlers {
	return Handlers{
		Acquire:         f.acquire,
		Revoke:          f.revoke,
		ListConnections: f.list,
		SetSecret:       f.set,
		Snapshot:        f.snapshot,
	}
}

func (f *Fake) acquire(_ context.Context, r AcquireReq) (AcquireResp, error) {
	var out AcquireResp
	if err := herrors.Require(r.Ref != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fields, ok := f.secrets[r.Ref]
	if !ok {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	lease := "lease-" + hexStr(8)
	f.leases[lease] = f.now().Add(90 * time.Second).Unix()
	cp := make(map[string]string, len(fields))
	for k, v := range fields {
		cp[k] = v
	}
	return AcquireResp{Lease: lease, ExpiresAt: f.leases[lease], Fields: cp}, nil
}

func (f *Fake) revoke(_ context.Context, r RevokeReq) (RevokeResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.leases[r.Lease]; !ok {
		return RevokeResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	delete(f.leases, r.Lease)
	return RevokeResp{OK: true}, nil
}

func (f *Fake) list(_ context.Context, _ ListConnectionsReq) (ListConnectionsResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := ListConnectionsResp{}
	for ref := range f.secrets {
		out.Connections = append(out.Connections, Connection{
			Name: nameOf(ref), Type: "api-key", Ref: ref, Version: 1})
	}
	return out, nil
}

func (f *Fake) set(_ context.Context, r SetSecretReq) (SetSecretResp, error) {
	var out SetSecretResp
	if err := herrors.Require(r.Name != "" && r.Value != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := "cred://builtin/platform/" + r.Name
	f.secrets[ref] = map[string]string{"value": r.Value}
	return SetSecretResp{Ref: ref, Version: 1}, nil
}

func (f *Fake) snapshot(_ context.Context, _ SnapshotReq) (SnapshotResp, error) {
	return SnapshotResp{Path: "/data/broker-snapshot.db"}, nil
}

func nameOf(ref string) string {
	const pre = "cred://builtin/platform/"
	if len(ref) > len(pre) && ref[:len(pre)] == pre {
		return ref[len(pre):]
	}
	return ref
}

func hexStr(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
