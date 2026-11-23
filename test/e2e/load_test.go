//go:build hhube2e && hhubload

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestThreeUserLoad(t *testing.T) {
	const turns = 12
	owners := []string{"load_a_" + nonce, "load_b_" + nonce, "load_c_" + nonce}
	cursors := make(map[string]int64, len(owners))
	for _, owner := range owners {
		_, code := addUser(t, owner)
		if _, err := mockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
			External: owner, UpdateID: idem("load-link", owner), Text: "/start " + code,
		}); err != nil {
			t.Fatal(err)
		}
		cursors[owner] = waitMock(t, owner, 0, "Account linked", 10*time.Second)
	}
	start := time.Now()
	errs := make(chan error, len(owners)*turns)
	var wg sync.WaitGroup
	for _, owner := range owners {
		client := mockClient(t, owner, "Inject")
		for i := range turns {
			wg.Add(1)
			go func(owner string, i int) {
				defer wg.Done()
				_, err := client.Inject(context.Background(), cv1.InjectReq{External: owner,
					UpdateID: idem("load", owner, fmt.Sprint(i)), Text: fmt.Sprintf("load %s %d", owner, i)})
				errs <- err
			}(owner, i)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
	}
	for _, owner := range owners {
		owner := owner
		testkit.Poll(t, 5*time.Minute, 500*time.Millisecond, func() bool {
			res, err := mockClient(t, owner, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{
				External: owner, After: cursors[owner]})
			if err != nil {
				return false
			}
			seen := map[string]bool{}
			for _, m := range res.Messages {
				if strings.HasPrefix(m.Body, "answer: load "+owner+" ") {
					seen[m.Body] = true
				}
			}
			return len(seen) == turns
		})
	}
	t.Logf("load: %d messages, 3 users, %.1f s", len(owners)*turns, time.Since(start).Seconds())
}
