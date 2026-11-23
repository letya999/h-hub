package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWaitPostgresRejectsTemporarySocketServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	docker := `#!/bin/sh
printf '%s\n' "$*" >> "$TRACE"
case " $* " in
  *" -h 127.0.0.1 "*) test "$(wc -l < "$TRACE")" -ge 3 ;;
  *) exit 0 ;;
esac
`
	for name, body := range map[string]string{"docker": docker, "sleep": "#!/bin/sh\nexit 0\n"} {
		writeStub(t, filepath.Join(dir, name), body)
	}
	cmd := exec.Command("sh", "-c", ". ./postgres-ready.sh; wait_postgres restore-pg")
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "TRACE=" + log}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wait for final server: %v: %s", err, out)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(calls), "pg_isready"); got != 3 {
		t.Fatalf("ready before final TCP server: %d probes, want 3", got)
	}
}

func writeStub(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
