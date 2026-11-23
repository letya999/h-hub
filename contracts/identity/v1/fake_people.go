package v1

import (
	"context"
	"encoding/json"
	"fmt"

	herrors "github.com/letya999/h-hub/shared/errors"
)

type fakePerson struct {
	space  string
	active bool
}

func (f *Fake) createPrincipal(_ context.Context, r CreatePrincipalReq) (CreatePrincipalResp, error) {
	if r.ID == "" || r.Space == "" {
		return CreatePrincipalResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.people[r.ID]; ok && (p.space != r.Space || !p.active) {
		return CreatePrincipalResp{}, herrors.New(herrors.CodeConflict, "err.conflict")
	}
	f.people[r.ID] = fakePerson{r.Space, true}
	return CreatePrincipalResp{ID: r.ID}, nil
}

func (f *Fake) deactivateHuman(_ context.Context, r DeactivateHumanReq) (DeactivateHumanResp, error) {
	if r.ID == "" {
		return DeactivateHumanResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.people[r.ID]
	if !ok {
		return DeactivateHumanResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	p.active = false
	f.people[r.ID] = p
	return DeactivateHumanResp{OK: true}, nil
}

func (f *Fake) linkBegin(_ context.Context, r LinkBeginReq) (LinkBeginResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.people[r.Owner]; !ok || !p.active {
		return LinkBeginResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	code := f.codes[r.Owner]
	if code == "" {
		code = fmt.Sprintf("fake-%s", r.Owner)
		f.codes[r.Owner] = code
	}
	return LinkBeginResp{Code: code, ExpiresAt: f.now() + 86400}, nil
}

func (f *Fake) linkConfirm(_ context.Context, r LinkConfirmReq) (LinkConfirmResp, error) {
	if r.Channel == "" || r.External == "" || r.Code == "" {
		return LinkConfirmResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for owner, code := range f.codes {
		if code == r.Code {
			if f.links[r.Channel+"/"+r.External] != "" {
				return LinkConfirmResp{}, herrors.New(herrors.CodeConflict, "err.conflict")
			}
			delete(f.codes, owner)
			f.links[r.Channel+"/"+r.External] = owner
			return LinkConfirmResp{Owner: owner, Space: f.people[owner].space}, nil
		}
	}
	return LinkConfirmResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
}

func (f *Fake) resolve(_ context.Context, r ResolveReq) (ResolveResp, error) {
	if r.Channel == "" || r.External == "" {
		return ResolveResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := f.links[r.Channel+"/"+r.External]
	p := f.people[owner]
	if owner == "" || !p.active {
		return ResolveResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	return ResolveResp{Owner: owner, Space: p.space}, nil
}

func (f *Fake) mintLinkToken(ctx context.Context, r MintLinkTokenReq) (MintLinkTokenResp, error) {
	if r.Aud != "task-engine" {
		return MintLinkTokenResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	resolved, err := f.resolve(ctx, ResolveReq{Channel: r.Channel, External: r.External})
	if err != nil {
		return MintLinkTokenResp{}, fmt.Errorf("resolve: %w", err)
	}
	exp := f.now() + ServiceTTL
	claims, err := json.Marshal(OwnerClaims{Sub: "channel-hub", Owner: resolved.Owner, Space: resolved.Space, Aud: []string{r.Aud}, Exp: exp, Kid: f.kid})
	if err != nil {
		return MintLinkTokenResp{}, fmt.Errorf("link claims: %w", err)
	}
	tok, err := f.sign("owner", claims)
	if err != nil {
		return MintLinkTokenResp{}, fmt.Errorf("sign link: %w", err)
	}
	return MintLinkTokenResp{Token: tok, ExpiresAt: exp}, nil
}
