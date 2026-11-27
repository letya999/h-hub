package backend

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// InstallWorkerToken writes the token over Docker exec stdin to the worker's tmpfs.
func (d *Docker) InstallWorkerToken(ctx context.Context, ref WorkerRef, token string) error {
	return d.workerExecInput(ctx, ref, "install-token", token)
}

// ProbeWorker checks cellinit readiness inside its own network namespace.
func (d *Docker) ProbeWorker(ctx context.Context, ref WorkerRef, token string) error {
	return d.workerExecInput(ctx, ref, "probe", token)
}

func (d *Docker) workerExecInput(ctx context.Context, ref WorkerRef, operation, token string) error {
	c, err := d.workerInspect(ctx, ref)
	if err != nil {
		return err
	}
	if !c.State.Running {
		return errors.New("worker is not running")
	}
	var exec struct{ ID string }
	args := map[string]any{
		"Cmd": []string{"/cellinit", operation}, "AttachStdin": true, "AttachStdout": true, "AttachStderr": true,
	}
	if err := d.req(ctx, http.MethodPost, "/containers/"+c.ID+"/exec", args, &exec); err != nil {
		return fmt.Errorf("prepare worker token delivery: %w", err)
	}
	if exec.ID == "" {
		return errors.New("worker token delivery has no exec ID")
	}
	if err := d.writeExec(ctx, url.PathEscape(exec.ID), token); err != nil {
		return err
	}
	var status struct {
		Running  bool
		ExitCode int
	}
	if err := d.req(ctx, http.MethodGet, "/exec/"+url.PathEscape(exec.ID)+"/json", nil, &status); err != nil {
		return fmt.Errorf("observe worker token delivery: %w", err)
	}
	if status.Running || status.ExitCode != 0 {
		return fmt.Errorf("worker token delivery exit code %d, running=%t", status.ExitCode, status.Running)
	}
	return nil
}

func (d *Docker) writeExec(ctx context.Context, id, data string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", d.sock)
	if err != nil {
		return fmt.Errorf("connect Docker exec: %w", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort close
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("bound Docker exec: %w", err)
	}
	stream, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("Docker exec expects a Unix stream")
	}
	return writeExecStream(ctx, stream, id, data)
}

func writeExecStream(ctx context.Context, stream *net.UnixConn, id, data string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/exec/"+id+"/start",
		strings.NewReader(`{"Detach":false,"Tty":false}`))
	if err != nil {
		return fmt.Errorf("open Docker exec stream: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	if err := req.Write(stream); err != nil {
		return fmt.Errorf("start Docker exec stream: %w", err)
	}
	reader := bufio.NewReader(stream)
	res, err := http.ReadResponse(reader, req)
	if err != nil {
		return fmt.Errorf("read Docker exec handshake: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	if res.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("Docker exec handshake status %d", res.StatusCode)
	}
	if _, err := io.WriteString(stream, data); err != nil {
		return fmt.Errorf("write worker token: %w", err)
	}
	if err := stream.CloseWrite(); err != nil {
		return fmt.Errorf("close Docker exec input: %w", err)
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(reader, 4096)); err != nil {
		return fmt.Errorf("complete worker token delivery: %w", err)
	}
	return nil
}
