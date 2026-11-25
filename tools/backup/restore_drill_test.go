package backup

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/letya999/h-hub/internal/descriptor"
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

// A drilled service boots a current image whose config refuses
// startup when a required env is missing; because the drill runs
// containers with -d --rm the dead container erases itself and the
// readiness loop fails with "No such container", far from the cause.
// Every env key the descriptor declares for a drilled service must
// reach its docker run block. identity-hub is exempt on purpose: it
// boots the N-1 image with the previous release's requirements.
func TestRestoreDrillCarriesDeclaredEnv(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("restore-drill.sh")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := descriptor.Load(filepath.Join("..", "..", "deploy", "descriptor.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string][]string{}
	for _, svc := range desc.Services {
		declared[svc.Name] = svc.Env
	}
	lines := strings.Split(string(data), "\n")
	for _, svc := range []string{
		"task-engine", "channel-hub", "model-gateway",
		"spawn-controller", "tool-hub", "credential-broker",
	} {
		block := drillRunBlock(lines, svc)
		if block == "" {
			t.Fatalf("restore-drill.sh boots no %s container", svc)
		}
		for _, env := range declared[svc] {
			if !strings.Contains(block, env+"=") {
				t.Errorf("%s: drill missing declared env %s", svc, env)
			}
		}
	}
}

// drillRunBlock extracts the docker run or create command that carries
// the service's network alias: from the alias line to the end of the
// command, marked by the >/dev/null redirect every drill command has.
func drillRunBlock(lines []string, service string) string {
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "--network-alias "+service) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		b.WriteString(lines[i])
		if strings.Contains(lines[i], ">/dev/null") {
			break
		}
	}
	return b.String()
}
