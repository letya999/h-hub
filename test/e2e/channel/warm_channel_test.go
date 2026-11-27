//go:build hhube2e

package channel

import (
	"context"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

type warmChannel struct {
	owner  string
	cursor int64
}

func (f *warmChannel) inject(t *testing.T, key, text string, replyTo int64) {
	t.Helper()
	_, err := MockClient(t, f.owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: f.owner, UpdateID: Idem(f.owner, key), Text: text, ReplyTo: replyTo,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWarmHermesChannel(t *testing.T) {
	t.Parallel()
	f := warmChannel{owner: "warm_channel_" + Nonce}
	space, code := AddUser(t, f.owner)
	f.inject(t, "link", "/start "+code, 0)
	f.cursor = WaitMock(t, f.owner, 0, "Account linked", 10*time.Second)
	cases := []struct {
		name string
		run  func(*testing.T, *warmChannel)
	}{
		{"TestOwnerQuestionRoundTrip", ownerQuestionRoundTrip},
		{"TestOwnerChoiceRoundTrip", ownerChoiceRoundTrip},
		{"TestScriptedLongAnswer", scriptedLongAnswer},
		{"TestHermesCompleteLongDelivery", hermesCompleteLongDelivery},
	}
	var runtime string
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, &f)
			id := strings.TrimSpace(string(DockerOutput(t, "ps", "-q", "--filter", "label=hhub.space="+space)))
			if id == "" || (runtime != "" && id != runtime) {
				t.Fatalf("expected the same warm Hermes runtime, previous=%q current=%q", runtime, id)
			}
			runtime = id
		})
	}
}
