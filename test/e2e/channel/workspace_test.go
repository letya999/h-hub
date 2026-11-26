//go:build hhube2e

package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"gopkg.in/yaml.v3"
)

func TestPhase12Workspace(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "cap.workspace.read")
	testkit.Req(t, "cap.workspace.write")
	testkit.Req(t, "cap.config.edit_own")
	f := newWorkspaceProof(t)
	f.activate("platform", `{"tools":{"core":["files","config","probe"]}}`)
	f.autoFiles()
	f.refuseUnsafePaths()
	f.revokeGrants()
	f.respawnWhenIdle()
	f.editConfig()
}

type workspaceProof struct {
	t      *testing.T
	scope  OwnerSpace
	cursor int64
}

func newWorkspaceProof(t *testing.T) *workspaceProof {
	t.Helper()
	owner := "workspace_" + Nonce
	space, code := AddUser(t, owner)
	f := &workspaceProof{t: t, scope: OwnerSpace{Owner: owner, Space: space}}
	f.chat("link", "/start "+code, "Account linked")
	f.activate("user", `{"workspace":{"files":{"soul.md":"public space persona","agents.md":"public space instructions"}}}`)
	return f
}

func (f *workspaceProof) activate(role, patch string) sv1.ActivateResp {
	f.t.Helper()
	ctx := context.Background()
	c := SpawnClient(f.t, f.scope)
	if _, err := c.SubmitLayers(ctx, sv1.SubmitLayersReq{
		Space: f.scope.Space, Role: role, Docs: json.RawMessage(patch),
	}); err != nil {
		f.t.Fatalf("submit phase 12 passport: %v", err)
	}
	p, err := c.Preview(ctx, sv1.PreviewReq{Space: f.scope.Space})
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := c.Activate(ctx, sv1.ActivateReq{Space: f.scope.Space, Base: p.Base, Candidate: p.Candidate})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *workspaceProof) inject(mark, text string, reply int64) {
	f.t.Helper()
	if _, err := MockClient(f.t, f.scope.Owner, "Inject").Inject(context.Background(), cv1.InjectReq{
		External: f.scope.Owner, UpdateID: Idem(f.scope.Owner, mark), Text: text, ReplyTo: reply,
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *workspaceProof) wait(text string) {
	f.t.Helper()
	f.cursor = WaitMock(f.t, f.scope.Owner, f.cursor, text, 150*time.Second)
}

func (f *workspaceProof) chat(mark, input, answer string) {
	f.t.Helper()
	f.inject(mark, input, 0)
	f.wait(answer)
}

func (f *workspaceProof) container() string {
	f.t.Helper()
	id := strings.TrimSpace(string(DockerOutput(f.t, "ps", "-q", "--filter", "label=hhub.space="+f.scope.Space)))
	if id == "" || strings.Contains(id, "\n") {
		f.t.Fatalf("expected one active workspace runtime, got %q", id)
	}
	return id
}

type workspaceCall struct {
	Name      string `yaml:"name"`
	Arguments string `yaml:"arguments"`
}

type workspaceStep struct {
	Expect     string          `yaml:"expect"`
	ToolResult string          `yaml:"expect_tool_result,omitempty"`
	Respond    string          `yaml:"respond,omitempty"`
	Calls      []workspaceCall `yaml:"tool_calls,omitempty"`
}

type workspaceCheck struct {
	name, args, result string
}

func (f *workspaceProof) load(mark string, steps []workspaceStep) string {
	f.t.Helper()
	id := "ws_" + mark + "_" + Nonce
	input := "scenario:" + id + " go"
	for i := range steps {
		steps[i].Expect = input + steps[i].Expect
	}
	raw, err := yaml.Marshal(struct {
		ID    string          `yaml:"id"`
		Steps []workspaceStep `yaml:"steps"`
	}{id, steps})
	if err != nil {
		f.t.Fatal(err)
	}
	PostScenario(f.t, string(raw), http.StatusCreated)
	return input
}

func (f *workspaceProof) script(mark string, checks []workspaceCheck) {
	f.t.Helper()
	prev := ""
	var steps []workspaceStep
	for _, check := range checks {
		steps = append(steps, workspaceStep{ToolResult: prev, Calls: []workspaceCall{{check.name, check.args}}})
		prev = check.result
	}
	final := mark + " checked"
	steps = append(steps, workspaceStep{ToolResult: prev, Respond: final})
	f.chat(mark, f.load(mark, steps), final)
}

func (f *workspaceProof) autoFiles() {
	f.script("auto", []workspaceCheck{
		{"files_mkdir", `{"path":"files/notes"}`, `"ok":true`},
		{"files_write", `{"path":"files/notes/keep.txt","text":"first marker"}`, `"ok":true`},
		{"files_list", `{"path":"files/notes"}`, "keep.txt"},
		{"files_read", `{"path":"files/notes/keep.txt"}`, "first marker"},
		{"files_edit", `{"path":"files/notes/keep.txt","before":"first","text":"saved"}`, `"ok":true`},
		{"files_search", `{"path":"files/notes","query":"saved"}`, "notes/keep.txt"},
		{"files_write", `{"path":"files/notes/temporary.txt","text":"remove me"}`, `"ok":true`},
		{"files_remove", `{"path":"files/notes/temporary.txt"}`, `"ok":true`},
		{"files_read", `{"path":"files/notes/temporary.txt"}`, locale.T(locale.EN, "err.workspace-missing")},
	})
}

func (f *workspaceProof) assertNoRequest(after int64) {
	f.t.Helper()
	out, err := MockClient(f.t, f.scope.Owner, "ReadOutbox").ReadOutbox(context.Background(),
		cv1.ReadOutboxReq{External: f.scope.Owner, After: after})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, message := range out.Messages {
		if message.Kind == "request" {
			f.t.Fatalf("non-interactive approval prompted: %s", message.Body)
		}
	}
}

func (f *workspaceProof) refuseUnsafePaths() {
	if _, err := ExecDocker(f.container(), "ln", "-s", "/var/tmp", "/workspace/files/escape"); err != nil {
		f.t.Fatal(err)
	}
	f.script("boundary", []workspaceCheck{
		{"files_write", `{"path":"files/../outside","text":"x"}`, locale.T(locale.EN, "err.workspace-path")},
		{"files_write", `{"path":"/tmp/absolute","text":"x"}`, locale.T(locale.EN, "err.workspace-absolute")},
		{"files_write", `{"path":"files/escape/phase12","text":"x"}`, locale.T(locale.EN, "err.workspace-symlink")},
		{"files_write", `{"path":"config/readonly","text":"x"}`, locale.T(locale.EN, "err.workspace-read-only")},
		{"files_write", `{"path":"unknown/out","text":"x"}`, locale.T(locale.EN, "err.workspace-outside")},
	})
}

func (f *workspaceProof) revokeGrants() {
	old := f.container()
	activation := f.activate("user", `{
		"grants":{"allow":["tools.use","workspace.read","config.edit_own"]}}`)
	if activation.Effect != "live" {
		f.t.Fatalf("grant change requested %s", activation.Effect)
	}
	f.denyCall(activation.Digest, "files", "files_write", "cap.workspace.write")
	f.chat("write-off", "list my current tools", "answer: tools [config_edit,files_list,files_read,files_search,probe_echo]")
	if f.container() != old {
		f.t.Fatal("revoking workspace.write restarted Hermes")
	}
	activation = f.activate("user", `{"grants":{"allow":["tools.use","config.edit_own"]}}`)
	f.denyCall(activation.Digest, "files", "files_read", "cap.workspace.read")
	f.chat("read-off", "list my current tools", "answer: tools [config_edit,probe_echo]")
}

func (f *workspaceProof) denyCall(digest, set, tool, proof string) {
	f.t.Helper()
	job, run := Idem(proof, "job"), Idem(proof, "run")
	token := mintOwnerRequestRun(f.t, f.scope, job, run, digest)
	c := thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(ToolURL, token)}}
	_, err := c.Invoke(context.Background(), thv1.InvokeReq{Run: run, Digest: digest,
		Toolset: set, Tool: tool, Arguments: json.RawMessage(`{"path":"files/denied.txt","text":"denied"}`)})
	testkit.Deny(f.t, proof, herrors.CodeOf(err) == herrors.CodeForbidden)
}

