package lifecycle

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestGroupJoinsLoops(t *testing.T) {
	t.Parallel()
	before := testkit.Goroutines()
	g := NewGroup(context.Background())
	started := make(chan struct{})
	g.Go(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	})
	<-started
	g.Stop()
	testkit.CheckLeak(t, before)
}

func TestServeDrains(t *testing.T) {
	t.Parallel()
	srv := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, time.Second) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not drain")
	}
}
