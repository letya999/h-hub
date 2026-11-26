package svc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
)

func TestWaitForRuntimeAfterUnavailablePoll(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(httpapi.Handle("service", func(w http.ResponseWriter, r *http.Request) error {
		var req sv1.EnsureRuntimeReq
		if err := httpapi.Decode(r, &req); err != nil {
			return fmt.Errorf("read readiness poll: %w", err)
		}
		if req.Digest != "pinned" {
			return herrors.New(herrors.CodeStale, "err.stale")
		}
		if calls.Add(1) == 1 {
			return herrors.New(herrors.CodeUnavailable, "err.unavailable")
		}
		httpapi.WriteJSON(w, sv1.EnsureRuntimeResp{Runtime: "native", Ready: true})
		return nil
	}))
	t.Cleanup(srv.Close)
	client := &sv1.Client{T: sv1.HTTP{C: httpapi.NewClient(srv.URL, "service")}}
	d := dispatcher{s: &Server{deps: Deps{Now: time.Now}}}
	got, err := d.awaitRuntime(t.Context(), client, "space", "pinned")
	if err != nil || got.Runtime != "native" || calls.Load() != 2 {
		t.Fatalf("idempotent readiness poll: %+v, %v, requests=%d", got, err, calls.Load())
	}
	_, err = d.awaitRuntime(t.Context(), client, "space", "changed")
	if herrors.CodeOf(err) != herrors.CodeStale {
		t.Fatalf("changed passport: %v", err)
	}
}
