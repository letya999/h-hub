//go:build hhube2e

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	"github.com/letya999/h-hub/shared/locale"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestPhase13Skills(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "cap.persona.edit_own", "sys.skill-showcase", "sys.skill-scan", "sys.skill-catalog")
	f := newWorkspaceProof(t, "skills_"+Nonce)
	f.activate("platform", `{"tools":{"core":["files","ownerreq","skills"]}}`)
	configureSkills(f)
	useSkills(f)
	checkSkillPin(f)
	revokePersona(f)
	refuseMissingShowcase(f)
}

func configureSkills(f *workspaceProof) {
	f.t.Helper()
	skills := map[string]string{}
	for i := range 30 {
		skills[fmt.Sprintf("target-%02d", i)] = "# " + strings.Repeat("long skill description ", 6)
	}
	doc, err := json.Marshal(map[string]any{
		"workspace": map[string]any{"skills": skills},
		"config":    map[string]any{"approvals": map[string]string{"mode": "auto"}},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.activate("user", string(doc))
}

func useSkills(f *workspaceProof) {
	f.t.Helper()
	markers := []string{locale.T(locale.EN, "tool.skills.search-hint"), "layer/workspace-notes", "hub/guide"}
	for i := range 30 {
		markers = append(markers, fmt.Sprintf("layer/target-%02d", i))
	}
	input := f.load("skills-overflow", []workspaceStep{{
		SystemHas: markers, SystemLacks: []string{"long skill description", "skills_list", "own/notes"},
		Calls: []workspaceCall{{"skills_search", `{"query":"target-01"}`}},
	}, {
		ToolResult: "layer/target-01", Calls: []workspaceCall{{"skills_read", `{"name":"hub/guide"}`}},
	}, {
		ToolResult: locale.T(locale.EN, "tool.skills.guide-ask"), Respond: "skill catalog checked",
	}})
	f.chat("skills-overflow", input, "skill catalog checked")
	f.script("skill-writes", []workspaceCheck{
		{"files_write_skill", `{"path":"skills/notes.md","text":"# durable owner note"}`, `"ok":true`},
		{"files_edit_skill", `{"path":"skills/notes.md","before":"durable","text":"reviewed"}`, `"ok":true`},
		{"files_edit_skill", `{"path":"skills/notes.md","before":"reviewed","text":"password` + " = " + `example-only-value"}`, `"rule":"secret-like"`},
		{"files_write", `{"path":"skills/notes.md","text":"ordinary write"}`, locale.T(locale.EN, "err.workspace-read-only")},
		{"files_write_skill", `{"path":"skills/disposable.md","text":"# Disposable"}`, `"ok":true`},
		{"files_remove_skill", `{"path":"skills/disposable.md"}`, `"ok":true`},
		{"files_read", `{"path":"skills/disposable.md"}`, locale.T(locale.EN, "err.workspace-missing")},
	})
	cid := f.container()
	raw, err := ExecDocker(cid, "cat", "/workspace/skills/notes.md")
	testkit.Deny(f.t, "sys.skill-scan", err == nil && string(raw) == "# reviewed owner note")
	if _, err := ExecDocker(cid, "touch", "/workspace/skills/runtime-write.md"); err == nil {
		f.t.Fatal("runtime could write directly to its skills directory")
	}
}

func checkSkillPin(f *workspaceProof) {
	f.t.Helper()
	f.script("same-skill-session", []workspaceCheck{
		{"skills_read", `{"name":"own/notes"}`, locale.T(locale.EN, "err.not-found")},
	})
	input := f.load("new-skill-session", []workspaceStep{{
		SystemHas: []string{"own/notes"},
		Calls:     []workspaceCall{{"skills_read", `{"name":"own/notes"}`}},
	}, {
		ToolResult: "# reviewed owner note", Respond: "new conversation saw the skill",
	}})
	job, err := TaskClient(f.t, f.scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: f.scope.Space, Conversation: Idem("skills-new"), Input: input, Idem: Idem("skills-new"),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	got := WaitJob(f.t, f.scope, job.ID, "succeeded", 2*time.Minute)
	testkit.Deny(f.t, "sys.skill-catalog", got.Result == "new conversation saw the skill")
}

func revokePersona(f *workspaceProof) {
	f.t.Helper()
	act := f.activate("user", `{"grants":{"allow":["tools.use","workspace.read","workspace.write"]}}`)
	f.denyCall(act.Digest, "files", "files_write_skill", "cap.persona.edit_own")
	f.chat("persona-off", "list my current tools",
		"answer: tools [ask,choose,files_edit,files_list,files_mkdir,files_read,files_remove,files_search,files_write,skills_read,skills_search]")
}

func refuseMissingShowcase(f *workspaceProof) {
	f.t.Helper()
	cid := f.container()
	mount := `{{range .Mounts}}{{if eq .Destination "/etc/hhub/render"}}{{.RW}}{{end}}{{end}}`
	if got := strings.TrimSpace(string(DockerOutput(f.t, "inspect", "--format", mount, cid))); got != "false" {
		f.t.Fatalf("showcase render mount is not read-only: %q", got)
	}
	label := `{{index .Config.Labels "hhub.digest"}}`
	digest := strings.TrimSpace(string(DockerOutput(f.t, "inspect", "--format", label, cid)))
	render := path.Join("/render", f.scope.Space, digest, sv1.SkillShowcaseFile)
	DockerOutput(f.t, "exec", "spawn-controller", "mv", render, render+".saved")
	f.t.Cleanup(func() {
		if _, err := ExecDocker("spawn-controller", "mv", render+".saved", render); err != nil {
			f.t.Errorf("restore showcase for disposable space: %v", err)
		}
	})
	DockerOutput(f.t, "restart", cid)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d := testkit.NewDocker(f.t)
	code, err := d.Wait(ctx, cid)
	if err != nil {
		f.t.Fatal(err)
	}
	logs, err := d.Logs(ctx, cid)
	if err != nil {
		f.t.Fatal(err)
	}
	testkit.Deny(f.t, "sys.skill-showcase", code == 1 && strings.Contains(logs, "read layer skill showcase"))
}
