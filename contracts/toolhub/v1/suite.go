package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

// Case is one conformance scenario.
type Case struct {
	Name string
	Run  func(t *testing.T, ctx context.Context, c *Client)
}

// Conformance runs every contract case as a subtest. The client must
// answer for digest "dg_1" with tools.use and memory.search granted.
func Conformance(t *testing.T, mk func(t *testing.T) *Client) {
	t.Helper()
	ctx := context.Background()
	for _, cs := range cases {
		t.Run(cs.Name, func(t *testing.T) {
			t.Parallel()
			cs.Run(t, ctx, mk(t))
		})
	}
}

var cases = []Case{
	{"admit_allowed", caseAdmitAllowed},
	{"admit_denied", caseAdmitDenied},
	{"projection_sections", caseProjection},
	{"projection_empty_refused", caseProjectionEmpty},
	{"invoke_echo", caseInvoke},
	{"invoke_denied", caseInvokeDenied},
	{"invalid_input", caseInvalidInput},
}

// ungrantedClient answers for "dg_1" with no grants at all — the
// cases proving refusal need a spec where nothing is allowed.
func ungrantedClient(t *testing.T) *Client {
	t.Helper()
	fake := NewFake("dg_1")
	mux := http.NewServeMux()
	for op, h := range Ops(fake.Handlers()) {
		mux.Handle("/v1/"+op, httpapi.Handle("tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{T: HTTP{C: httpapi.NewClient(srv.URL, "tk")}}
}

func caseAdmitAllowed(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.AdmitCall(ctx, AdmitCallReq{
		Run: "run_1", Digest: "dg_1", Toolset: "probe", Tool: "probe_echo"})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !res.Allowed || res.Receipt.ID == "" || res.Receipt.At.IsZero() {
		t.Fatalf("admission: %+v", res)
	}
	if len(res.Receipt.Caps) == 0 || res.Receipt.Caps[0] != "tools.use" {
		t.Fatalf("caps checked: %+v", res.Receipt)
	}
}

func caseAdmitDenied(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.AdmitCall(ctx, AdmitCallReq{
		Run: "run_1", Digest: "dg_1", Toolset: "cap", Tool: "missing_tool"})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if res.Allowed || res.Reason == "" {
		t.Fatalf("denial: %+v", res)
	}
	if res.Receipt.ID == "" {
		t.Fatal("denial without receipt")
	}
}

func caseProjection(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.Projection(ctx, ProjectionReq{Run: "run_1", Digest: "dg_1"})
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if len(res.Core) == 0 || res.Core[0].Name != "probe_echo" {
		t.Fatalf("core: %+v", res.Core)
	}
	if len(res.Extended) == 0 {
		t.Fatal("extended empty despite grant")
	}
	for _, ti := range res.Core {
		if ti.Name == "" || len(ti.Parameters) == 0 {
			t.Fatalf("tool without name or schema: %+v", ti)
		}
	}
}

func caseProjectionEmpty(t *testing.T, ctx context.Context, _ *Client) {
	res, err := ungrantedClient(t).Projection(ctx,
		ProjectionReq{Run: "run_1", Digest: "dg_1"})
	if err == nil {
		t.Fatalf("empty projection answered: %+v", res)
	}
	if herrors.CodeOf(err) == herrors.CodeInvalid {
		t.Fatalf("empty projection is a refusal, not bad input: %v", err)
	}
}

func caseInvoke(t *testing.T, ctx context.Context, c *Client) {
	res, err := c.Invoke(ctx, InvokeReq{
		Run: "run_1", Digest: "dg_1", Toolset: "probe", Tool: "probe_echo",
		Arguments: json.RawMessage(`{"text":"hi"}`)})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var out struct {
		Echo struct {
			Text string `json:"text"`
		} `json:"echo"`
	}
	if err := json.Unmarshal(res.Result, &out); err != nil || out.Echo.Text != "hi" {
		t.Fatalf("echo: %s err %v", res.Result, err)
	}
	if res.Receipt.ID == "" {
		t.Fatal("invoke without receipt")
	}
}

func caseInvokeDenied(t *testing.T, ctx context.Context, _ *Client) {
	_, err := ungrantedClient(t).Invoke(ctx, InvokeReq{
		Run: "run_1", Digest: "dg_1", Toolset: "probe", Tool: "probe_echo",
		Arguments: json.RawMessage(`{}`)})
	if herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("ungranted invoke: got %v, want forbidden", err)
	}
}

func caseInvalidInput(t *testing.T, ctx context.Context, c *Client) {
	_, err := c.AdmitCall(ctx, AdmitCallReq{})
	if herrors.CodeOf(err) != herrors.CodeInvalid {
		t.Fatalf("missing fields: got %v, want invalid", err)
	}
}
