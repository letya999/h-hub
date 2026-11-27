package v1

import "context"

type ClaimWorkerReq struct {
	Owner   string `json:"owner"`
	Space   string `json:"space"`
	Digest  string `json:"digest"`
	Profile string `json:"profile"`
	Key     string `json:"key"`
}

type ClaimWorkerResp struct {
	Worker     string `json:"worker"`
	Generation string `json:"generation"`
	Lease      string `json:"lease"`
	Ready      bool   `json:"ready"`
	SocketSlot int    `json:"socket_slot,omitempty"`
	Token      string `json:"token,omitempty"`
}

type WorkerLeaseReq struct {
	Owner      string `json:"owner"`
	Space      string `json:"space"`
	Worker     string `json:"worker"`
	Generation string `json:"generation"`
	Lease      string `json:"lease"`
	Action     string `json:"action"`
}

const (
	RenewWorkerLease   = "renew"
	ReleaseWorkerLease = "release"
	RetireWorkerLease  = "retire"
)

type WorkerLeaseResp struct {
	OK bool `json:"ok"`
}

type WorkerHandlers struct {
	Claim func(context.Context, ClaimWorkerReq) (ClaimWorkerResp, error)
	Lease func(context.Context, WorkerLeaseReq) (WorkerLeaseResp, error)
}

func (c *Client) ClaimWorker(ctx context.Context, r ClaimWorkerReq) (ClaimWorkerResp, error) {
	var out ClaimWorkerResp
	err := call(c, ctx, "ClaimWorker", r, &out)
	return out, err
}

func (c *Client) WorkerLease(ctx context.Context, r WorkerLeaseReq) (WorkerLeaseResp, error) {
	var out WorkerLeaseResp
	err := call(c, ctx, "WorkerLease", r, &out)
	return out, err
}
