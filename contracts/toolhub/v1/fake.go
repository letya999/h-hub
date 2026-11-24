package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// fakeTool is one entry of the fake's fixed tool table.
type fakeTool struct {
	section string
	cap     string
	info    ToolInfo
}

// Fake is an in-memory tool-hub for tests: a fixed tool table where
// each tool names its capability, and a grant set the caller picks.
// The conformance suite pins the shared behavior.
type Fake struct {
	mu     sync.Mutex
	digest string
	grants map[string]bool
	seq    int
	table  map[string]fakeTool
}

// NewFake builds a fake answering for digest with the given grants.
func NewFake(digest string, grants ...string) *Fake {
	allowed := map[string]bool{}
	for _, g := range grants {
		allowed[g] = true
	}
	return &Fake{digest: digest, grants: allowed, table: map[string]fakeTool{
		"probe/probe_echo": {section: "core", cap: "tools.use", info: ToolInfo{
			Toolset: "probe", Name: "probe_echo",
			Description: "Echo the given string.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)}},
		"extra/probe_note": {section: "extended", cap: "memory.search", info: ToolInfo{
			Toolset: "extra", Name: "probe_note",
			Description: "Capability probe.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)}},
	}}
}

// Handlers exposes the fake as the contract operation table.
func (f *Fake) Handlers() Handlers {
	return Handlers{
		AdmitCall:  f.admit,
		Projection: f.projection,
		Invoke:     f.invoke,
	}
}

func (f *Fake) admit(_ context.Context, r AdmitCallReq) (AdmitCallResp, error) {
	var out AdmitCallResp
	if err := herrors.Require(r.Run != "" && r.Digest != "" && r.Toolset != "" && r.Tool != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	decision, receipt := f.decide(r.Digest, r.Toolset+"/"+r.Tool)
	out.Allowed, out.Receipt = decision, receipt
	if !decision {
		out.Reason = "err.tool-denied"
	}
	return out, nil
}

func (f *Fake) projection(_ context.Context, r ProjectionReq) (ProjectionResp, error) {
	var out ProjectionResp
	if err := herrors.Require(r.Run != "" && r.Digest != "", "err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Digest != f.digest {
		return out, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	for _, t := range f.table {
		if !f.grants[t.cap] {
			continue
		}
		if t.section == "core" {
			out.Core = append(out.Core, t.info)
		} else {
			out.Extended = append(out.Extended, t.info)
		}
	}
	if len(out.Core)+len(out.Extended) == 0 {
		return out, herrors.New(herrors.CodeForbidden, "err.tool-projection-empty")
	}
	out.Digest = f.digest
	return out, nil
}

func (f *Fake) invoke(_ context.Context, r InvokeReq) (InvokeResp, error) {
	var out InvokeResp
	if err := herrors.Require(r.Run != "" && r.Digest != "" && r.Toolset != "" && r.Tool != "",
		"err.invalid-arg"); err != nil {
		return out, err //nolint:wrapcheck // shared error form
	}
	allowed, receipt := f.decide(r.Digest, r.Toolset+"/"+r.Tool)
	if !allowed {
		return out, herrors.New(herrors.CodeForbidden, "err.tool-denied")
	}
	out.Receipt = receipt
	args := r.Arguments
	if len(args) == 0 {
		args = json.RawMessage("null")
	}
	out.Result = json.RawMessage(fmt.Sprintf(`{"echo":%s}`, args))
	return out, nil
}

// decide is the fake's admission: digest must match and the tool's
// capability must be granted; every decision issues a receipt.
func (f *Fake) decide(digest, key string) (bool, Receipt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, known := f.table[key]
	f.seq++
	receipt := Receipt{ID: fmt.Sprintf("rcpt_%d", f.seq), At: time.Now().UTC()}
	if !known || digest != f.digest {
		return false, receipt
	}
	receipt.Caps = []string{t.cap}
	return f.grants[t.cap], receipt
}
