//go:build hhube2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
)

func mockClient(t *testing.T, external, op string) *cv1.Client {
	t.Helper()
	tok, err := operator.Token(external, "", op, "channel-hub", "/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok)}}
}

func addUser(t *testing.T, id string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/hubctl", "user", "add", id)
	cmd.Dir = "/src"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hubctl user add: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 3 || fields[0] != id {
		t.Fatalf("hubctl user output: %q", out)
	}
	cleanupSpace(t, id, fields[1])
	return fields[1], fields[2]
}

func waitMock(t *testing.T, external string, after int64, want string, timeout time.Duration) int64 {
	t.Helper()
	var seq int64
	testkit.Poll(t, timeout, 250*time.Millisecond, func() bool {
		res, err := mockClient(t, external, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: external, After: after})
		if err != nil {
			return false
		}
		for _, m := range res.Messages {
			if strings.Contains(m.Body, want) {
				seq = m.Seq
				return true
			}
		}
		return false
	})
	return seq
}

func TestMockChannelThreeUsers(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.tenant-isolation")
	testkit.Req(t, "sys.forged-token")
	testkit.Req(t, "sys.service-edge")
	owners := []string{"mock_alice_" + nonce, "mock_boris_" + nonce, "mock_cy_" + nonce}
	for _, o := range owners {
		space, code := addUser(t, o)
		_, err := mockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: idem("link", o), Text: "/start " + code})
		if err != nil {
			t.Fatalf("link inject %s: %v", o, err)
		}
		cursor := waitMock(t, o, 0, "Account linked", 10*time.Second)
		_, err = mockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: idem("unknown", o), Text: "/unknown"})
		if err != nil {
			t.Fatal(err)
		}
		cursor = waitMock(t, o, cursor, "Commands:", 10*time.Second)
		jobs, err := taskClient(t, ownerSpace{o, space}).ListJobs(context.Background(), tv1.ListJobsReq{})
		if err != nil || len(jobs.Jobs) != 0 {
			t.Fatalf("unknown command created a job: %+v %v", jobs, err)
		}
		_, err = mockClient(t, o, "Inject").Inject(context.Background(), cv1.InjectReq{External: o, UpdateID: idem("msg", o), Text: "hello " + o})
		if err != nil {
			t.Fatalf("message inject %s: %v", o, err)
		}
		waitMock(t, o, cursor, "answer: hello "+o, 3*time.Minute)
	}
	if _, err := mockClient(t, owners[0], "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[1]}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("cross-tenant outbox: %v", err)
	}
	if _, err := mockClient(t, owners[0], "Inject").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[0]}); herrors.CodeOf(err) != herrors.CodeForbidden {
		t.Fatalf("wrong operation edge: %v", err)
	}
	forgedClient := forgedMock(t, owners[0])
	if _, err := forgedClient.ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owners[0]}); herrors.CodeOf(err) != herrors.CodeUnauthorized {
		t.Fatalf("forged outbox token: %v", err)
	}
}

func forgedMock(t *testing.T, owner string) *cv1.Client {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "operator", Owner: owner, Target: "ReadOutbox", Aud: []string{"channel-hub"}, Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(key, "operator", claims)
	if err != nil {
		t.Fatal(err)
	}
	return &cv1.Client{T: cv1.HTTP{C: httpapi.NewClient("http://channel-hub:9000", tok)}}
}
