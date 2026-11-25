//go:build hhube2e

package channel

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

// ownerScenario loads one owner-request scenario: the model asks in
// the first turn, ends that turn once the tool comes back pending
// (same input — the turn has not moved), and answers in the
// continuation run whose input carries the composed owner answer.
func ownerScenario(t *testing.T, sid, tool, args, answer, final string) {
	t.Helper()
	input := "scenario:" + sid + " go"
	composed := input + "\n\n[owner request answered: " + answer + "]"
	script := fmt.Sprintf("id: %s\nsteps:\n"+
		"  - expect: %s\n"+
		"    tool_calls:\n"+
		"      - name: %s\n"+
		"        arguments: %s\n"+
		"  - expect: %s\n"+
		"    respond: %s\n"+
		"  - expect: %s\n"+
		"    respond: %s\n",
		sid, strconv.Quote(input), tool, strconv.Quote(args),
		strconv.Quote(input), strconv.Quote("asking the owner"),
		strconv.Quote(composed), strconv.Quote(final))
	PostScenario(t, script, http.StatusCreated)
}

func ownerRoundTrip(t *testing.T, mark, tool, args, question, reply, answer, final string) {
	t.Helper()
	owner := "mock_orq_" + mark + Nonce
	_, code := AddUser(t, owner)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("link", mark), Text: "/start " + code}); err != nil {
		t.Fatalf("link inject: %v", err)
	}
	cursor := WaitMock(t, owner, 0, "Account linked", 10*time.Second)
	sid := "orq" + strings.ReplaceAll(Idem(mark), ".", "")
	ownerScenario(t, sid, tool, args, answer, final)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("ask", mark), Text: "scenario:" + sid + " go"}); err != nil {
		t.Fatalf("message inject: %v", err)
	}
	cursor = WaitMock(t, owner, cursor, question, 3*time.Minute)
	if _, err := MockClient(t, owner, "Inject").Inject(context.Background(),
		cv1.InjectReq{External: owner, UpdateID: Idem("answer", mark), Text: reply}); err != nil {
		t.Fatalf("answer inject: %v", err)
	}
	WaitMock(t, owner, cursor, final, 3*time.Minute)
	res, err := http.Get("http://scripted-model:9000/v1/scenarios/" + sid + "/verify")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("scenario not fully consumed: %s", res.Status)
	}
}

func TestOwnerQuestionRoundTrip(t *testing.T) {
	t.Parallel()
	ownerRoundTrip(t, "ask", "ask", `{"question":"pick a color"}`,
		"pick a color", "blue", "blue", "color noted")
}

func TestOwnerChoiceRoundTrip(t *testing.T) {
	t.Parallel()
	ownerRoundTrip(t, "choose", "choose",
		`{"question":"red or blue","options":["red","blue"]}`,
		"red or blue", "2", "blue", "blue it is")
}
