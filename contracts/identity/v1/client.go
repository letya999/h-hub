package v1

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/letya999/h-hub/shared/httpapi"
)

// Transport is the call plumbing below the typed client: HTTP in
// production; tests route through httptest against the real Ops
// handlers so wire semantics stay identical.
type Transport interface {
	Call(ctx context.Context, op string, req, resp any) error
}

// HTTP calls a peer over the shared client.
type HTTP struct{ C *httpapi.Client }

// Call posts one operation request.
func (t HTTP) Call(ctx context.Context, op string, req, resp any) error {
	err := t.C.Do(ctx, http.MethodPost, "/v1/"+op, req, resp)
	if err != nil {
		return fmt.Errorf("call %s: %w", op, err)
	}
	return nil
}

// Client is the typed caller surface of the contract.
type Client struct{ T Transport }

// IssueServiceToken requests a service token by signing
// CanonicalProof with the caller's private key.
func (c *Client) IssueServiceToken(ctx context.Context, sub, aud, nonce string,
	key ed25519.PrivateKey) (IssueServiceTokenResp, error) {
	sig := ed25519.Sign(key, CanonicalProof(sub, aud, nonce))
	var out IssueServiceTokenResp
	err := c.call(ctx, "IssueServiceToken", IssueServiceTokenReq{
		Sub: sub, Aud: aud, Nonce: nonce,
		Proof: base64.RawURLEncoding.EncodeToString(sig),
	}, &out)
	return out, err
}

// MintRunToken requests a run token; the HTTP transport must already
// carry the caller's service token.
func (c *Client) MintRunToken(ctx context.Context, r MintRunTokenReq) (MintRunTokenResp, error) {
	var out MintRunTokenResp
	err := c.call(ctx, "MintRunToken", r, &out)
	return out, err
}

// ActAs requests an audited owner-scoped token. The transport carries
// an operator-signed bearer token bound to the same owner and audience.
func (c *Client) ActAs(ctx context.Context, r ActAsReq) (ActAsResp, error) {
	var out ActAsResp
	err := c.call(ctx, "ActAs", r, &out)
	return out, err
}

func (c *Client) CreatePrincipal(ctx context.Context, r CreatePrincipalReq) (CreatePrincipalResp, error) {
	var out CreatePrincipalResp
	err := c.call(ctx, "CreatePrincipal", r, &out)
	return out, err
}

func (c *Client) DeactivateHuman(ctx context.Context, r DeactivateHumanReq) (DeactivateHumanResp, error) {
	var out DeactivateHumanResp
	err := c.call(ctx, "DeactivateHuman", r, &out)
	return out, err
}

func (c *Client) LinkBegin(ctx context.Context, r LinkBeginReq) (LinkBeginResp, error) {
	var out LinkBeginResp
	err := c.call(ctx, "LinkBegin", r, &out)
	return out, err
}

func (c *Client) LinkConfirm(ctx context.Context, r LinkConfirmReq) (LinkConfirmResp, error) {
	var out LinkConfirmResp
	err := c.call(ctx, "LinkConfirm", r, &out)
	return out, err
}

func (c *Client) Resolve(ctx context.Context, r ResolveReq) (ResolveResp, error) {
	var out ResolveResp
	err := c.call(ctx, "Resolve", r, &out)
	return out, err
}

func (c *Client) MintLinkToken(ctx context.Context, r MintLinkTokenReq) (MintLinkTokenResp, error) {
	var out MintLinkTokenResp
	err := c.call(ctx, "MintLinkToken", r, &out)
	return out, err
}

// call routes one op through the transport and wraps transport
// failures once, keeping the shared error form in the chain.
func (c *Client) call(ctx context.Context, op string, req, resp any) error {
	if err := c.T.Call(ctx, op, req, resp); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
