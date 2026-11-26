//go:build hhube2e

package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestHermesPinnedProcess(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.harness-pinned")
	owner := "hermes_image_" + Nonce
	scope := OwnerSpace{Owner: owner, Space: EnsureSpace(t, owner, Idem("hermes-pin"))}
	cid := warmRuntime(t, scope)
	raw, err := os.ReadFile("/src/deploy/hermes-image.ref")
	if err != nil {
		t.Fatal(err)
	}
	ref := strings.TrimSpace(string(raw))
	image := strings.TrimSpace(string(DockerOutput(t, "inspect", "--format", "{{.Config.Image}}", cid)))
	top := string(DockerOutput(t, "top", cid, "-eo", "pid,args"))
	if image != ref || !strings.Contains(top, "/opt/hermes/.venv/bin/python -P -m hermes_cli.main gateway run") {
		t.Fatalf("expected a pinned, running upstream process, got %s\n%s", image, top)
	}
	for _, path := range []string{"/var/tmp/config.yaml", "/var/tmp/.env", "/var/tmp/.op.env"} {
		if _, err := ExecDocker(cid, "sh", "-c", `exec 3>>"$1"`, "sh", path); err == nil {
			t.Fatalf("Hermes can write its launch policy at %s", path)
		}
	}
	tag, _, _ := strings.Cut(ref, "@")
	badDigest := "sha256:" + strings.Repeat("a", 64)
	c := SpawnClient(t, scope)
	db := AdminDB(t, "spawn_controller")
	service := TaskServiceSpawnClient(t, scope.Space)
	for _, image := range []string{tag, tag + "@" + badDigest, badDigest} {
		doc, err := json.Marshal(map[string]any{"stack": map[string]string{"image": image}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.SubmitLayers(context.Background(), sv1.SubmitLayersReq{Space: scope.Space, Role: "platform", Docs: doc})
		testkit.Deny(t, "sys.harness-pinned", herrors.CodeOf(err) == herrors.CodeInvalid)
		// Fault-inject a corrupted stored passport as well: a warm container cannot mask it.
		if _, err := db.ExecContext(context.Background(), `UPDATE spaces SET
			spec=jsonb_set(spec,'{stack,image}',to_jsonb($2::text)) WHERE id=$1`, scope.Space, image); err != nil {
			t.Fatal(err)
		}
		ready, err := service.EnsureRuntime(context.Background(), sv1.EnsureRuntimeReq{Space: scope.Space})
		testkit.Deny(t, "sys.harness-pinned", !ready.Ready && herrors.CodeOf(err) == herrors.CodeInvalid)
	}
}

func TestHermesPassportTools(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.harness-tool-list")
	owner := "hermes_tools_" + Nonce
	space, code := AddUser(t, owner)
	scope := OwnerSpace{Owner: owner, Space: space}
	cursor := int64(0)
	talk := func(text, answer string) {
		t.Helper()
		_, err := MockClient(t, owner, "Inject").Inject(context.Background(), cv1.InjectReq{
			External: owner, UpdateID: Idem("tools", owner, strconv.FormatInt(cursor, 10)), Text: text})
		if err != nil {
			t.Fatal(err)
		}
		cursor = WaitMock(t, owner, cursor, answer, 3*time.Minute)
	}
	talk("/start "+code, "Account linked")
	previous := ""
	for _, names := range [][]string{{}, {"todo_list"}, {}} {
		digest := activateNative(t, scope, names)
		talk("list my current tools", "answer: tools ["+strings.Join(names, ",")+"]")
		if len(names) == 1 {
			probeNativeTodo(t, talk)
		}
		cid := strings.TrimSpace(string(DockerOutput(t, "ps", "-q", "--filter", "label=hhub.space="+space,
			"--filter", "label=hhub.digest="+digest)))
		if cid == "" || cid == previous {
			t.Fatalf("expected a new runtime for %v, got %q", names, cid)
		}
		if previous != "" {
			rejectToolMismatch(t, scope.Space, digest, cid, len(names) == 0)
		}
		previous = cid
	}
}

func probeNativeTodo(t *testing.T, talk func(string, string)) {
	t.Helper()
	id := "native_todo_" + Nonce
	script := strings.ReplaceAll(`id: ID
steps:
  - expect: "scenario:ID read"
    tool_calls:
      - name: todo_list
        arguments: "{}"
  - expect: "scenario:ID read"
    expect_tool_result: '"total": 0'
    respond: "native todo list is empty"
`, "ID", id)
	PostScenario(t, script, http.StatusCreated)
	talk("scenario:"+id+" read", "native todo list is empty")
}

func activateNative(t *testing.T, scope OwnerSpace, names []string) string {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"tools": map[string][]string{"core": {}, "extended": {}, "native": names}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = SpawnClient(t, scope).SubmitLayers(context.Background(), sv1.SubmitLayersReq{
		Space: scope.Space, Role: "platform", Docs: doc})
	if err != nil {
		t.Fatal(err)
	}
	_, act := apply(t, scope)
	return act.Digest
}

func rejectToolMismatch(t *testing.T, space, digest, cid string, extra bool) {
	t.Helper()
	raw, err := ExecDocker(cid, "cat", "/etc/hhub/render/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	actual := []string{}
	reason := "missing a projected tool"
	if extra {
		actual = append(actual, "todo")
		reason = "unexpected Hermes tool"
	}
	cfg["platform_toolsets"] = map[string][]string{"api_server": actual}
	cfg["agent"] = map[string][]string{"disabled_toolsets": {}}
	corrupt, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	write := func(value []byte) error {
		_, err := ExecDocker("spawn-controller", "sh", "-c", `printf %s "$1" > "$2"`, "sh",
			string(value), "/render/"+space+"/"+digest+"/config.yaml")
		return err
	}
	if err := write(corrupt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := write(raw); err != nil {
			t.Errorf("restore the disposable space's config: %v", err)
		}
	})
	DockerOutput(t, "restart", cid)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d := testkit.NewDocker(t)
	code, err := d.Wait(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := d.Logs(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	testkit.Deny(t, "sys.harness-tool-list", code == 1 && strings.Contains(logs, reason))
}
