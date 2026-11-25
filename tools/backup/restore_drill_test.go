package backup

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A spec compiled by current code renders tool_url, so the restored
// runtime dials tool-hub on every turn; without a drill container the
// projection call fails and the restore probe turns refuse. The drill
// must carry every service a rendered config can name.
func TestRestoreDrillRunsToolHub(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("restore-drill.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !regexp.MustCompile(`services="[^"]*tool-hub`).MatchString(script) {
		t.Fatal("tool-hub missing from the key-volume service list")
	}
	for _, want := range []string{
		"--network-alias tool-hub",
		`"tool-hub:$toolhub"`,
		"model_gateway tool_hub",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("restore-drill.sh missing %q", want)
		}
	}
}

// The backup's most valuable payload is the sealed secrets base, so
// the drill must open it: credential-broker boots on the restored
// snapshot with the backed-up key (a boot means cipher_version matched
// and the base opened), then its connection listing is diffed against
// the listing the backup recorded. Secret values are never read.
func TestRestoreDrillRunsCredentialBroker(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("restore-drill.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !regexp.MustCompile(`services="[^"]*credential-broker`).MatchString(script) {
		t.Fatal("credential-broker missing from the key-volume service list")
	}
	for _, want := range []string{
		"--network-alias credential-broker",
		`"credential-broker:$broker"`,
		"broker-snapshot.db",
		"broker-db-key",
		"broker-connections.txt",
		"secret list",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("restore-drill.sh missing %q", want)
		}
	}
}

// The drill can diff restored connections only if backup.sh records
// the listing next to the snapshot.
func TestBackupRecordsConnectionListing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("backup.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{"secret list", "broker-connections.txt"} {
		if !strings.Contains(script, want) {
			t.Fatalf("backup.sh missing %q", want)
		}
	}
}
