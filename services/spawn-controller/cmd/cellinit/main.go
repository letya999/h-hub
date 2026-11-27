//go:build linux

// Command cellinit is the worker's static PID 1 and token installer.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
	"github.com/letya999/h-hub/services/spawn-controller/internal/cellinit"
	"github.com/letya999/h-hub/shared/lifecycle"
	"github.com/letya999/h-hub/shared/log"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 1 {
		return bootstrap(ctx, args[0], os.Stdin)
	}
	if len(args) > 0 || os.Getpid() != 1 {
		return errors.New("worker cellinit must be PID 1")
	}
	const setDumpable = 4
	if _, _, err := syscall.Syscall6(syscall.SYS_PRCTL, setDumpable, 0, 0, 0, 0, 0); err != 0 {
		return fmt.Errorf("protect generation token: %w", err)
	}
	token, err := awaitToken(ctx)
	if err != nil {
		return err
	}
	return serve(ctx, token)
}

func serve(ctx context.Context, token string) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	handler, err := cellinit.Handler(token, cancel)
	if err != nil {
		return fmt.Errorf("worker HTTP: %w", err)
	}
	listener, err := wv1.ListenWorkerSocket(wv1.WorkerSocket)
	if err != nil {
		return fmt.Errorf("prepare worker connection socket: %w", err)
	}
	defer listener.Close() //nolint:errcheck // best-effort close
	go handoff(ctx, listener, token, cancel)
	srv := &http.Server{Addr: wv1.Loopback, Handler: handler, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: wv1.MaxDuration + 5*time.Second, IdleTimeout: 5 * time.Second}
	if err := lifecycle.Serve(ctx, srv, 2*time.Second); err != nil {
		return fmt.Errorf("serve worker: %w", err)
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return fmt.Errorf("worker stopped: %w", cause)
	}
	return nil
}

func handoff(ctx context.Context, listener *net.UnixListener, token string, cancel context.CancelCauseFunc) {
	l := log.New("cellinit", os.Stderr)
	if err := wv1.ServeWorkerSocket(ctx, listener, token, func(err error) {
		l.Error(ctx, "worker connection refused", err)
	}); err != nil {
		cancel(err)
	}
}
