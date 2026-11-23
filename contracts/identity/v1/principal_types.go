package v1

// CreatePrincipalReq registers a human owner and its provisioned space.
type CreatePrincipalReq struct {
	ID    string `json:"id"`
	Space string `json:"space"`
}

type CreatePrincipalResp struct {
	ID string `json:"id"`
}

// DeactivateHumanReq permanently disables a principal and its links.
type DeactivateHumanReq struct {
	ID string `json:"id"`
}

type DeactivateHumanResp struct {
	OK bool `json:"ok"`
}

// LinkBeginReq creates or returns the owner's pending one-time code.
type LinkBeginReq struct {
	Owner string `json:"owner"`
}

type LinkBeginResp struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
}

// LinkConfirmReq proves control of an external identity in its channel.
type LinkConfirmReq struct {
	Channel  string `json:"channel"`
	External string `json:"external"`
	Code     string `json:"code"`
}

type LinkConfirmResp struct {
	Owner string `json:"owner"`
	Space string `json:"space"`
}

// ResolveReq maps a verified channel identity to an active principal.
type ResolveReq struct {
	Channel  string `json:"channel"`
	External string `json:"external"`
}

type ResolveResp struct {
	Owner string `json:"owner"`
	Space string `json:"space"`
}

// MintLinkTokenReq asks identity to mint a tenant-scoped owner token for a bound channel identity.
type MintLinkTokenReq struct {
	Channel  string `json:"channel"`
	External string `json:"external"`
	Aud      string `json:"aud"`
}

type MintLinkTokenResp struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}
