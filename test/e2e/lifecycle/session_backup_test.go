//go:build hhube2e

package lifecycle

import (
	"os"
	"strings"
	"testing"

	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestHermesSessionArchiveIncludesCommittedWAL(t *testing.T) {
	t.Parallel()
	pin, err := os.ReadFile("/src/deploy/hermes-image.ref")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("/src/tools/backup/session-archive.py")
	if err != nil {
		t.Fatal(err)
	}
	DockerOutput(t, "run", "--rm", "--network", "none", "--entrypoint", "/opt/hermes/.venv/bin/python",
		strings.TrimSpace(string(pin)), "-c", sessionArchiveRoundTrip, string(source))
}

const sessionArchiveRoundTrip = `
import sqlite3, subprocess, sys, tempfile
from pathlib import Path

def call(mode, directory, payload=None):
    code = ('import sys; scope = {"__name__": "archive"}; exec(sys.argv[1], scope); '
            'scope[sys.argv[2]](scope["Path"](sys.argv[3]))')
    return subprocess.run([sys.executable, "-c", code, sys.argv[1], mode, str(directory)],
                          input=payload, stdout=subprocess.PIPE, check=True).stdout

with tempfile.TemporaryDirectory() as base:
    source = Path(base) / "source"
    (source / "sessions").mkdir(parents=True)
    (source / "sessions" / "turn.jsonl").write_text("saved transcript")
    db = sqlite3.connect(source / "state.db")
    db.execute("PRAGMA journal_mode=WAL")
    db.execute("PRAGMA wal_autocheckpoint=0")
    db.execute("CREATE TABLE turns (text)")
    db.execute("INSERT INTO turns VALUES ('committed in WAL')")
    db.commit()
    dest = Path(base) / "restored"
    call("restore", dest, call("capture", source))
    db.close()
    assert (dest / "sessions" / "turn.jsonl").read_text() == "saved transcript"
    assert not (dest / "state.db-wal").exists()
    with sqlite3.connect(dest / "state.db") as copy:
        assert copy.execute("SELECT text FROM turns").fetchone() == ("committed in WAL",)
`
