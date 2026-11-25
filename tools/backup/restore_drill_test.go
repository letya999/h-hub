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
