package v1

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/httpapi"
)

// stubSign produces token-shaped strings for the contract suite; the
// real identity service wires shared/auth.Sign with its issuer key.
func stubSign(purpose string, payload []byte) (string, error) {
	return "hhub_" + purpose + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".stub", nil
}

// TestConformance runs the contract suite against the Fake over real
// HTTP so wire and in-process behavior are checked by one suite.
func TestConformance(t *testing.T) {
	t.Parallel()
	subPub, subKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fake := NewFake(stubSign, "kid-test",
		map[string]ed25519.PublicKey{"task-engine": subPub},
		func() int64 { return time.Now().Unix() })
	Conformance(t, func(t *testing.T) (*Client, ed25519.PrivateKey) {
		t.Helper()
		mux := http.NewServeMux()
		for op, h := range Ops(fake.Handlers()) {
			mux.Handle("/v1/"+op, httpapi.Handle("tk", h))
		}
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return &Client{T: HTTP{C: httpapi.NewClient(srv.URL, "tk")}}, subKey
	})
}
