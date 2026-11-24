//go:build hhube2e

// Capability triples (phase 9): tools.use — the one capability with
// a real consumer this phase — gets three checks on one warm
// runtime: a granted call passes end to end, an ungranted tool is
// absent from the projection (it does not exist for the model), and
// a direct call without admission is refused. Grants are live
// leaves, so toggles apply without restarts; the meta check counts
// runtime starts against the distinct-projection budget.
package capability

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	thv1 "github.com/letya999/h-hub/contracts/toolhub/v1"
	"github.com/letya999/h-hub/shared/auth"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/testkit"
	. "github.com/letya999/h-hub/test/e2e/harness"
	"gopkg.in/yaml.v3"
)

// catalogCaps reads the capability names from the platform catalog —
// the triple set is generated from the same file grant validation
// resolves against, so a new catalog entry grows its own checks.
func catalogCaps(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(
		"/src/services/spawn-controller/internal/svc/platform/capabilities.yaml")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var f struct {
		Capabilities []struct {
			Name string `yaml:"name"`
		} `yaml:"capabilities"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("catalog parse: %v", err)
	}
	var out []string
	for _, c := range f.Capabilities {
		out = append(out, c.Name)
	}
	if len(out) == 0 {
		t.Fatal("catalog is empty")
	}
	return out
}

// setGrants replaces the user layer's allow list and activates the
// candidate, returning the new spec digest.
func setGrants(t *testing.T, scope OwnerSpace, allow []string) string {
	t.Helper()
	raw, err := json.Marshal(allow)
	if err != nil {
		t.Fatal(err)
	}
	c := SpawnClient(t, scope)
	ctx := context.Background()
	if _, err := c.SubmitLayers(ctx, sv1.SubmitLayersReq{
		Space: scope.Space, Role: "user",
		Docs: json.RawMessage(`{"grants":{"allow":` + string(raw) + `}}`)}); err != nil {
		t.Fatalf("submit grants: %v", err)
	}
	prev, err := c.Preview(ctx, sv1.PreviewReq{Space: scope.Space})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	act, err := c.Activate(ctx, sv1.ActivateReq{
		Space: scope.Space, Base: prev.Base, Candidate: prev.Candidate})
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	return act.Digest
}

// toolJob drives one real turn: the scripted model asks for the tool,
// the runtime calls it through tool-hub admission and the scripted
// answer lands as the job result.
func toolJob(t *testing.T, scope OwnerSpace, tool, mark string) tv1.JobInfo {
	t.Helper()
	sid := "s" + strings.ReplaceAll(Idem(mark), ".", "")
	input := "scenario:" + sid + " call"
	script := fmt.Sprintf("id: %s\nsteps:\n"+
		"  - expect: %s\n"+
		"    tool_calls:\n"+
		"      - name: %s\n"+
		"        arguments: %s\n"+
		"  - expect: %s\n"+
		"    respond: %s\n",
		sid, strconv.Quote(input), tool, strconv.Quote(`{"text":"ping"}`),
		strconv.Quote(input), strconv.Quote("cap "+mark+" done"))
	PostScenario(t, script, http.StatusCreated)
	res, err := TaskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: scope.Space, Conversation: "cap", Input: input, Idem: Idem("job", mark)})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	return WaitJob(t, scope, res.ID, "succeeded", 3*time.Minute)
}

// runToken mints a run token bound to the space and digest through
// the same task-engine edge the runtime uses; the returned client
// calls tool-hub with it.
func runToken(t *testing.T, scope OwnerSpace, digest, run string) *thv1.Client {
	t.Helper()
	key, err := auth.LoadPrivateKey("/etc/hhub/services/task-engine/service.key")
	if err != nil {
		t.Fatalf("task-engine key: %v", err)
	}
	ctx := context.Background()
	id := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, "")}}
	svc, err := id.IssueServiceToken(ctx, "task-engine", "identity-hub",
		Idem("mint", run), key)
	if err != nil {
		t.Fatalf("service token: %v", err)
	}
	mint := &idv1.Client{T: idv1.HTTP{C: httpapi.NewClient(IdentityURL, svc.Token)}}
	tok, err := mint.MintRunToken(ctx, idv1.MintRunTokenReq{
		Job: Idem("job", run), Run: run, Space: scope.Space, Owner: scope.Owner,
		Digest: digest, Aud: []string{"tool-hub"}})
	if err != nil {
		t.Fatalf("run token: %v", err)
	}
	return &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(ToolURL, tok.Token)}}
}

// hasTool reports whether the projection offers the named tool.
func hasTool(p thv1.ProjectionResp, tool string) bool {
	for _, ti := range append(append([]thv1.ToolInfo{}, p.Core...), p.Extended...) {
		if ti.Name == tool {
			return true
		}
	}
	return false
}

// receiptAllowed polls the ledger for the decision row of one call
// and returns its verdict; a missing row fails the poll.
func receiptAllowed(t *testing.T, key, kind, tool string) bool {
	t.Helper()
	db := AdminDB(t, "tool_hub")
	col := "job"
	if kind == "run" {
		col = "run"
	}
	var allowed bool
	testkit.Poll(t, 15*time.Second, 250*time.Millisecond, func() bool {
		return db.QueryRowContext(context.Background(),
			"SELECT allowed FROM receipts WHERE "+col+"=$1 AND tool=$2 ORDER BY seq DESC LIMIT 1",
			key, tool).Scan(&allowed) == nil
	})
	return allowed
}

// warmRuntime runs one job so the space owns a running container.
func warmRuntime(t *testing.T, scope OwnerSpace) string {
	t.Helper()
	res, err := TaskClient(t, scope).CreateJob(context.Background(), tv1.CreateJobReq{
		Space: scope.Space, Conversation: "cap",
		Input: "hi " + Idem("warm"), Idem: Idem("warm")})
	if err != nil {
		t.Fatalf("warm job: %v", err)
	}
	WaitJob(t, scope, res.ID, "succeeded", 3*time.Minute)
	return currentRuntime(t, scope.Space)
}

// currentRuntime returns the space's runtime container id.
func currentRuntime(t *testing.T, space string) string {
	t.Helper()
	db := AdminDB(t, "spawn_controller")
	var cid string
	if err := db.QueryRowContext(context.Background(),
		"SELECT container FROM runtimes WHERE space=$1", space).Scan(&cid); err != nil {
		t.Fatalf("runtime row: %v", err)
	}
	return cid
}

func without(caps []string, drop string) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if c != drop {
			out = append(out, c)
		}
	}
	return out
}

func TestCapabilityTriples(t *testing.T) {
	owner, space := "capability", EnsureSpace(t, "capability", Idem("cap"))
	scope := OwnerSpace{Owner: owner, Space: space}
	caps := catalogCaps(t)

	// The platform role opens all catalog names; the user layer then
	// selects from them — a user grant can only narrow what platform
	// allows.
	sc := SpawnClient(t, scope)
	all, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.SubmitLayers(context.Background(), sv1.SubmitLayersReq{
		Space: space, Role: "platform",
		Docs: json.RawMessage(`{"grants":{"allow":` + string(all) + `}}`)}); err != nil {
		t.Fatalf("platform grants: %v", err)
	}

	cid := warmRuntime(t, scope)
	starts := map[string]bool{cid: true}
	projections := map[string]bool{}

	// tools.use is the capability with a real consumer this phase:
	// probe_echo admits, projects and invokes through the same path
	// every later tool will use. The markers stay on the top-level
	// test — the trace gate keys req/deny proofs by test function.
	testkit.Req(t, "cap.tools.use")

	// Granted: the call passes end to end — model asks, admission
	// allows, executor answers, receipt lands.
	grantDigest := setGrants(t, scope, caps)
	projections[grantDigest] = true
	job := toolJob(t, scope, "probe_echo", "tools.use")
	if !receiptAllowed(t, job.ID, "job", "probe_echo") {
		t.Fatal("granted call for tools.use recorded a denial")
	}

	// Ungranted: the tool is absent from the projection — it does
	// not exist for the model (with no granted tool the projection
	// is refused outright) — and a direct call without admission is
	// refused.
	denyDigest := setGrants(t, scope, without(caps, "tools.use"))
	projections[denyDigest] = true
	run := Idem("run", "tools.use")
	tc := runToken(t, scope, denyDigest, run)
	p, err := tc.Projection(context.Background(), thv1.ProjectionReq{
		Run: run, Digest: denyDigest})
	absent := err != nil || !hasTool(p, "probe_echo")
	_, invErr := tc.Invoke(context.Background(), thv1.InvokeReq{
		Run: run, Digest: denyDigest, Toolset: "probe", Tool: "probe_echo",
		Arguments: json.RawMessage(`{"text":"ping"}`)})
	refused := herrors.CodeOf(invErr) == herrors.CodeForbidden
	if receiptAllowed(t, run, "run", "probe_echo") {
		t.Fatal("bypass call for tools.use recorded an allow")
	}
	testkit.Deny(t, "cap.tools.use", absent && refused)

	starts[currentRuntime(t, space)] = true

	// Meta check: runtime starts in this run may not exceed the
	// distinct projections plus kill scenarios (none here) — every
	// triple ran on the one warm runtime because grants are live.
	if len(starts) > len(projections) {
		t.Fatalf("runtime starts %d exceed projection budget %d",
			len(starts), len(projections))
	}

	// A token signed by a foreign key never passes admission — the
	// bypass path is refused before any decision is read.
	_, fkey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := json.Marshal(idv1.RunClaims{
		Sub: "task-engine", Job: "j", Run: "r", Space: space, Owner: owner,
		Digest: "d", Aud: []string{"tool-hub"},
		Exp: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	forged, err := auth.Sign(fkey, "run", claims)
	if err != nil {
		t.Fatal(err)
	}
	fc := &thv1.Client{T: thv1.HTTP{C: httpapi.NewClient(ToolURL, forged)}}
	_, ferr := fc.Projection(context.Background(), thv1.ProjectionReq{Run: "r", Digest: "d"})
	testkit.Deny(t, "cap.tools.use",
		ferr != nil && herrors.CodeOf(ferr) == herrors.CodeUnauthorized)
}

// TestGrantRevocation proves revocation is live: on one warm runtime
// a granted call passes, the grant is then removed, and without a
// runtime restart the next call is refused and the tool leaves the
// projection. The runtime container id before and after is the same.
func TestGrantRevocation(t *testing.T) {
	owner, space := "caprevoke", EnsureSpace(t, "caprevoke", Idem("revoke"))
	scope := OwnerSpace{Owner: owner, Space: space}
	caps := catalogCaps(t)

	sc := SpawnClient(t, scope)
	all, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.SubmitLayers(context.Background(), sv1.SubmitLayersReq{
		Space: space, Role: "platform",
		Docs: json.RawMessage(`{"grants":{"allow":` + string(all) + `}}`)}); err != nil {
		t.Fatalf("platform grants: %v", err)
	}

	warmRuntime(t, scope)
	testkit.Req(t, "sys.grant-revocation")

	// Granted: probe_echo passes end to end on this runtime.
	setGrants(t, scope, caps)
	job := toolJob(t, scope, "probe_echo", "revoke-grant")
	if !receiptAllowed(t, job.ID, "job", "probe_echo") {
		t.Fatal("granted call recorded a denial")
	}
	before := currentRuntime(t, space)

	// Revoked: the very next call on the same runtime is refused and
	// the tool is gone from the projection.
	denyDigest := setGrants(t, scope, without(caps, "tools.use"))
	run := Idem("run", "revoke")
	tc := runToken(t, scope, denyDigest, run)
	p, err := tc.Projection(context.Background(), thv1.ProjectionReq{
		Run: run, Digest: denyDigest})
	absent := err != nil || !hasTool(p, "probe_echo")
	_, invErr := tc.Invoke(context.Background(), thv1.InvokeReq{
		Run: run, Digest: denyDigest, Toolset: "probe", Tool: "probe_echo",
		Arguments: json.RawMessage(`{"text":"ping"}`)})
	refused := herrors.CodeOf(invErr) == herrors.CodeForbidden
	if receiptAllowed(t, run, "run", "probe_echo") {
		t.Fatal("call on a revoked grant recorded an allow")
	}
	if after := currentRuntime(t, space); after != before {
		t.Fatalf("runtime restarted: %s -> %s", before, after)
	}
	testkit.Deny(t, "sys.grant-revocation", absent && refused)
}
