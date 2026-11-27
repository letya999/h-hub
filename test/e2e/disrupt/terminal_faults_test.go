//go:build hhube2e

package disrupt

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
)

func TestTerminalWorkerFaults(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.kill-uncertain")
	killed := startFaultTerminal(t, "killed")
	if err := killed.docker.Kill(t.Context(), killed.container); err != nil {
		t.Fatal(err)
	}
	assertTerminalUncertain(t, <-killed.done)
	assertTerminalUncertain(t, killed.replay(t))
	testkit.Deny(t, "sys.kill-uncertain", killed.dispatches(t) == 1)
	killed.waitForRetirement(t)

	restarted := startFaultTerminal(t, "restarted")
	if err := restarted.docker.Restart(t.Context(), "tool-hub"); err != nil {
		t.Fatal(err)
	}
	if err := <-restarted.done; err == nil {
		t.Fatal("in-flight terminal response survived restart")
	}
	health := httpapi.NewClientTimeout(ToolURL, "", time.Second)
	testkit.Poll(t, 12*time.Second, 100*time.Millisecond, func() bool {
		return health.Do(t.Context(), http.MethodGet, "/healthz", nil, nil) == nil
	})
	assertTerminalUncertain(t, restarted.replay(t))
	restarted.assertLive(t)
}

type terminalFault struct {
	scope      OwnerSpace
	req        thv1.InvokeReq
	client     *thv1.Client
	docker     *testkit.Docker
	worker     string
	container  string
	generation string
	lease      string
	done       chan error
}

func startFaultTerminal(t *testing.T, mark string) *terminalFault {
	t.Helper()
	owner := "terminal_" + mark + Nonce
	scope := OwnerSpace{Owner: owner, Space: EnsureSpace(t, owner, "terminal")}
	digest := activateFaultTerminal(t, scope)
	run := Idem("terminal-run", mark)
	token := MintRunToken(t, scope, Idem("terminal-job", mark), run, digest)
	f := &terminalFault{scope: scope, docker: testkit.NewDocker(t), done: make(chan error, 1),
		client: &thv1.Client{T: thv1.HTTP{C: httpapi.NewClientTimeout(ToolURL, token, 40*time.Second)}},
		req: thv1.InvokeReq{Run: run, Digest: digest, Toolset: "terminal", Tool: "terminal_exec", Key: Idem(mark),
			Arguments: json.RawMessage(`{"argv":["python3","-c","from pathlib import Path; import time; Path('started').write_text('running'); time.sleep(25)"],"workdir":"files","timeout_ms":30000}`)}}
	go func() { f.done <- f.replay(t) }()
	f.waitUntilRunning(t)
	return f
}

func activateFaultTerminal(t *testing.T, scope OwnerSpace) string {
	t.Helper()
	c := SpawnClient(t, scope)
	layers := []sv1.SubmitLayersReq{
		{Space: scope.Space, Role: "platform", Docs: json.RawMessage(`{"tools":{"core":["terminal"]}}`)},
		{Space: scope.Space, Role: "user", Docs: json.RawMessage(`{"grants":{"allow":["tools.use","terminal.exec"]},"config":{"approvals":{"mode":"auto"}}}`)},
	}
	var digest string
	for _, layer := range layers {
		if _, err := c.SubmitLayers(t.Context(), layer); err != nil {
			t.Fatal(err)
		}
		p, err := c.Preview(t.Context(), sv1.PreviewReq{Space: scope.Space})
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Activate(t.Context(), sv1.ActivateReq{Space: scope.Space, Base: p.Base, Candidate: p.Candidate})
		if err != nil {
			t.Fatal(err)
		}
		digest = res.Digest
	}
	return digest
}

func (f *terminalFault) replay(t *testing.T) error {
	t.Helper()
	_, err := f.client.Invoke(t.Context(), f.req)
	return err
}

func (f *terminalFault) waitUntilRunning(t *testing.T) {
	t.Helper()
	db := AdminDB(t, "spawn_controller")
	testkit.Poll(t, 12*time.Second, 100*time.Millisecond, func() bool {
		ctx := t.Context()
		err := db.QueryRowContext(ctx, `SELECT id, runtime, lease FROM intents WHERE kind='worker' AND space=$1`,
			f.scope.Space).Scan(&f.worker, &f.generation, &f.lease)
		if err != nil {
			return false
		}
		list, err := f.docker.List(ctx, "hhub.generation="+f.generation)
		if err != nil || len(list) != 1 {
			return false
		}
		f.container = list[0].ID
		raw, err := ExecDocker(f.container, "cat", "/workspace/files/started")
		return err == nil && string(raw) == "running"
	})
}

func (f *terminalFault) waitForRetirement(t *testing.T) {
	t.Helper()
	db := AdminDB(t, "spawn_controller")
	testkit.Poll(t, 10*time.Second, 100*time.Millisecond, func() bool {
		var state, reason string
		err := db.QueryRowContext(t.Context(), `SELECT m.state, i.stop_reason FROM intents i JOIN fsm.machines m
			ON m.kind=i.kind AND m.id=i.id WHERE i.id=$1 AND i.runtime=$2`, f.worker, f.generation).Scan(&state, &reason)
		return err == nil && state == "absent" && reason == "terminal-outcome-uncertain"
	})
}

func (f *terminalFault) assertLive(t *testing.T) {
	t.Helper()
	inspect, err := f.docker.ContainerInspect(t.Context(), f.container)
	if err != nil || !inspect.State.Running || inspect.Config.Labels["hhub.generation"] != f.generation {
		t.Fatalf("tool-hub restart lost the worker: running=%t, err=%v", inspect.State.Running, err)
	}
	if got := f.dispatches(t); got != 1 {
		t.Fatalf("restart issued %d command attempts", got)
	}
	state, err := WorkerControlClient(t).ClaimWorker(t.Context(), sv1.ClaimWorkerReq{
		Owner: f.scope.Owner, Space: f.scope.Space, Profile: "terminal",
		Digest: f.req.Digest, Key: f.lease,
	})
	if err != nil || !state.Ready || state.Generation != f.generation || state.Worker != f.worker {
		t.Fatalf("restart lost the active lease: ready=%t generation=%q err=%v", state.Ready, state.Generation, err)
	}
}

func (f *terminalFault) dispatches(t *testing.T) int {
	t.Helper()
	var n int
	if err := AdminDB(t, "tool_hub").QueryRowContext(t.Context(),
		"SELECT count(*) FROM fsm.outbox WHERE kind='terminal-call' AND machine_id=$1 AND topic='execute-terminal-once'",
		f.lease).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertTerminalUncertain(t *testing.T, err error) {
	t.Helper()
	var outcome *herrors.Error
	if !errors.As(err, &outcome) || outcome.Code != herrors.CodeUncertain || outcome.Retryable {
		t.Fatalf("command should have an uncertain outcome without retry: %v", err)
	}
}
