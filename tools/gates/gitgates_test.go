package gates

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// commitMsgFile is the prepared commit message git handed to
// .githooks/commit-msg, forwarded by `just check-commit`. Empty means a
// normal test run, where the branch range is verified instead.
var commitMsgFile = flag.String("commit-msg", "",
	"prepared commit message file (set by .githooks/commit-msg)")

// TestFixNeedsTestCommit is the commit-msg hook's entry point
// (just check-commit): with -commit-msg it checks the commit being
// created; without it, the branch range, like TestRepo.
func TestFixNeedsTestCommit(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if *commitMsgFile == "" {
		if err := FixNeedsTest(root); err != nil {
			t.Error(err)
		}
		return
	}
	if err := checkStaged(root, *commitMsgFile); err != nil {
		t.Error(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git",
		append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitRepo builds a scratch repo, pins refs/remotes/origin/main on the
// seed commit, and returns it with a helper committing the given files
// under one subject.
func gitRepo(t *testing.T) (dir string, commit func(subject string, files ...string)) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	commit = func(subject string, files ...string) {
		t.Helper()
		for _, f := range files {
			writeFile(t, dir, f)
			gitRun(t, dir, "add", f)
		}
		gitRun(t, dir, "-c", "user.email=t@t", "-c", "user.name=t",
			"commit", "-qm", subject)
	}
	commit("chore: seed", "seed.txt")
	gitRun(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	return dir, commit
}

func TestFixNeedsTestRange(t *testing.T) {
	t.Parallel()
	t.Run("rejects a testless fix in the branch range", func(t *testing.T) {
		t.Parallel()
		dir, commit := gitRepo(t)
		commit("feat(x): add code", "x.go")
		commit("fix(x): repair", "y.go")
		if err := FixNeedsTest(dir); err == nil {
			t.Error("range containing a fix without a test passed")
		}
	})
	t.Run("accepts a fix carrying a test", func(t *testing.T) {
		t.Parallel()
		dir, commit := gitRepo(t)
		commit("fix(x): repair", "x.go", "x_test.go")
		if err := FixNeedsTest(dir); err != nil {
			t.Error(err)
		}
	})
	t.Run("parses subjects and file lists apart", func(t *testing.T) {
		t.Parallel()
		dir, commit := gitRepo(t)
		commit("fix(x): repair", "x.go", "x_test.go")
		commit("docs(y): note", "z.go")
		if err := FixNeedsTest(dir); err != nil {
			t.Error(err)
		}
	})
	t.Run("skips merge commits", func(t *testing.T) {
		t.Parallel()
		dir, commit := gitRepo(t)
		commit("feat(x): base", "x.go")
		gitRun(t, dir, "checkout", "-q", "-b", "side")
		commit("feat(y): side work", "y.go")
		gitRun(t, dir, "checkout", "-q", "main")
		gitRun(t, dir, "-c", "user.email=t@t", "-c", "user.name=t",
			"merge", "--no-ff", "-m", "fix: merge side", "side")
		if err := FixNeedsTest(dir); err != nil {
			t.Error(err)
		}
	})
}

func TestFixNeedsTestStagedBad(t *testing.T) {
	t.Parallel()
	dir, _ := gitRepo(t)
	writeFile(t, dir, "x.go")
	gitRun(t, dir, "add", "x.go")
	msg := filepath.Join(dir, "MSG")
	if err := os.WriteFile(msg, []byte("fix(x): repair\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStaged(dir, msg); err == nil {
		t.Error("staged fix without a test passed")
	}
}

func TestFixNeedsTestStagedGood(t *testing.T) {
	t.Parallel()
	dir, _ := gitRepo(t)
	writeFile(t, dir, "x.go")
	writeFile(t, dir, "x_test.go")
	gitRun(t, dir, "add", "x.go", "x_test.go")
	msg := filepath.Join(dir, "MSG")
	if err := os.WriteFile(msg, []byte("fix(x): repair\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStaged(dir, msg); err != nil {
		t.Error(err)
	}
}

func TestFixNeedsTestStagedNonFix(t *testing.T) {
	t.Parallel()
	dir, _ := gitRepo(t)
	writeFile(t, dir, "x.go")
	gitRun(t, dir, "add", "x.go")
	msg := filepath.Join(dir, "MSG")
	if err := os.WriteFile(msg,
		[]byte("docs(x): note\n\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStaged(dir, msg); err != nil {
		t.Error(err)
	}
}
