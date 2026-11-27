// Package v1 is the direct HTTP contract for an isolated cell.
package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

const (
	ExecPath    = "/exec"
	HealthPath  = "/healthz"
	Loopback    = "127.0.0.1:9000"
	MaxBody     = 128 << 10
	MaxOutput   = 32 << 10
	MaxDuration = 60 * time.Second
)

type ExecReq struct {
	Argv      []string          `json:"argv"`
	Workdir   string            `json:"workdir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Stdin     string            `json:"stdin,omitempty"`
	TimeoutMS int               `json:"timeout_ms"`
}

type ExecResp struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
	TimedOut  bool   `json:"timed_out"`
}

// Decode reads exactly one command with declared slots.
func Decode(in io.Reader) (ExecReq, error) {
	var r ExecReq
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, herrors.Wrap(herrors.CodeInvalid, "err.decode", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return r, herrors.New(herrors.CodeInvalid, "err.decode")
	}
	if err := r.Validate(); err != nil {
		return r, fmt.Errorf("worker arguments: %w", err)
	}
	return r, nil
}

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func (r ExecReq) Validate() error {
	if !validArgs(r.Argv) {
		return invalidSlot("argv")
	}
	if r.TimeoutMS <= 0 || r.TimeoutMS > int(MaxDuration/time.Millisecond) {
		return invalidSlot("timeout_ms")
	}
	if len(r.Stdin) > 32<<10 {
		return invalidSlot("stdin")
	}
	if !validWorkdir(r.Workdir) {
		return invalidSlot("workdir")
	}
	if !validEnv(r.Env) {
		return invalidSlot("env")
	}
	return nil
}

func invalidSlot(slot string) error {
	return herrors.New(herrors.CodeInvalid, "err.invalid-arg").With("slot", slot) //nolint:wrapcheck // shared error form
}

func validArgs(args []string) bool {
	if len(args) == 0 || len(args) > 64 || args[0] == "" {
		return false
	}
	size := 0
	for _, a := range args {
		size += len(a)
		if size > 16<<10 || strings.ContainsRune(a, 0) {
			return false
		}
	}
	return true
}

func validWorkdir(dir string) bool {
	if len(dir) > 512 || strings.ContainsAny(dir, "\\\x00") || path.IsAbs(dir) {
		return false
	}
	for _, name := range strings.Split(dir, "/") {
		if name == ".." {
			return false
		}
	}
	return true
}

func validEnv(env map[string]string) bool {
	if len(env) > 16 {
		return false
	}
	for key, value := range env {
		if !varName.MatchString(key) || reservedEnv(key) || len(value) > 1024 || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func reservedEnv(key string) bool {
	key = strings.ToUpper(key)
	switch key {
	case "PATH", "HOME", "TMPDIR", "ENV", "BASH_ENV", "SSH_AUTH_SOCK", "KUBECONFIG", "DATABASE_URL":
		return true
	}
	for _, prefix := range []string{"HHUB_", "DOCKER_", "POSTGRES_", "PG", "LD_", "DYLD_", "NODE_", "PYTHON"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
