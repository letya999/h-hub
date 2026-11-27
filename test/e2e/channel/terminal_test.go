//go:build hhube2e

package channel

import (
	"testing"

	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestPhase14Terminal(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "cap.terminal.exec")
	f := newWorkspaceProof(t, "terminal_"+Nonce)
	f.activate("platform", `{"tools":{"core":["files","ownerreq","terminal"]}}`)
	f.activate("user", `{"grants":{"allow":["tools.use","workspace.read","terminal.exec"]},
		"config":{"approvals":{"mode":"auto"}}}`)
	f.script("terminal", []workspaceCheck{
		{"terminal_exec", `{"argv":["python3","-c","from pathlib import Path; Path('terminal-proof.txt').write_text('phase 14 durable result')"],"workdir":"files","timeout_ms":3000}`, `"exit_code":0`},
		{"files_read", `{"path":"files/terminal-proof.txt"}`, "phase 14 durable result"},
		{"terminal_exec", `{"argv":["python3","-c","print('a'*40000)"],"timeout_ms":3000}`, `"truncated":true`},
	})
	act := f.activate("user", `{"grants":{"allow":["tools.use","workspace.read"]}}`)
	f.denyCall(act.Digest, "terminal", "terminal_exec", "cap.terminal.exec")
	f.chat("terminal-off", "list my current tools", "answer: tools [ask,choose,files_list,files_read,files_search]")
}
