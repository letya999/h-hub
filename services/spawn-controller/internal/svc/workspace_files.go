package svc

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

func listWorkspace(root *os.Root, rel string, _ sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	entries, err := boundedDirectory(root, rel)
	if err != nil {
		return sv1.WorkspaceResp{}, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return sv1.WorkspaceResp{Entries: names}, nil
}

func boundedDirectory(root *os.Root, rel string) ([]os.DirEntry, error) {
	dir, err := root.Open(rel)
	if err != nil {
		return nil, workspaceIO(err)
	}
	defer dir.Close() //nolint:errcheck // no writes on the directory handle
	entries, err := dir.ReadDir(maxWorkspaceEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, workspaceIO(err)
	}
	if len(entries) > maxWorkspaceEntries {
		return nil, fileRefusal("err.workspace-entries")
	}
	return entries, nil
}

func readWorkspace(root *os.Root, rel string, _ sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	text, err := workspaceText(root, rel)
	return sv1.WorkspaceResp{Text: text}, err
}

func workspaceText(root *os.Root, rel string) (string, error) {
	info, err := root.Lstat(rel)
	if err != nil {
		return "", workspaceIO(err)
	}
	if !info.Mode().IsRegular() {
		return "", fileRefusal("err.workspace-file")
	}
	file, err := root.Open(rel)
	if err != nil {
		return "", workspaceIO(err)
	}
	defer file.Close() //nolint:errcheck // opened read-only
	data, err := io.ReadAll(io.LimitReader(file, maxWorkspaceBytes+1))
	if err != nil {
		return "", workspaceIO(err)
	}
	if len(data) > maxWorkspaceBytes {
		return "", fileRefusal("err.workspace-size")
	}
	if !utf8.Valid(data) {
		return "", fileRefusal("err.workspace-text")
	}
	return string(data), nil
}

func mkdirWorkspace(root *os.Root, rel string, _ sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	if err := root.Mkdir(rel, 0o755); err != nil {
		return sv1.WorkspaceResp{}, workspaceIO(err)
	}
	return sv1.WorkspaceResp{}, nil
}

func removeWorkspace(root *os.Root, rel string, _ sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	if err := root.Remove(rel); err != nil {
		return sv1.WorkspaceResp{}, workspaceIO(err)
	}
	return sv1.WorkspaceResp{}, nil
}

func replaceWorkspace(root *os.Root, rel string, req sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	return sv1.WorkspaceResp{}, atomicWorkspaceWrite(root, rel, req.Text)
}

func editWorkspace(root *os.Root, rel string, req sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	var out sv1.WorkspaceResp
	if req.Before == "" {
		return out, fileRefusal("err.workspace-edit")
	}
	old, err := workspaceText(root, rel)
	if err != nil {
		return out, err
	}
	if strings.Count(old, req.Before) != 1 {
		return out, fileRefusal("err.workspace-edit")
	}
	return out, atomicWorkspaceWrite(root, rel, strings.Replace(old, req.Before, req.Text, 1))
}

func atomicWorkspaceWrite(root *os.Root, rel, text string) (retErr error) {
	if len(text) > maxWorkspaceBytes {
		return fileRefusal("err.workspace-size")
	}
	temp := path.Join(path.Dir(rel), "."+newID("hhub-write"))
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return workspaceIO(err)
	}
	defer func() {
		if retErr != nil {
			if err := root.Remove(temp); err != nil && !errors.Is(err, fs.ErrNotExist) {
				retErr = errors.Join(retErr, fmt.Errorf("remove staged workspace file: %w", err))
			}
		}
	}()
	_, writeErr := io.WriteString(file, text)
	var syncErr error
	if writeErr == nil {
		syncErr = file.Sync()
	}
	if err := errors.Join(writeErr, syncErr, file.Close()); err != nil {
		return workspaceIO(err)
	}
	if err := root.Rename(temp, rel); err != nil {
		return workspaceIO(err)
	}
	return nil
}

func workspaceIO(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return herrors.Wrap(herrors.CodeNotFound, "err.workspace-missing", err)
	}
	return herrors.Wrap(herrors.CodeInvalid, "err.workspace-file", err)
}

func searchWorkspace(root *os.Root, rel string, r sv1.WorkspaceReq) (sv1.WorkspaceResp, error) {
	var out sv1.WorkspaceResp
	if r.Query == "" || len(r.Query) > maxWorkspaceBytes {
		return out, fileRefusal("err.invalid-arg")
	}
	search := workspaceSearch{root: root, query: r.Query}
	if err := search.scan(rel, strings.Count(r.Path, "/")+1); err != nil {
		return out, err
	}
	out.Entries = search.matches
	slices.Sort(out.Entries)
	return out, nil
}

type workspaceSearch struct {
	root    *os.Root
	query   string
	seen    int
	matches []string
}

func (s *workspaceSearch) scan(dir string, depth int) error {
	entries, err := boundedDirectory(s.root, dir)
	if err != nil {
		return err
	}
	s.seen += len(entries)
	if s.seen > maxWorkspaceEntries {
		return fileRefusal("err.workspace-entries")
	}
	if len(entries) > 0 && depth >= maxWorkspaceDepth {
		return fileRefusal("err.workspace-depth")
	}
	for _, entry := range entries {
		name := path.Join(dir, entry.Name())
		if err := s.visit(name, entry, depth); err != nil {
			return err
		}
	}
	return nil
}

func (s *workspaceSearch) visit(name string, entry os.DirEntry, depth int) error {
	if entry.IsDir() {
		return s.scan(name, depth+1)
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fileRefusal("err.workspace-symlink")
	}
	text, err := workspaceText(s.root, name)
	if err != nil {
		return err
	}
	if strings.Contains(text, s.query) {
		s.matches = append(s.matches, name)
	}
	return nil
}
