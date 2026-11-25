package svc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// layerSet is the ordered chain of revisions being compiled: index 0
// is always the platform layer.
type layerSet []namedDocs

type namedDocs struct {
	role string
	rev  string
	docs DocSet
}

// compiled is one compileSet result: the effective spec, its digest
// and per-leaf layer attribution.
type compiled struct {
	spec    Passports
	digest  string
	explain map[string]string
}

// compileSet merges the chain into the effective spec: each lower
// leaf must obey its registry class against the running result, then
// the typed spec is cross-validated and pinned by digest. Explain
// records which layer supplied each leaf.
func compileSet(chain layerSet) (compiled, error) {
	out := compiled{explain: map[string]string{}}
	merged := DocSet{}
	for i, l := range chain {
		if err := applyLayer(merged, out.explain, l, i == 0); err != nil {
			return out, err
		}
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return out, fmt.Errorf("spec marshal: %w", err)
	}
	if err := strictSpec(raw, &out.spec); err != nil {
		return out, err
	}
	out.spec.SchemaVersion = 2
	if err := validateSpec(out.spec); err != nil {
		return out, err
	}
	_, sum, err := compile(out.spec)
	if err != nil {
		return out, err
	}
	out.digest = sum
	return out, nil
}

// applyLayer folds one layer's leaves into the running merge. The
// base layer writes freely; every lower leaf goes through its
// registry class check first.
func applyLayer(merged DocSet, explain map[string]string, l namedDocs, base bool) error {
	for _, leaf := range registry.Leaves {
		v, ok := leafValue(l.docs, leaf.Path)
		if !ok {
			continue
		}
		if !base {
			parent, _ := leafValue(merged, leaf.Path)
			if err := checkNarrow(leaf, parent, v); err != nil {
				return fmt.Errorf("%s: %w", leaf.Path, err)
			}
		}
		setLeaf(merged, leaf.Path, v)
		explain[leaf.Path] = l.role
	}
	return nil
}

// strictSpec decodes canonical docs into the typed spec, refusing
// keys the spec does not declare.
func strictSpec(raw []byte, p *Passports) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	return nil
}

// validateSpec is the cross-document check over the compiled spec.
func validateSpec(p Passports) error {
	if err := validateModels(p.Models); err != nil {
		return err
	}
	if err := validateGrants(p.Grants); err != nil {
		return err
	}
	if m := p.Config.Approvals.Mode; m != "" && !slices.Contains(approvalModes, m) {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("leaf", "config.approvals.mode")
	}
	for f := range p.Workspace.Files {
		if f == "" || strings.Contains(f, "..") || strings.HasPrefix(f, "/") {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("file", f)
		}
	}
	return nil
}

// validateModels checks route names, the default's membership,
// cred:// key refs and the budget ordering.
func validateModels(m ModelsPassport) error {
	names := map[string]bool{}
	for _, rt := range m.Routes {
		if rt.Name == "" || names[rt.Name] || rt.URL == "" || rt.Model == "" {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("route", rt.Name)
		}
		if rt.KeyRef != "" && !strings.HasPrefix(rt.KeyRef, "cred://") {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("key_ref", rt.KeyRef)
		}
		names[rt.Name] = true
	}
	if !names[m.Default] {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("default", m.Default)
	}
	if m.Reserve < 0 || m.DailyTokens <= m.Reserve ||
		m.RunTokens <= m.Reserve {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("budget", "reserve")
	}
	return nil
}

// checkNarrow enforces the registry class of a leaf against the
// already-merged parent value: a lower layer may never widen it.
func checkNarrow(leaf Leaf, parent, v any) error {
	switch leaf.Class {
	case "invariant":
		if !reflect.DeepEqual(parent, v) {
			return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
											With("class", leaf.Class)
		}
	case "user_data":
		return nil
	case "choice":
		return checkChoice(leaf.Path, v)
	case "keyset-narrow":
		return checkKeys(leaf.Path, parent, v)
	case "narrow":
		return checkSubset(leaf.Path, parent, v)
	}
	return nil
}

// checkKeys enforces keyset-narrow: keys must be a subset of the
// parent's and each kept value equal to it.
func checkKeys(path string, parent, v any) error {
	pm, ok := parent.(map[string]any)
	if !ok {
		pm = map[string]any{}
	}
	vm, ok := v.(map[string]any)
	if !ok {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("leaf", path)
	}
	for k, val := range vm {
		if !reflect.DeepEqual(pm[k], val) {
			return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
											With("key", k)
		}
	}
	return nil
}

// checkSubset is the narrow rule: scalar ceilings may only shrink,
// lists may only lose entries, keyed lists may only lose whole keys
// (each kept element equal to the parent's).
func checkSubset(path string, parent, v any) error {
	switch pv := parent.(type) {
	case []any:
		vv, ok := v.([]any)
		if !ok {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", path)
		}
		return subsetList(path, pv, vv)
	case string:
		return narrowString(path, pv, v)
	case float64:
		return narrowNum(path, pv, v)
	case map[string]any:
		return checkKeys(path, pv, v)
	default:
		if !reflect.DeepEqual(parent, v) {
			return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
											With("leaf", path)
		}
	}
	return nil
}

// narrowNum lets a numeric ceiling only shrink.
func narrowNum(path string, parent float64, v any) error {
	f, ok := v.(float64)
	if !ok || f > parent {
		return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
										With("leaf", path)
	}
	return nil
}

// subsetList narrows a list leaf. Elements with a "name" key match by
// name and must equal the parent's element; scalars match by value.
func subsetList(path string, parent, v []any) error {
	for _, e := range v {
		if !memberOf(parent, e) {
			return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
											With("leaf", path)
		}
	}
	return nil
}

// memberOf reports whether e deep-equals an element of parent, keyed
// by "name" when the element carries one.
func memberOf(parent []any, e any) bool {
	for _, p := range parent {
		if reflect.DeepEqual(p, e) {
			return true
		}
	}
	return false
}

// narrowString compares resource strings: equal units with a smaller
// amount narrow; anything else only equality passes.
func narrowString(path string, parent string, v any) error {
	s, ok := v.(string)
	if !ok {
		return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
										With("leaf", path)
	}
	pn, pu := splitUnit(parent)
	vn, vu := splitUnit(s)
	if pn >= 0 && vn >= 0 && pu == vu && vn <= pn {
		return nil
	}
	if s == parent {
		return nil
	}
	return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
									With("leaf", path)
}

// splitUnit splits "256m" into (256, "m"); no numeric part → -1.
func splitUnit(s string) (float64, string) {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return -1, s
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return -1, s
	}
	return f, s[i:]
}

// approvalModes is the config.approvals.mode domain: ask requires
// the human's answer, auto and bypass resolve approvals at once —
// inside the grants, never beyond them.
var approvalModes = []string{"ask", "auto", "bypass"}

// checkChoice enforces enumerated leaves; domains live here, not in
// the registry, because they derive from the effective document.
func checkChoice(path string, v any) error {
	switch path {
	case "models.default":
		return nil // domain = effective routes; validateSpec enforces it
	case "config.approvals.mode":
		s, ok := v.(string)
		if !ok || !slices.Contains(approvalModes, s) {
			return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
											With("leaf", path)
		}
		return nil
	}
	return herrors.New(herrors.CodeInvalid, "err.forbidden"). //nolint:wrapcheck // shared error form
									With("leaf", path)
}
