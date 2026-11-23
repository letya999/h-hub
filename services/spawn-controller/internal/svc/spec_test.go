package svc

import (
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
