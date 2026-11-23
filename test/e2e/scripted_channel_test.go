//go:build hhube2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestScriptedLongAnswer(t *testing.T) {
	t.Parallel()
	id := "long_" + nonce
	owner := "scripted_" + nonce
	_, code := addUser(t, owner)
	if _, err := mockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{External: owner, UpdateID: idem("link", owner), Text: "/start " + code}); err != nil {
		t.Fatal(err)
	}
	cursor := waitMock(t, owner, 0, "Account linked", 10*time.Second)
	response := "<script>alert(1)</script>" + strings.Repeat("Ж", 21000)
	script := fmt.Sprintf("id: %s\nsteps:\n  - expect: %s\n    respond: %s\n", id, strconv.Quote("scenario:"+id+" long"), strconv.Quote(response))
	postScenario(t, script, http.StatusCreated)
	if _, err := mockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{External: owner, UpdateID: idem("long", owner), Text: "scenario:" + id + " long"}); err != nil {
		t.Fatal(err)
	}
	var messages []cv1.OutMessage
	testkit.Poll(t, 3*time.Minute, 250*time.Millisecond, func() bool {
		res, err := mockClient(t, owner, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owner, After: cursor})
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
	res, err := http.Get("http://scripted-model:9000/v1/scenarios/" + id + "/verify")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("unused script steps: %s", res.Status)
	}
}

func postScenario(t *testing.T, body string, want int) {
	t.Helper()
	res, err := http.Post("http://scripted-model:9000/v1/scenarios", "application/yaml", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != want {
		t.Fatalf("load scenario: %s", res.Status)
	}
}
