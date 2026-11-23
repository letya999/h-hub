package backend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCreateAndStartRecoversCreatedContainer(t *testing.T) {
	t.Parallel()
	created, started := 0, 0
	d := &Docker{hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{}`
		switch r.URL.Path {
		case "/containers/hhub-run-1/json":
			body = `{"Id":"existing-id","Config":{"Image":"sha256:image","User":"1000:1000","Env":["HHUB_SUB=harness-runtime"],"Labels":{"hhub.managed":"true","hhub.runtime":"hhub-run-1"}},"State":{"Running":false}}`
		case "/containers/existing-id/start":
			started++
			status = 204
		case "/containers/create":
			created++
			status = 409
		default:
			return nil, fmt.Errorf("unexpected Docker request %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	id, err := d.CreateAndStart(context.Background(), Launch{
		Name: "hhub-run-1", Image: "sha256:image", UID: "1000:1000",
		Env: []string{"HHUB_SUB=harness-runtime"},
	})
	if err != nil || id != "existing-id" || created != 0 || started != 1 {
		t.Fatalf("replay: id=%q err=%v created=%d started=%d", id, err, created, started)
	}
	_, err = d.CreateAndStart(context.Background(), Launch{
		Name: "hhub-run-1", Image: "sha256:wrong", UID: "1000:1000",
		Env: []string{"HHUB_SUB=harness-runtime"},
	})
	if err == nil || started != 1 {
		t.Fatalf("mismatched intent started: err=%v started=%d", err, started)
	}
}
