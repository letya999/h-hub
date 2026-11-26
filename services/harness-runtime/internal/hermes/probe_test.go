package hermes

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRetireProbeAfterNativeSettlement(t *testing.T) {
	t.Parallel()
	var paths []string
	g := &Gateway{stream: &http.Client{Transport: probeTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		body := `{"run_id":"probe","status":"failed"}`
		switch len(paths) {
		case 1:
			body = `{"run_id":"probe","status":"running"}`
		case 2, 3:
		default:
			return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	if err := g.CloseProbe(t.Context(), Run{ID: "probe", Session: "probe"}); err != nil {
		t.Fatal(err)
	}
	want := "GET /v1/runs/probe,GET /v1/runs/probe,DELETE /api/sessions/probe"
	if strings.Join(paths, ",") != want {
		t.Fatalf("probe cleanup requests: %v", paths)
	}
}
