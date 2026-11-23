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
	d := &Docker{network: "hhub", hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{}`
		switch r.URL.Path {
		case "/containers/hhub-run-1/json":
			body = `{"Id":"existing-id","Config":{"Image":"sha256:image","User":"1000:1000","Env":["HHUB_SUB=harness-runtime"],"Labels":{"hhub.managed":"true","hhub.runtime":"hhub-run-1"}},"State":{"Running":false},"NetworkSettings":{"Networks":{"hhub":{}}}}`
		case "/containers/existing-id/start":
			started++
			status = 204
		case "/containers/create":
			created++
			status = 409
		default:
			return nil, fmt.Errorf("unexpected Docker request %s %s", r.Method, r.URL)
		}
		return dockerResponse(status, body), nil
	})}}
	id, err := d.CreateAndStart(context.Background(), Launch{
		Name: "hhub-run-1", Image: "sha256:image", UID: "1000:1000",
		Env: []string{"HHUB_SUB=harness-runtime"}, Network: "hhub",
	})
	if err != nil || id != "existing-id" || created != 0 || started != 1 {
		t.Fatalf("replay: id=%q err=%v created=%d started=%d", id, err, created, started)
	}
	_, err = d.CreateAndStart(context.Background(), Launch{
		Name: "hhub-run-1", Image: "sha256:wrong", UID: "1000:1000",
		Env: []string{"HHUB_SUB=harness-runtime"}, Network: "hhub",
	})
	if err == nil || started != 1 {
		t.Fatalf("mismatched intent started: err=%v started=%d", err, started)
	}
}

func TestDockerRefusesForeignNetwork(t *testing.T) {
	t.Parallel()
	deletes := 0
	d := foreignNetworkDocker(&deletes)
	if _, _, err := d.Inspect(context.Background(), "live"); err == nil {
		t.Fatal("foreign container inspected as owned")
	}
	if err := d.Remove(context.Background(), "live"); err == nil || deletes != 0 {
		t.Fatalf("foreign container removed: err=%v deletes=%d", err, deletes)
	}
	rows, err := d.ManagedContainers(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "own" {
		t.Fatalf("janitor crossed installation network: %+v %v", rows, err)
	}
}

func foreignNetworkDocker(deletes *int) *Docker {
	return &Docker{network: "restore", hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{}`
		switch r.URL.Path {
		case "/containers/live/json":
			body = `{"Id":"live","State":{"Running":true},"NetworkSettings":{"Networks":{"hhub":{}}}}`
		case "/containers/json":
			body = `[{"Id":"live","Names":["/hhub-rt-live"],"Labels":{"hhub.managed":"true","hhub.runtime":"hhub-rt-live"},"NetworkSettings":{"Networks":{"hhub":{}}}},{"Id":"own","Names":["/hhub-rt-own"],"Labels":{"hhub.managed":"true","hhub.runtime":"hhub-rt-own"},"NetworkSettings":{"Networks":{"restore":{}}}}]`
		case "/containers/live":
			*deletes++
			status = 204
		default:
			return nil, fmt.Errorf("unexpected Docker request %s %s", r.Method, r.URL)
		}
		return dockerResponse(status, body), nil
	})}}
}

func dockerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: fmt.Sprint(status),
		Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
