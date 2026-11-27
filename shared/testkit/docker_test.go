package testkit

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type waitTransport func(*http.Request) (*http.Response, error)

func (f waitTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDockerWaitReobservesTheSameContainer(t *testing.T) {
	t.Parallel()
	calls := 0
	d := &Docker{hc: &http.Client{Transport: waitTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/containers/fixed/wait" {
			t.Fatalf("unexpected observation: %s %s", req.Method, req.URL.Path)
		}
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"StatusCode":1}`))}, nil
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := d.Wait(ctx, "fixed")
	if err != nil || code != 1 || calls != 2 {
		t.Fatalf("wait returned code=%d, calls=%d, err=%v", code, calls, err)
	}
}
