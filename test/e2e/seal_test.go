//go:build hhube2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
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
}

func dockerOutput(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", args[0], err, out)
	}
	return out
}
