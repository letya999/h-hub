package testkit

import "testing"

func TestReqIDFormat(t *testing.T) {
	t.Parallel()
	good := []string{"adr.fsm.cas-version", "sys.kill-uncertain", "cap.telegram"}
	for _, id := range good {
		if !reqRe.MatchString(id) {
			t.Errorf("%q must be valid", id)
		}
	}
	bad := []string{"", "adr", "adr.", "x.y", "adr..", "ADR.fsm.x", "adr.fsm..x"}
	for _, id := range bad {
		if reqRe.MatchString(id) {
			t.Errorf("%q must be invalid", id)
		}
	}
}

func TestCheckLeakStable(t *testing.T) {
	t.Parallel()
	CheckLeak(t, Goroutines())
}

func TestDSNDefault(t *testing.T) {
	t.Parallel()
	if DSN() == "" {
		t.Fatal("DSN must have a default")
	}
}
