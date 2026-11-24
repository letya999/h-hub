package svc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	herrors "github.com/letya999/h-hub/shared/errors"
	"gopkg.in/yaml.v3"
)

// DocSet is one layer revision: the six passport documents as a JSON
// object. Missing documents inherit the upper layer; a document's
// leaves are addressed by registry paths (stack.identity.owner …).
type DocSet map[string]any

// parseDocs decodes a revision strictly: only the six sections, and
// every key path must be a registry leaf or sit inside one.
func parseDocs(raw json.RawMessage) (DocSet, error) {
	var docs DocSet
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&docs); err != nil {
		return nil, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err)
	}
	for section := range docs {
		if !docSections[section] {
			return nil, herrors.New(herrors.CodeInvalid, //nolint:wrapcheck // shared error form
				"err.invalid-arg").With("section", section)
		}
	}
	if err := checkLeaves(docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// checkLeaves rejects paths that no registry leaf declares: a leaf
// without a consumer never enters a revision.
func checkLeaves(docs DocSet) error {
	for section, doc := range docs {
		if err := checkLeafMap(section, doc); err != nil {
			return err
		}
	}
	return nil
}

// checkLeafMap walks one document; a declared leaf's subtree is data,
// anything else must be a prefix of one.
func checkLeafMap(prefix string, v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for k, val := range m {
		p := prefix + "." + k
		if _, isLeaf := registry.leaf(p); isLeaf {
			continue
		}
		if !isLeafPrefix(p) {
			return herrors.New(herrors.CodeInvalid, "err.invalid-arg"). //nolint:wrapcheck // shared error form
											With("leaf", p)
		}
		if err := checkLeafMap(p, val); err != nil {
			return err
		}
	}
	return nil
}

// isLeafPrefix reports whether path is a strict prefix of a leaf.
func isLeafPrefix(path string) bool {
	for p := range registry.byPath {
		if strings.HasPrefix(p, path+".") {
			return true
		}
	}
	return false
}

// leafValue extracts a leaf path's value from docs; ok=false when the
// layer does not write the leaf.
func leafValue(docs DocSet, path string) (any, bool) {
	var node any = map[string]any(docs)
	for _, part := range strings.Split(path, ".") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		node, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return node, true
}

// setLeaf writes a leaf path into a docs-shaped map.
func setLeaf(docs DocSet, path string, v any) {
	parts := strings.Split(path, ".")
	node := docs
	for _, part := range parts[:len(parts)-1] {
		next, ok := node[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			node[part] = next
		}
		node = next
	}
	node[parts[len(parts)-1]] = v
}

// layerDocs reads a directory of passport documents into one DocSet.
// Documents ship as yaml; values normalize to JSON types so merges
// and digests are stable.
func layerDocs(dir string) (DocSet, error) {
	docs := DocSet{}
	entries, err := fs.ReadDir(platformFS, dir)
	if err != nil {
		return nil, fmt.Errorf("layer dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := platformFS.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("layer doc %s: %w", e.Name(), err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, e.Name(), err)
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		if d, ok := normYAML(doc).(map[string]any); ok {
			docs[name] = d
		}
	}
	return docs, nil
}

// normYAML rewrites yaml scalars into their JSON equivalents —
// integers become float64, nested maps stay map[string]any.
func normYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normYAML(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normYAML(val)
		}
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	default:
		return v
	}
}

// platformLayer is the shipped platform layer plus per-space facts
// (owner binding, pinned image, declared URLs) written at create.
func platformLayer(s *Server, owner, image string) (DocSet, error) {
	docs, err := layerDocs("platform/layer")
	if err != nil {
		return nil, err
	}
	setLeaf(docs, "stack.identity.owner", owner)
	setLeaf(docs, "stack.image", image)
	setLeaf(docs, "config.task_url", s.deps.TaskURL)
	setLeaf(docs, "config.model_url", s.deps.ModelURL)
	return docs, nil
}

// templateLayer reads a named template folder: its documents become
// the space's user layer and its md files land in workspace.files.
func templateLayer(name string) (DocSet, error) {
	if name != "assistant" && name != "restricted" {
		return nil, herrors.New(herrors.CodeInvalid, //nolint:wrapcheck // shared error form
			"err.missing-input").With("template", name)
	}
	dir := "platform/templates/" + name
	docs, err := layerDocs(dir)
	if err != nil {
		return nil, err
	}
	files := map[string]any{}
	entries, err := fs.ReadDir(platformFS, dir)
	if err != nil {
		return nil, fmt.Errorf("template dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := platformFS.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("template file %s: %w", e.Name(), err)
		}
		files[e.Name()] = string(data)
	}
	setLeaf(docs, "workspace.files", files)
	return docs, checkLeaves(docs)
}
