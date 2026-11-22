package clock

import (
	"testing"
	"time"
)

func TestFakeAdvance(t *testing.T) {
	t.Parallel()
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)
	f.Advance(5 * time.Minute)
	if got := f.Now(); !got.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("fake now=%v", got)
	}
	if System().Now().IsZero() {
		t.Fatal("system clock must report time")
	}
}
