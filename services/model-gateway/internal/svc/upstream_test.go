package svc

import (
	"os"
	"path/filepath"
	"testing"

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

func TestRouteKeyConfinedToRoutesDir(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.model-gateway.route-key-file")
	dir := t.TempDir()
	s := &Server{deps: Deps{KeysDir: dir}}
	_, terr := s.routeKey(routeView{KeyFile: "../service.key"})
	testkit.Deny(t, "adr.model-gateway.route-key-file", terr != nil)
	if terr == nil {
		t.Fatal("traversal key accepted")
	}
	if _, err := s.routeKey(routeView{KeyFile: "/abs/key"}); err == nil {
		t.Fatal("absolute key accepted")
	}
	if _, err := s.routeKey(routeView{KeyFile: "missing.key"}); err == nil {
		t.Fatal("missing key file accepted")
	}
	routes := filepath.Join(dir, "routes")
	if err := os.MkdirAll(routes, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routes, "p.key"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.routeKey(routeView{KeyFile: "p.key"})
	if err != nil || got != "secret" {
		t.Fatalf("key=%q err=%v", got, err)
	}
	keyless, err := s.routeKey(routeView{})
	if err != nil || keyless != "" {
		t.Fatalf("keyless route: %q %v", keyless, err)
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
