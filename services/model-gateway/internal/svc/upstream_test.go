package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestRewriteModelPinsRouteModel(t *testing.T) {
	t.Parallel()
	out, err := rewriteModel([]byte(`{"model":"caller-pick","messages":[]}`), "route-model")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"messages":[],"model":"route-model"}` &&
		string(out) != `{"model":"route-model","messages":[]}` {
		t.Fatalf("rewrite: %s", out)
	}
}

func TestRewriteModelPassesGarbageThrough(t *testing.T) {
	t.Parallel()
	if _, err := rewriteModel([]byte(`not json`), "m"); err == nil {
		t.Fatal("garbage body accepted")
	}
}

func TestParseUsageReadsTotal(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	if got := parseUsage(body); got != 15 {
		t.Fatalf("usage=%d", got)
	}
	if got := parseUsage([]byte(`{"choices":[]}`)); got != 0 {
		t.Fatalf("absent usage=%d", got)
	}
	if got := parseUsage([]byte(`{"usage":{"prompt_tokens":3,"completion_tokens":4}}`)); got != 7 {
		t.Fatalf("split usage=%d", got)
	}
}

// idSrv fakes identity's token issuance for the broker hop.
func idSrv(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	ops := idv1.Ops(idv1.Handlers{IssueServiceToken: func(
		_ context.Context, _ idv1.IssueServiceTokenReq) (idv1.IssueServiceTokenResp, error) {
		return idv1.IssueServiceTokenResp{Token: "svc-tk", ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
	}})
	for op, h := range ops {
		if op == "IssueServiceToken" {
			mux.Handle("/v1/"+op, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := h(w, r); err != nil { // proof-signed op; fake ignores auth
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			continue
		}
		mux.Handle("/v1/"+op, httpapi.Handle("tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// credSrv mounts the contract Fake so routeKey crosses a real HTTP
// boundary instead of a stubbed function.
func credSrv(t *testing.T) *httptest.Server {
	t.Helper()
	fake := credv1.NewFake()
	mux := http.NewServeMux()
	for op, h := range credv1.Ops(fake.Handlers()) {
		mux.Handle("/v1/"+op, httpapi.Handle("svc-tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	set, err := (&credv1.Client{T: credv1.HTTP{
		C: httpapi.NewClient(srv.URL, "svc-tk")}}).SetSecret(context.Background(),
		credv1.SetSecretReq{Name: "cliproxy-key", Value: "upstream-secret"})
	if err != nil || set.Ref == "" {
		t.Fatalf("seed secret: %v %q", err, set.Ref)
	}
	return srv
}

func TestRouteKeyLeasesFromBroker(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.model-gateway.route-key-lease")
	ctx := context.Background()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{deps: Deps{BrokerURL: credSrv(t).URL},
		toks: &tokenSource{url: idSrv(t).URL, key: key,
			sub: "model-gateway", now: time.Now, cache: map[string]cached{}}}

	got, err := s.routeKey(ctx, routeView{KeyRef: "cred://builtin/platform/cliproxy-key"})
	if err != nil || got != "upstream-secret" {
		t.Fatalf("key=%q err=%v", got, err)
	}
	keyless, err := s.routeKey(ctx, routeView{})
	if err != nil || keyless != "" {
		t.Fatalf("keyless route: %q %v", keyless, err)
	}
	_, terr := s.routeKey(ctx, routeView{KeyRef: "cred://builtin/platform/missing"})
	testkit.Deny(t, "adr.model-gateway.route-key-lease", terr != nil)
	if terr == nil {
		t.Fatal("unknown ref leased")
	}
}

func TestModelsViewRouteUnknown(t *testing.T) {
	t.Parallel()
	m := modelsView{Routes: []routeView{{Name: "a", URL: "http://x"}}}
	if _, err := m.route("missing"); err == nil {
		t.Fatal("unknown route accepted")
	}
	if _, err := m.route("a"); err != nil {
		t.Fatal(err)
	}
	m.Routes[0].URL = ""
	if _, err := m.route("a"); err == nil {
		t.Fatal("route without url accepted")
	}
}
