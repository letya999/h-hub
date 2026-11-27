package cellinit

import (
	"testing"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
)

func TestStreamMarksTruncationAndConsumesExcess(t *testing.T) {
	t.Parallel()
	var out stream
	data := make([]byte, wv1.MaxOutput/2+1)
	n, err := out.Write(data)
	if err != nil || n != len(data) {
		t.Fatalf("writer rejected surplus bytes: n=%d err=%v", n, err)
	}
	if !out.truncated || len(out.buf) != wv1.MaxOutput/2 {
		t.Fatalf("truncated=%t size=%d", out.truncated, len(out.buf))
	}
}
