package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/letya999/h-hub/shared/httpapi"
)

// TestConformance runs the contract suite against the Fake over real
// HTTP.
func TestConformance(t *testing.T) {
	t.Parallel()
	Conformance(t, func(t *testing.T) *Client {
		t.Helper()
		fake := NewFake(map[string]string{"mock/alice": "alice"})
		mux := http.NewServeMux()
		for op, h := range Ops(fake.Handlers()) {
			mux.Handle("/v1/"+op, httpapi.Handle("tk", h))
		}
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return &Client{T: HTTP{C: httpapi.NewClient(srv.URL, "tk")}}
	})
}
