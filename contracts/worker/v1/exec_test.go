package v1

import (
	"errors"
	"strings"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

func TestExecValidation(t *testing.T) {
	t.Parallel()
	valid := ExecReq{Argv: []string{"printf", "%s", "`literal`; literal"}, TimeoutMS: 500,
		Env: map[string]string{"COLOUR": "green"}, Workdir: "project/src"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, slot string
		change     func(*ExecReq)
	}{
		{"zero args", "argv", func(r *ExecReq) { r.Argv = nil }},
		{"empty executable", "argv", func(r *ExecReq) { r.Argv[0] = "" }},
		{"NUL in args", "argv", func(r *ExecReq) { r.Argv[1] = "\x00" }},
		{"working directory traversal", "workdir", func(r *ExecReq) { r.Workdir = "project/../space" }},
		{"absolute working directory", "workdir", func(r *ExecReq) { r.Workdir = "/workspace" }},
		{"unbounded stdin", "stdin", func(r *ExecReq) { r.Stdin = strings.Repeat("a", 32<<10+1) }},
		{"unbounded time", "timeout_ms", func(r *ExecReq) { r.TimeoutMS = 1_000_000 }},
		{"controller env", "env", func(r *ExecReq) { r.Env = map[string]string{"hhub_dsn": "literal"} }},
		{"loader env", "env", func(r *ExecReq) { r.Env = map[string]string{"LD_PRELOAD": "literal"} }},
		{"malformed env key", "env", func(r *ExecReq) { r.Env = map[string]string{"A=B": "literal"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := valid
			r.Argv = append([]string(nil), r.Argv...)
			tc.change(&r)
			var refused *herrors.Error
			if err := r.Validate(); !errors.As(err, &refused) || refused.Details["slot"] != tc.slot {
				t.Fatalf("validation = %v, want refusal for %s", err, tc.slot)
			}
		})
	}
}
