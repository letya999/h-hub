package svc

import (
	"bytes"
	"errors"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/testkit"
)

func TestSpecDigestBindsImage(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.spawn-runtime.digest-pins-image")
	p := Passports{SchemaVersion: 2, Stack: StackPassport{Image: "sha256:first"}}
	_, first, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Stack.Image = "sha256:second"
	_, second, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("image change did not change effective-spec digest")
	}
	testkit.Deny(t, "adr.spawn-runtime.digest-pins-image", staleDigest(first, second))
}

func TestSpecSchemaVersion(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.schema-version")
	p := Passports{SchemaVersion: 2}
	spec, _, err := compile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(spec, []byte(`"schema_version":2`)) {
		t.Fatalf("new spec has no version: %s", spec)
	}
	for _, doc := range []string{`{"stack":{},"config":{},"models":{}}`, string(spec)} {
		if _, err := specPassports(spaceRow{spec: []byte(doc)}); err != nil {
			t.Fatalf("cannot read old/new spec: %v", err)
		}
	}
	_, err = specPassports(spaceRow{spec: []byte(`{"schema_version":3}`)})
	testkit.Deny(t, "sys.schema-version", err != nil)
}

// TestGrantNamesResolveToCatalog pins the catalog rule: a grant may
// hold only names the capability catalog declares — an unknown name
// fails validation, it never sits inert in the spec.
func TestGrantNamesResolveToCatalog(t *testing.T) {
	t.Parallel()
	p := Passports{Grants: GrantsPassport{Allow: []string{"tools.use", "bogus.cap"}}}
	if err := validateSpec(p); err == nil {
		t.Fatal("unknown grant name accepted")
	}
	p.Grants.Allow = []string{"tools.use", "memory.search"}
	if err := validateGrants(p.Grants, ""); err != nil {
		t.Fatalf("catalog names refused: %v", err)
	}
}

func TestUnknownLayerGrantIdentifiesSpace(t *testing.T) {
	t.Parallel()
	platform, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = compileSet(layerSet{
		{role: "platform", space: "space-17", docs: platform},
		{role: "user", space: "space-17", docs: DocSet{"grants": map[string]any{
			"allow": []any{"tools.use", "models.select"},
		}}},
	})
	var invalid *herrors.Error
	if !errors.As(err, &invalid) || invalid.Details["grant"] != "models.select" ||
		invalid.Details["space"] != "space-17" {
		t.Fatalf("unknown grant must name right and space: %v", err)
	}
}

// TestToolAdmissionFailClosed pins rule 13 for tool calls: a spec
// that selects toolsets without an admission url must refuse runs —
// quiet tool-less mode hides operator error. A spec selecting nothing
// runs without tools by design.
func TestToolAdmissionFailClosed(t *testing.T) {
	t.Parallel()
	if err := toolAdmission(Passports{
		Tools: ToolsPassport{Core: []string{"probe"}},
	}); err == nil {
		t.Fatal("toolsets without tool_url passed admission")
	}
	for _, p := range []Passports{
		{},
		{Tools: ToolsPassport{Core: []string{"probe"}},
			Config: ConfigPassport{ToolURL: "http://tool-hub:9000"}},
	} {
		if err := toolAdmission(p); err != nil {
			t.Fatal(err)
		}
	}
}

// route finds one named route in the passport table.
func route(m ModelsPassport, name string) ModelRoute {
	for _, rt := range m.Routes {
		if rt.Name == name {
			return rt
		}
	}
	return ModelRoute{}
}

// routeVision reports the vision flag of one named route.
func routeVision(m ModelsPassport, name string) bool {
	return route(m, name).Vision
}

// TestPlatformLayerHonestVision pins the live finding: the shipped
// route table must declare vision only where the upstream accepts
// image parts — the false flag on devin let photo intakes reach a
// provider that refuses them, and the drift hid in the second copy
// of the table.
func TestPlatformLayerHonestVision(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.model-route")
	m, err := modelDefaults()
	testkit.Deny(t, "sys.model-route", err == nil)
	testkit.Deny(t, "sys.model-route", !routeVision(m, "devin"))
	testkit.Deny(t, "sys.model-route", routeVision(m, "devin-vision"))
}

// TestApprovalsModeLeaf proves the config.approvals.mode leaf: the
// platform layer pins ask, a lower layer may choose inside the
// domain, and a value outside it is refused at compile — whoever
// wrote it.
func TestApprovalsModeLeaf(t *testing.T) {
	t.Parallel()
	platform, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	base := namedDocs{role: "platform", rev: "p1", docs: platform}
	out, err := compileSet(layerSet{base})
	if err != nil {
		t.Fatalf("platform compile: %v", err)
	}
	if out.spec.Config.Approvals.Mode != "ask" {
		t.Fatalf("platform mode = %q, want ask", out.spec.Config.Approvals.Mode)
	}
	userDocs := func(mode string) DocSet {
		return DocSet{"config": map[string]any{
			"approvals": map[string]any{"mode": mode}}}
	}
	for _, mode := range []string{"auto", "bypass"} {
		out, err := compileSet(layerSet{base,
			{role: "user", rev: "u1", docs: userDocs(mode)}})
		if err != nil {
			t.Fatalf("user chooses %s: %v", mode, err)
		}
		if out.spec.Config.Approvals.Mode != mode {
			t.Fatalf("merged mode = %q, want %s", out.spec.Config.Approvals.Mode, mode)
		}
	}
	bogus := userDocs("sometimes")
	if _, err := compileSet(layerSet{base,
		{role: "user", rev: "u1", docs: bogus}}); err == nil {
		t.Fatal("user layer wrote a mode outside the domain")
	}
	badDocs, err := layerDocs("platform/layer")
	if err != nil {
		t.Fatal(err)
	}
	setLeaf(badDocs, "config.approvals.mode", "sometimes")
	badBase := namedDocs{role: "platform", rev: "p2", docs: badDocs}
	if _, err := compileSet(layerSet{badBase}); err == nil {
		t.Fatal("platform layer wrote a mode outside the domain")
	}
}
