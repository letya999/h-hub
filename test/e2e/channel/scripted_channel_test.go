//go:build hhube2e

package channel

import (
	"context"
	"fmt"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func scriptedLongAnswer(t *testing.T, f *warmChannel) {
	id := "long_" + Nonce
	response := "<script>alert(1)</script>" + longCyrillicText()
	script := fmt.Sprintf("id: %s\nsteps:\n  - expect: %s\n    respond: %s\n", id, strconv.Quote("scenario:"+id+" long"), strconv.Quote(response))
	PostScenario(t, script, http.StatusCreated)
	f.inject(t, id, "scenario:"+id+" long", 0)
	var messages []cv1.OutMessage
	testkit.Poll(t, 3*time.Minute, 250*time.Millisecond, func() bool {
		res, err := MockClient(t, f.owner, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: f.owner, After: f.cursor})
		if err != nil {
			return false
		}
		messages = res.Messages
		for _, m := range messages {
			if m.Kind == "file" {
				return true
			}
		}
		return false
	})
	texts, files := 0, 0
	for _, m := range messages {
		if m.Kind == "text" {
			texts++
		}
		if m.Kind == "file" {
			files++
		}
	}
	if texts != 5 || files != 1 || !strings.Contains(messages[4].Body, "truncated") || strings.Contains(messages[0].Body, "<script>") {
		t.Fatalf("long delivery: %d text, %d file, first=%q", texts, files, messages[0].Body[:min(80, len(messages[0].Body))])
	}
	f.cursor = messages[len(messages)-1].Seq
	res, err := http.Get("http://scripted-model:9000/v1/scenarios/" + id + "/verify")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("unused script steps: %s", res.Status)
	}
}

// Unique three-letter groups preserve the 21,000 letters and 42,000 UTF-8 bytes.
func longCyrillicText() string {
	var text strings.Builder
	for n := range 7000 {
		text.WriteRune('А' + rune(n/(32*32)))
		text.WriteRune('А' + rune(n/32%32))
		text.WriteRune('А' + rune(n%32))
	}
	return text.String()
}
