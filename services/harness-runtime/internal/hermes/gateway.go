// Package hermes controls the upstream gateway on the container loopback.
package hermes

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/letya999/h-hub/shared/proctree"
)

const gatewayURL = "http://127.0.0.1:8642"

type Gateway struct {
	key    string
	client *http.Client
	stream *http.Client
}

// Start runs the upstream executable with only the environment it needs.
// A gateway exit brings the adapter down with it.
func Start(ctx context.Context, stop context.CancelCauseFunc) (*Gateway, <-chan struct{}, error) {
	g := &Gateway{key: rand.Text(), client: &http.Client{Timeout: 5 * time.Second}, stream: &http.Client{
		Transport:     &http.Transport{ResponseHeaderTimeout: 20 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	// The pinned CLI entry point reads dependency bytecode from the immutable image.
	cmd := exec.CommandContext(ctx, "/opt/hermes/.venv/bin/python", "-P", "-m", "hermes_cli.main", "gateway", "run")
	cmd.Env = []string{
		"PATH=/opt/hermes/bin:/opt/hermes/.venv/bin:/usr/local/bin:/usr/bin:/bin",
		"HOME=/opt/data", "HERMES_HOME=/opt/data", "HERMES_ENABLE_PROJECT_PLUGINS=0",
		"HERMES_WRITE_SAFE_ROOT=/opt/data",
		"PYTHONDONTWRITEBYTECODE=1", "PYTHONUNBUFFERED=1", "XDG_RUNTIME_DIR=/tmp/hermes-runtime",
		"API_SERVER_ENABLED=true", "API_SERVER_HOST=127.0.0.1", "API_SERVER_KEY=" + g.key,
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.SysProcAttr = proctree.GroupedSysProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start Hermes: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := cmd.Wait()
		if ctx.Err() == nil {
			if err == nil {
				err = errors.New("gateway stopped")
			}
			stop(fmt.Errorf("hermes gateway exited: %w", err))
		}
	}()
	return g, done, nil
}

// Authentic checks the proof from the immutable in-process bridge. The claim
// is only a lookup handle; the hub credential stays in the admission table.
func (g *Gateway) Authentic(claim, proof string) bool {
	mac, err := hex.DecodeString(proof)
	if err != nil || len(claim) == 0 || len(claim) > 128 {
		return false
	}
	want := hmac.New(sha256.New, []byte(g.key))
	want.Write([]byte(claim))
	return hmac.Equal(mac, want.Sum(nil))
}

// Ready requires a response from the upstream gateway itself.
func (g *Gateway) Ready(ctx context.Context) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL+"/health", nil)
		if err != nil {
			return fmt.Errorf("hermes health request: %w", err)
		}
		if g.healthy(req) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("hermes health: %w", context.Cause(ctx))
		case <-tick.C:
		}
	}
}

func (g *Gateway) healthy(req *http.Request) bool {
	resp, err := g.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close() //nolint:errcheck // response has been consumed
	var health struct{ Status, Platform string }
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&health) == nil &&
		health.Platform == "hermes-agent" && health.Status == "ok"
}
