// Package meta is the meta-test (enforcement.md 3): every linter from
// the spec is enabled and every threshold is set, so a check cannot be
// silently weakened.
package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type config struct {
	Linters struct {
		Enable   []string       `yaml:"enable"`
		Settings map[string]any `yaml:"settings"`
	} `yaml:"linters"`
	Formatters struct {
		Enable []string `yaml:"enable"`
	} `yaml:"formatters"`
}

func load(t *testing.T) config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var c config
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

var requiredLinters = []string{
	"funlen", "gocognit", "cyclop", "nestif", "maintidx", "dupl",
	"interfacebloat", "revive", "errcheck", "errorlint", "nilerr",
	"wrapcheck", "forbidigo", "depguard", "paralleltest", "tparallel",
	"nolintlint",
}

func TestLintersEnabled(t *testing.T) {
	t.Parallel()
	c := load(t)
	en := map[string]bool{}
	for _, l := range append(c.Linters.Enable, c.Formatters.Enable...) {
		en[l] = true
	}
	for _, l := range append(requiredLinters, "gofmt") {
		if !en[l] {
			t.Errorf("linter %s is not enabled", l)
		}
	}
}

// get walks a path of map keys in settings.
func get(s map[string]any, path ...string) any {
	var cur any = s
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func TestThresholds(t *testing.T) {
	t.Parallel()
	s := load(t).Linters.Settings
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"funlen.lines", get(s, "funlen", "lines"), 60},
		{"funlen.statements", get(s, "funlen", "statements"), 40},
		{"funlen.ignore-comments", get(s, "funlen", "ignore-comments"), true},
		{"gocognit.min-complexity", get(s, "gocognit", "min-complexity"), 15},
		{"cyclop.max-complexity", get(s, "cyclop", "max-complexity"), 12},
		{"cyclop.package-average", get(s, "cyclop", "package-average"), 5},
		{"nestif.min-complexity", get(s, "nestif", "min-complexity"), 4},
		{"maintidx.under", get(s, "maintidx", "under"), 20},
		{"dupl.threshold", get(s, "dupl", "threshold"), 100},
		{"interfacebloat.max", get(s, "interfacebloat", "max"), 5},
		{"errcheck.check-blank", get(s, "errcheck", "check-blank"), true},
		{"errcheck.check-type-assertions", get(s, "errcheck", "check-type-assertions"), true},
		{"forbidigo.analyze-types", get(s, "forbidigo", "analyze-types"), true},
		{"nolintlint.require-explanation", get(s, "nolintlint", "require-explanation"), true},
		{"nolintlint.require-specific", get(s, "nolintlint", "require-specific"), true},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// reviveArgs returns rule name -> arguments list.
func reviveArgs(t *testing.T) map[string][]any {
	t.Helper()
	s := load(t).Linters.Settings
	rules, ok := get(s, "revive", "rules").([]any)
	if !ok {
		t.Fatal("revive.rules missing")
	}
	args := map[string][]any{}
	for _, r := range rules {
		m, ok := r.(map[string]any)
		name, nok := m["name"].(string)
		if !ok || !nok {
			t.Fatal("revive rule has no name")
		}
		if a, ok := m["arguments"].([]any); ok {
			args[name] = a
		} else {
			args[name] = nil
		}
	}
	return args
}

func TestReviveRules(t *testing.T) {
	t.Parallel()
	args := reviveArgs(t)
	for _, want := range []string{
		"argument-limit", "function-result-limit", "file-length-limit", "max-control-nesting",
	} {
		if _, ok := args[want]; !ok {
			t.Errorf("revive rule %s not configured", want)
		}
	}
	checkArg(t, args, "argument-limit", 5)
	checkArg(t, args, "function-result-limit", 3)
	checkArg(t, args, "max-control-nesting", 4)
	fl, ok := args["file-length-limit"][0].(map[string]any)
	if !ok || fl["max"] != 500 || fl["skip-comments"] != true || fl["skip-blank-lines"] != true {
		t.Error("revive file-length-limit must be max 500 skipping comments and blanks")
	}
}

func checkArg(t *testing.T, args map[string][]any, rule string, want int) {
	t.Helper()
	a := args[rule]
	if len(a) == 0 || a[0] != want {
		t.Errorf("revive %s = %v, want %d", rule, a, want)
	}
}

func TestForbidigoPatterns(t *testing.T) {
	t.Parallel()
	s := load(t).Linters.Settings
	fb, ok := get(s, "forbidigo", "forbid").([]any)
	if !ok {
		t.Fatal("forbidigo.forbid missing")
	}
	var pats []string
	for _, p := range fb {
		m, mok := p.(map[string]any)
		if pat, ok := m["pattern"].(string); mok && ok {
			pats = append(pats, pat)
		}
	}
	joined := fmt.Sprintf("%v", pats)
	for _, want := range []string{
		`os\.(Getenv|LookupEnv)`, `exec\.Command`, `DefaultClient`,
		`time\.Sleep`, `t\.Skip`, `panic`, `fmt\.Print`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("forbidigo pattern for %q missing (have %v)", want, pats)
		}
	}
}

func TestDepguardRules(t *testing.T) {
	t.Parallel()
	s := load(t).Linters.Settings
	rules, ok := get(s, "depguard", "rules").(map[string]any)
	if !ok {
		t.Fatal("depguard.rules missing")
	}
	for _, want := range []string{
		"no-v0", "shared", "contracts", "hubctl", "hubctl-install", "services", "tools",
	} {
		if _, ok := rules[want]; !ok {
			t.Errorf("depguard rule %s missing", want)
		}
	}
	svc := 0
	for name := range rules {
		if strings.HasPrefix(name, "svc-") {
			svc++
		}
	}
	if svc == 0 {
		t.Error("depguard per-service rules missing")
	}
}
