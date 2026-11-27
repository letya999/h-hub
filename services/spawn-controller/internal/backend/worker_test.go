package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestStopWorkerRequiresInspectAbsence(t *testing.T) {
	t.Parallel()
	ref := WorkerRef{Key: "key", Generation: "generation"}
	d := &Docker{network: "installation"}
	engine := workerEngine{present: true, inspectPath: "/containers/" + workerName(d.network, ref.Key) + "/json"}
	body, err := json.Marshal(map[string]any{
		"Id": "owned-container", "Config": map[string]any{"Labels": workerLabels(d.network, ref)},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.body = string(body)
	d.hc = &http.Client{Transport: &engine}
	absent, err := d.StopWorker(t.Context(), ref)
	if absent || err != nil || engine.deletes != 1 || engine.inspects != 2 {
		t.Fatalf("DELETE alone: absent=%t err=%v engine=%+v", absent, err, engine)
	}
	absent, err = d.StopWorker(t.Context(), WorkerRef{Key: ref.Key, Generation: "foreign"})
	if absent || err == nil || engine.deletes != 1 {
		t.Fatalf("wrong generation: absent=%t err=%v deletes=%d", absent, err, engine.deletes)
	}
	engine.present = false
	absent, err = d.StopWorker(t.Context(), ref)
	if !absent || err != nil || engine.deletes != 1 {
		t.Fatalf("already absent: absent=%t err=%v deletes=%d", absent, err, engine.deletes)
	}
}

type workerEngine struct {
	inspectPath, body string
	present           bool
	deletes, inspects int
}

func (e *workerEngine) RoundTrip(r *http.Request) (*http.Response, error) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == e.inspectPath:
		e.inspects++
		if e.present {
			return dockerResponse(http.StatusOK, e.body), nil
		}
		return dockerResponse(http.StatusNotFound, "{}"), nil
	case r.Method == http.MethodDelete && r.URL.Path == "/containers/owned-container":
		e.deletes++
		return dockerResponse(http.StatusNoContent, ""), nil
	default:
		return nil, fmt.Errorf("unexpected worker request: %s %s", r.Method, r.URL)
	}
}
