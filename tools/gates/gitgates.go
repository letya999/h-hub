package gates

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const bannedModule = "github.com/letya999/hermes-hub"
const blobLimit = 256 * 1024

// ModuleBan rejects the v0 module in go.mod/go.sum (rule 32).
func ModuleBan(root string) error {
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("module-ban: read %s: %w", name, err)
		}
		if strings.Contains(string(data), bannedModule) {
			return fmt.Errorf("module-ban: %s references %s", name, bannedModule)
		}
	}
	return nil
}

// BlobSize rejects tracked files over ~256 KB (enforcement.md 7).
// Without a git repo every file under root is checked.
func BlobSize(root string) error {
	files, err := tracked(root)
	if err != nil {
		return err
	}
	if files == nil {
		return walk(root, func(rel string, d fs.DirEntry) error {
			if d.IsDir() {
				return nil
			}
			return sizeCheck(filepath.Join(root, rel), rel)
		})
	}
	for _, rel := range files {
		if strings.HasPrefix(rel, brokenPrefix+"/") {
			continue
		}
		if err := sizeCheck(filepath.Join(root, rel), rel); err != nil {
			return err
		}
	}
	return nil
}

func sizeCheck(abs, rel string) error {
	st, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return nil // tracked but absent locally: nothing to measure
	}
	if err != nil {
		return fmt.Errorf("blob-size: stat %s: %w", rel, err)
	}
	if st.Size() > blobLimit {
		return fmt.Errorf("blob-size: %s is %d bytes, limit %d", rel, st.Size(), blobLimit)
	}
	return nil
}

// tracked lists `git ls-files` paths, or nil when root is not a repo.
func tracked(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return nil, nil
	}
	b, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("tracked: git ls-files: %w", err)
	}
	var out []string
	for _, p := range strings.Split(string(b), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

var fixSubjectRe = regexp.MustCompile(`^fix[:(]`)

// FixNeedsTest rejects a fix commit that changes no test (rule 21).
// It checks HEAD in a git repo; a fixture dir provides subject.txt and
// files.txt instead.
func FixNeedsTest(root string) error {
	subject, files, err := commitInfo(root)
	if err != nil {
		return err
	}
	return checkCommit(subject, files)
}

func checkCommit(subject string, files []string) error {
	if !fixSubjectRe.MatchString(subject) {
		return nil
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			return nil
		}
	}
	return fmt.Errorf("fix-needs-test: %q changes no *_test.go", subject)
}

func commitInfo(root string) (string, []string, error) {
	if b, err := os.ReadFile(filepath.Join(root, "subject.txt")); err == nil {
		f, err := os.ReadFile(filepath.Join(root, "files.txt"))
		if err != nil {
			return "", nil, fmt.Errorf("commit files.txt: %w", err)
		}
		return strings.TrimSpace(string(b)), strings.Fields(string(f)), nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return "", nil, nil
	}
	subj, err := exec.Command("git", "-C", root, "show", "-s", "--format=%s", "HEAD").Output()
	if err != nil {
		return "", nil, fmt.Errorf("commit info: %w", err)
	}
	names, err := exec.Command("git", "-C", root, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		return "", nil, fmt.Errorf("commit files: %w", err)
	}
	return strings.TrimSpace(string(subj)), strings.Fields(string(names)), nil
}
