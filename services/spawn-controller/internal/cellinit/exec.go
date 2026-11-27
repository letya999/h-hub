//go:build linux

// Package cellinit runs one bounded command in the worker's PID namespace.
package cellinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
)

const (
	workspace = "/workspace"
	grace     = 250 * time.Millisecond
)

// ErrUnclean tells the caller to shut down the isolated cell.
var ErrUnclean = errors.New("worker process tree did not exit")

// Run executes one command without inheriting cellinit's environment.
// The caller serializes calls and shuts down the cell on ErrUnclean.
func Run(ctx context.Context, req wv1.ExecReq) (wv1.ExecResp, error) {
	if os.Getpid() != 1 {
		return wv1.ExecResp{}, errors.New("cellinit must be PID 1")
	}
	if err := req.Validate(); err != nil {
		return wv1.ExecResp{}, fmt.Errorf("validate worker command: %w", err)
	}
	dir, err := openWorkdir(req.Workdir)
	if err != nil {
		return wv1.ExecResp{}, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err).With("slot", "workdir") //nolint:wrapcheck // shared error form
	}
	defer dir.Close() //nolint:errcheck // best-effort close
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	var stdout, stderr stream
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir = fmt.Sprintf("/proc/self/fd/%d", dir.Fd())
	cmd.Env = environment(req.Env)
	cmd.Stdin = strings.NewReader(req.Stdin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Cancel, cmd.WaitDelay = killChildren, grace
	if err := cmd.Start(); err != nil {
		return wv1.ExecResp{}, herrors.Wrap(herrors.CodeInvalid, "err.invalid-arg", err).With("slot", "argv") //nolint:wrapcheck // shared error form
	}
	waitErr := waitCommand(ctx, cmd)
	if errors.Is(waitErr, ErrUnclean) {
		return wv1.ExecResp{}, waitErr
	}
	if err := reapChildren(); err != nil {
		return wv1.ExecResp{}, fmt.Errorf("%w: %w", ErrUnclean, err)
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, exec.ErrWaitDelay) {
		return wv1.ExecResp{}, fmt.Errorf("wait worker command: %w", waitErr)
	}
	return wv1.ExecResp{
		ExitCode: cmd.ProcessState.ExitCode(), Stdout: string(stdout.buf), Stderr: string(stderr.buf),
		Truncated: stdout.truncated || stderr.truncated, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
	}, nil
}

func openWorkdir(rel string) (*os.File, error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	defer root.Close() //nolint:errcheck // best-effort close
	dir, err := root.Open(path.Clean(rel))
	if err != nil {
		return nil, fmt.Errorf("open working directory: %w", err)
	}
	if fi, err := dir.Stat(); err != nil || !fi.IsDir() {
		return nil, errors.Join(err, dir.Close(), errors.New("worker directory required"))
	}
	return dir, nil
}

func environment(extra map[string]string) []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/tmp", "TMPDIR=/tmp", "LANG=C.UTF-8"}
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	slices.Sort(env)
	return env
}

func waitCommand(ctx context.Context, cmd *exec.Cmd) error {
	result := make(chan error, 1)
	go func() { result <- cmd.Wait() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
	}
	timer := time.NewTimer(3 * grace)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return ErrUnclean
	}
}

func killChildren() error {
	if err := syscall.Kill(-1, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill worker children: %w", err)
	}
	return nil
}

func reapChildren() error {
	if err := killChildren(); err != nil {
		return err
	}
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		pid, err := syscall.Wait4(-1, nil, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.ECHILD) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reap worker children: %w", err)
		}
		if pid > 0 {
			continue
		}
		select {
		case <-deadline.C:
			return ErrUnclean
		case <-tick.C:
		}
	}
}
