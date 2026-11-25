// Package archtest holds the AST checks of enforcement.md section 3
// that lint rules cannot express: per-type method and struct-field
// ceilings and the per-service line budget.
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxMethods      = 20
	maxFields       = 20
	maxServiceLines = 8000
	brokenPrefix    = "testdata/broken"
)

// moduleBudgets pins production line ceilings for service modules
// (internal packages) that must stay small enough to review in one
// sitting; a module enters the table with its owning feature.
var moduleBudgets = map[string]int{
	"tool-hub/ownerreq": 800,
}

// Check runs all AST checks over root.
func Check(root string) error {
	fset := token.NewFileSet()
	types := map[string]*typeInfo{}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("archtest rel: %w", err)
		}
		if skip(filepath.ToSlash(rel), d) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		files = append(files, p)
		return collect(fset, p, filepath.ToSlash(filepath.Dir(rel)), types)
	})
	if err != nil {
		return fmt.Errorf("archtest walk: %w", err)
	}
	for _, t := range types {
		if err := t.check(); err != nil {
			return err
		}
	}
	if err := serviceLines(root, files); err != nil {
		return err
	}
	return moduleLines(root, files)
}

// skip reports whether a walk path is excluded (.git, broken examples).
func skip(rel string, d fs.DirEntry) bool {
	if d.IsDir() && rel == ".git" {
		return true
	}
	if rel == brokenPrefix || strings.HasPrefix(rel, brokenPrefix+"/") {
		return true
	}
	return false
}

type typeInfo struct {
	name    string
	methods int
	fields  int
}

func (t *typeInfo) check() error {
	if t.methods > maxMethods {
		return fmt.Errorf("archtest: type %s has %d methods, max %d", t.name, t.methods, maxMethods)
	}
	if t.fields > maxFields {
		return fmt.Errorf("archtest: type %s has %d fields, max %d", t.name, t.fields, maxFields)
	}
	return nil
}

// collect parses one file and accumulates method/field counts. Types
// are keyed by directory so same-named types in different packages are
// counted separately — the rule is per-type, not per-identifier.
func collect(fset *token.FileSet, path, dir string, types map[string]*typeInfo) error {
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("archtest parse %s: %w", path, err)
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			addMethod(types, dir, d)
		case *ast.GenDecl:
			addStruct(types, dir, d)
		}
	}
	return nil
}

func addMethod(types map[string]*typeInfo, dir string, d *ast.FuncDecl) {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return
	}
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return
	}
	info(types, dir+"/"+id.Name).methods++
}

func addStruct(types map[string]*typeInfo, dir string, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		t := info(types, dir+"/"+ts.Name.Name)
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 {
				t.fields++
				continue
			}
			t.fields += len(f.Names)
		}
	}
}

func info(types map[string]*typeInfo, name string) *typeInfo {
	t, ok := types[name]
	if !ok {
		t = &typeInfo{name: name}
		types[name] = t
	}
	return t
}

// lineCounts buckets production lines of files by keyOf; empty keys
// are skipped.
func lineCounts(root string, files []string, keyOf func(string, string) (string, error)) (map[string]int, error) {
	per := map[string]int{}
	for _, p := range files {
		key, err := keyOf(root, p)
		if err != nil {
			return nil, err
		}
		if key == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("archtest read %s: %w", p, err)
		}
		per[key] += strings.Count(string(data), "\n") + 1
	}
	return per, nil
}

// serviceLines enforces the per-service production line budget.
func serviceLines(root string, files []string) error {
	perSvc, err := lineCounts(root, files, svcOf)
	if err != nil {
		return err
	}
	for svc, n := range perSvc {
		if n > maxServiceLines {
			return fmt.Errorf("archtest: service %s has %d production lines, max %d", svc, n, maxServiceLines)
		}
	}
	return nil
}

// svcOf returns the service name owning a file, or "" outside services.
func svcOf(root, p string) (string, error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", fmt.Errorf("archtest rel: %w", err)
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
	if len(parts) != 3 || parts[0] != "services" || strings.HasSuffix(rel, "_test.go") {
		return "", nil
	}
	return parts[1], nil
}

// moduleLines enforces the pinned per-module production line budgets.
func moduleLines(root string, files []string) error {
	perMod, err := lineCounts(root, files, modOf)
	if err != nil {
		return err
	}
	for mod, max := range moduleBudgets {
		if n := perMod[mod]; n > max {
			return fmt.Errorf("archtest: module %s has %d production lines, max %d", mod, n, max)
		}
	}
	return nil
}

// modOf returns "<service>/<module>" for files under a budgeted
// internal package, or "" elsewhere.
func modOf(root, p string) (string, error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", fmt.Errorf("archtest rel: %w", err)
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 5)
	if len(parts) != 5 || parts[0] != "services" || parts[2] != "internal" ||
		strings.HasSuffix(rel, "_test.go") {
		return "", nil
	}
	return parts[1] + "/" + parts[3], nil
}
