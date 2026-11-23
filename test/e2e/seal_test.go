//go:build hhube2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/letya999/h-hub/cmd/hubctl/operator"
	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/auth"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/seal"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestSecretsAbsentFromDumpLogsInspectAndAnswer(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.seal-at-rest", "sys.secret-leak")
	owner := "seal_" + nonce
	_, code := addUser(t, owner)
	if _, err := mockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: owner, UpdateID: idem("link", owner), Text: "/start " + code,
	}); err != nil {
		t.Fatal(err)
	}
	waitMock(t, owner, 0, "Account linked", 10*time.Second)
	db := adminDB(t, "identity_hub")
	var kid string
	var sealed []byte
	if err := db.QueryRowContext(context.Background(), `SELECT kid,sealed FROM signing_keys WHERE purpose='signing-key'`).Scan(&kid, &sealed); err != nil {
		t.Fatal(err)
	}
	sealer, err := seal.Load("/run/hhub-secrets/seal-kek")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := sealer.Open("signing-key", kid, sealed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sealer.Open("other-purpose", kid, sealed)
	testkit.Deny(t, "sys.secret-leak", err != nil)
	dump := dockerOutput(t, "exec", "postgres", "pg_dump", "-U", "postgres", "identity_hub")
	if bytes.Contains(dump, []byte(code)) || bytes.Contains(dump, []byte(hex.EncodeToString(seed))) {
		t.Fatal("plaintext code or signing key in pg_dump")
	}
	for _, container := range []string{"identity-hub", "channel-hub", "task-engine"} {
		for _, args := range [][]string{{"logs", container}, {"inspect", container}} {
			out := dockerOutput(t, args...)
			if bytes.Contains(out, []byte(code)) || bytes.Contains(out, []byte(hex.EncodeToString(seed))) {
				t.Fatalf("secret in docker %s %s", args[0], container)
			}
		}
	}
	res, err := mockClient(t, owner, "ReadOutbox").ReadOutbox(context.Background(), cv1.ReadOutboxReq{External: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Messages {
		if strings.Contains(m.Body, code) || strings.Contains(m.Body, hex.EncodeToString(seed)) {
			t.Fatal("secret in answer")
		}
	}
	// Broker path: a live secret rides the operator write and comes
	// back through a lease — then the whole surface must stay clean.
	brokerCanary(t, owner, dump)
}

// brokerCanary writes a secret through the operator edge, reads it back
// through a service lease, and refuses every readable surface.
func brokerCanary(t *testing.T, owner string, dump []byte) {
	t.Helper()
	canary := "cnry" + nonce
	cTok, err := operator.Token("", "", "SetSecret", "credential-broker", "/etc/hhub/keys/operator.key")
	if err != nil {
		t.Fatal(err)
	}
	bc := &credv1.Client{T: credv1.HTTP{
		C: httpapi.NewClient(os.Getenv("HHUB_BROKER_URL"), cTok)}}
	set, err := bc.SetSecret(context.Background(), credv1.SetSecretReq{
		Name: "seal-canary", Value: canary})
	if err != nil {
		t.Fatal(err)
	}
	sKey, err := auth.LoadPrivateKey("/etc/hhub/services/channel-hub/service.key")
	if err != nil {
		t.Fatal(err)
	}
	identity := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(identityURL, "")}}
	svc, err := identity.IssueServiceToken(context.Background(),
		"channel-hub", "credential-broker", idem("seal-svc-token", owner), sKey)
	if err != nil {
		t.Fatal(err)
	}
	bc.T = credv1.HTTP{C: httpapi.NewClient(os.Getenv("HHUB_BROKER_URL"), svc.Token)}
	acq, err := bc.Acquire(context.Background(), credv1.AcquireReq{Ref: set.Ref})
	if err != nil || acq.Fields["value"] != canary {
		t.Fatalf("lease round trip: %v %v", acq, err)
	}
	vol := dockerOutput(t, "run", "--rm", "-v", "hhub_broker-data:/d",
		"alpine:3.21", "cat", "/d/broker.db")
	testkit.Deny(t, "sys.seal-at-rest", !bytes.Contains(vol, []byte(canary)))
	for _, args := range [][]string{{"logs", "credential-broker"},
		{"inspect", "credential-broker"}} {
		out := dockerOutput(t, args...)
		testkit.Deny(t, "sys.seal-at-rest", !bytes.Contains(out, []byte(canary)))
	}
	testkit.Deny(t, "sys.seal-at-rest", !bytes.Contains(dump, []byte(canary)))
	if t.Failed() {
		t.Fatal("canary escaped a sealed surface")
	}
}

func dockerOutput(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", args[0], err, out)
	}
	return out
}
