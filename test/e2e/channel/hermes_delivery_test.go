//go:build hhube2e

package channel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func hermesCompleteLongDelivery(t *testing.T, f *warmChannel) {
	ctx := context.Background()
	id := "long_native_" + Nonce
	var response strings.Builder
	response.WriteString("<script>alert(1)</script>")
	for i := range 21000 {
		response.WriteRune(0x4e00 + rune(i))
	}
	input := "scenario:" + id + " long"
	PostScenario(t, fmt.Sprintf("id: %s\nsteps:\n  - expect: %s\n    respond: %s\n", id,
		strconv.Quote(input), strconv.Quote(response.String())), http.StatusCreated)
	f.inject(t, id, input, 0)
	var chunks []cv1.OutMessage
	testkit.Poll(t, 3*time.Minute, 250*time.Millisecond, func() bool {
		out, err := MockClient(t, f.owner, "ReadOutbox").ReadOutbox(ctx, cv1.ReadOutboxReq{External: f.owner, After: f.cursor})
		chunks = out.Messages
		return err == nil && len(chunks) >= 6
	})
	if len(chunks) != 6 || chunks[5].Kind != "file" {
		t.Fatalf("expected five parts and a file, got %d entries", len(chunks))
	}
	var delivered strings.Builder
	for i, part := range chunks[:5] {
		if part.Kind != "text" {
			t.Fatalf("part %d is %s", i, part.Kind)
		}
		delivered.WriteString(strings.TrimSuffix(part.Body, locale.T(locale.EN, "channel.truncated")))
	}
	delivered.WriteString(chunks[5].Body)
	if delivered.String() != html.EscapeString(response.String()) {
		t.Fatalf("complete Unicode delivery differs from the provider response")
	}
}
