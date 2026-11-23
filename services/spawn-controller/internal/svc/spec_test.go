package svc

import (
	"bytes"
	"testing"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestSpecDigestBindsImage(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.spawn-runtime.digest-pins-image")
	p := defaultPassports("sha256:first", "http://model", "http://task",
		Limits{Mem: "256m", CPU: "1.0", Pids: 256})
	_, first, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Stack.Image = "sha256:second"
	_, second, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("image change did not change effective-spec digest")
	}
}

func TestSpecSchemaVersion(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.schema-version")
	p := defaultPassports("sha256:first", "http://model", "http://task", Limits{Mem: "256m", CPU: "1.0", Pids: 256})
	spec, _, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(spec, []byte(`"schema_version":1`)) {
		t.Fatalf("new spec has no version: %s", spec)
	}
	for _, doc := range []string{`{"stack":{},"config":{},"models":{}}`, string(spec)} {
		if _, err := specPassports(spaceRow{spec: []byte(doc)}); err != nil {
			t.Fatalf("cannot read old/new spec: %v", err)
		}
	}
	_, err = specPassports(spaceRow{spec: []byte(`{"schema_version":2}`)})
	testkit.Deny(t, "sys.schema-version", err != nil)
}