func (f *workspaceProof) respawnWhenIdle() {
	first, second := Idem("first_marker"), Idem("second_marker")
	f.chat("first-marker", "remember marker "+first, "answer: remember marker "+first)
	f.chat("second-marker", "remember marker "+second, "answer: remember marker "+second)
	old := f.container()
	f.inject("long-turn", "slow:12000 end before restart", 0)
	db := AdminDB(f.t, "model_gateway")
	testkit.Poll(f.t, 20*time.Second, 100*time.Millisecond, func() bool {
		var busy bool
		err := db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM usage_runs
			WHERE owner=$1 AND reserved>0)`, f.scope.Owner).Scan(&busy)
		return err == nil && busy
	})
	f.activate("platform", `{"tools":{"core":["files","config"]}}`)
	if f.container() != old {
		f.t.Fatal("tool selection replaced Hermes during an admitted turn")
	}
	f.wait("answer: end before restart")
	began := time.Now()
	f.chat("recall-markers", "recall my markers", "answer: markers "+first+"/"+second)
	f.t.Logf("idle restart through restored conversation answer: %s", time.Since(began))
	replacement := f.container()
	if replacement == old {
		f.t.Fatal("tool selection failed to replace the runtime")
	}
	f.logRespawn(replacement)
	raw, err := ExecDocker(replacement, "cat", "/workspace/files/notes/keep.txt")
	if err != nil || string(raw) != "saved marker" {
		f.t.Fatalf("workspace after respawn: %q %v", raw, err)
	}
}

func (f *workspaceProof) logRespawn(container string) {
	f.t.Helper()
	var seconds float64
	db := AdminDB(f.t, "spawn_controller")
	err := db.QueryRowContext(context.Background(), `SELECT EXTRACT(EPOCH FROM
		(MAX(h.at) FILTER (WHERE h.event='ready') - MAX(h.at) FILTER (WHERE h.event='claim')))
		FROM runtimes r JOIN fsm.history h ON h.kind='runtime' AND h.id=r.id
		WHERE r.space=$1 AND r.container LIKE $2 || '%'`, f.scope.Space, container).Scan(&seconds)
	if err != nil || seconds <= 0 {
		f.t.Fatalf("runtime claim to ready: %.3f s, %v", seconds, err)
	}
	f.t.Logf("Hermes respawn from runtime claim to ready: %.3f s", seconds)
}

func (f *workspaceProof) editConfig() {
	f.script("config", []workspaceCheck{
		{"config_edit", `{"key":"harness.native","value":[]}`, locale.T(locale.EN, "err.config-setting")},
		{"config_edit", `{"key":"timeout_ms","value":250000}`, `"effect":"respawn"`},
	})
	explain, err := SpawnClient(f.t, f.scope).Explain(context.Background(), sv1.ExplainReq{Space: f.scope.Space})
	if err != nil || explain.Layers["config.timeout_ms"] != "user" {
		f.t.Fatalf("reviewed edit was not written to the user layer: %+v %v", explain, err)
	}
	act := f.activate("user", `{"grants":{"allow":["tools.use","workspace.read"]}}`)
	f.denyCall(act.Digest, "config", "config_edit", "cap.config.edit_own")
	f.chat("config-off", "list my current tools", "answer: tools [files_list,files_read,files_search]")
}
