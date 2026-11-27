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

func (f *warmChannel) ownerRoundTrip(t *testing.T, mark, tool, args, question, reply, answer, final string) {
	t.Helper()
	sid := "orq" + strings.ReplaceAll(Idem(mark), ".", "")
	ownerScenario(t, sid, tool, args, answer, final)
	f.inject(t, "ask-"+mark, "scenario:"+sid+" go", 0)
	cursor := WaitMock(t, f.owner, f.cursor, question, 3*time.Minute)
	messages, err := MockClient(t, f.owner, "ReadOutbox").ReadOutbox(context.Background(),
		cv1.ReadOutboxReq{External: f.owner, After: f.cursor})
	if err != nil || len(messages.Messages) != 1 || messages.Messages[0].Kind != "request" ||
		messages.Messages[0].Body != "asking the owner\n\n"+question {
		t.Fatalf("expected one question with the turn introduction, got %+v (%v)", messages, err)
	}
	f.inject(t, "answer-"+mark, reply, cursor)
	answerSeq := WaitMock(t, f.owner, cursor, final, 3*time.Minute)
	acceptedSeq := WaitMock(t, f.owner, cursor, "Answer accepted.", 10*time.Second)
	f.cursor = max(answerSeq, acceptedSeq)
	res, err := http.Get("http://scripted-model:9000/v1/scenarios/" + sid + "/verify")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test response close
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("scenario not fully consumed: %s", res.Status)
	}
}

func ownerQuestionRoundTrip(t *testing.T, f *warmChannel) {
	f.ownerRoundTrip(t, "ask", "ask", `{"question":"pick a color"}`,
		"pick a color", "blue", "blue", "color noted")
}

func ownerChoiceRoundTrip(t *testing.T, f *warmChannel) {
	f.ownerRoundTrip(t, "choose", "choose",
		`{"question":"red or blue","options":["red","blue"]}`,
		"red or blue\n1. red\n2. blue", "2", "blue", "blue it is")
}
