package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestCorrelationAndRedaction(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")
	var buf bytes.Buffer
	l := New("svc-a", &buf)
	ctx := WithTenant(WithJobID(WithRunID(WithRequestID(context.Background(), "r1"), "run1"), "j1"), "u1")
	l.Info(ctx, "hello", "password", "p@ss", "ok", 42)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	for k, want := range map[string]string{
		"service": "svc-a", "request_id": "r1", "run_id": "run1",
		"job_id": "j1", "tenant": "u1", "level": "info", "msg": "hello",
	} {
		if rec[k] != want {
			t.Errorf("field %s=%v want %s", k, rec[k], want)
		}
	}
	f, ok := rec["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields=%v", rec["fields"])
	}
	if f["password"] != "[redacted]" {
		t.Errorf("password not redacted: %v", f["password"])
	}
	if f["ok"] != float64(42) {
		t.Errorf("ok=%v", f["ok"])
	}
}

func TestErrorLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	New("svc", &buf).Error(context.Background(), "boom", errors.New("kaboom"))
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["err"] != "kaboom" || rec["level"] != "error" {
		t.Fatalf("rec=%v", rec)
	}
}
