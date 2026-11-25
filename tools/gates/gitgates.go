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
// Three inputs: a fixture dir (subject.txt/files.txt), the commit being
// created (checkStaged, driven by .githooks/commit-msg through the
// -commit-msg test flag), or the range origin/main..HEAD that CI merges.
func FixNeedsTest(root string) error {
	if _, err := os.Stat(filepath.Join(root, "subject.txt")); err == nil {
		subject, files, err := fixtureCommit(root)
		if err != nil {
			return err
		}
		return checkCommit(subject, files)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return nil
	}
	return checkRange(root)
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

// fixtureCommit reads a broken example's recorded commit metadata.
func fixtureCommit(root string) (string, []string, error) {
	b, err := os.ReadFile(filepath.Join(root, "subject.txt"))
	if err != nil {
		return "", nil, fmt.Errorf("commit subject.txt: %w", err)
	}
	f, err := os.ReadFile(filepath.Join(root, "files.txt"))
	if err != nil {
		return "", nil, fmt.Errorf("commit files.txt: %w", err)
	}
	return strings.TrimSpace(string(b)), strings.Fields(string(f)), nil
}

// checkStaged verifies the commit a commit-msg hook is validating:
// subject from the prepared message file, files from the staged tree.
// A relative msgFile resolves against root. An amend/reword commit
// stages nothing; the resulting commit keeps HEAD's file list, so the
// check falls back to it.
func checkStaged(root, msgFile string) error {
	if !filepath.IsAbs(msgFile) {
		msgFile = filepath.Join(root, msgFile)
	}
	subject, err := msgSubject(msgFile)
	if err != nil {
		return err
	}
	out, err := gitOut(root, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	files := strings.Fields(out)
	if len(files) == 0 {
		out, err = gitOut(root, "show", "--name-only", "--format=", "HEAD")
		if err != nil {
			return err
		}
		files = strings.Fields(out)
	}
	return checkCommit(subject, files)
}

// msgSubject returns the first non-empty, non-comment line of the
// prepared commit message.
func msgSubject(msgFile string) (string, error) {
	b, err := os.ReadFile(msgFile)
	if err != nil {
		return "", fmt.Errorf("fix-needs-test: read %s: %w", msgFile, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" && !strings.HasPrefix(line, "#") {
			return line, nil
		}
	}
	return "", fmt.Errorf("fix-needs-test: %s has no subject", msgFile)
}

// checkRange verifies every non-merge commit the branch adds over
// origin/main. Without the upstream ref (a clone with no remote) the
// gate can see only HEAD.
func checkRange(root string) error {
	if err := exec.Command("git", "-C", root,
		"rev-parse", "--verify", "--quiet", "origin/main").Run(); err != nil {
		subject, files, err := commitInfo(root, "HEAD")
		if err != nil {
			return err
		}
		return checkCommit(subject, files)
	}
	out, err := gitOut(root, "log", "--no-merges",
		"--format=%x01%H%x01%s", "--name-only", "origin/main..HEAD")
	if err != nil {
		return err
	}
	// Layout per commit: "\x01 sha \x01 subject \n file1 \n file2 \n\n",
	// so the split yields sha, subject-with-files pairs.
	parts := strings.Split(string(out), "\x01")
	var bad []string
	for i := 1; i+1 < len(parts); i += 2 {
		lines := strings.Split(parts[i+1], "\n")
		subject := lines[0]
		var files []string
		for _, f := range lines[1:] {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		if err := checkCommit(subject, files); err != nil {
			bad = append(bad, parts[i][:12]+" "+subject)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("fix-needs-test: commits change no *_test.go: %s",
			strings.Join(bad, "; "))
	}
	return nil
}

// commitInfo reads the subject and changed files of one revision.
func commitInfo(root, rev string) (string, []string, error) {
	subj, err := gitOut(root, "show", "-s", "--format=%s", rev)
	if err != nil {
		return "", nil, err
	}
	names, err := gitOut(root, "show", "--name-only", "--format=", rev)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(subj), strings.Fields(names), nil
}

func gitOut(root string, args ...string) (string, error) {
	b, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("fix-needs-test: git %s: %w", args[0], err)
	}
	return string(b), nil
}
