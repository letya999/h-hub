package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PingHealth GETs http://127.0.0.1:<port>/healthz once; it backs the
// `healthcheck` subcommand every service binary exposes to the Docker
// HEALTHCHECK instruction.
func PingHealth(ctx context.Context, addr string) error {
	hc := &http.Client{Timeout: 3 * time.Second}
	port := addr
	if len(port) > 0 && port[0] == ':' {
		port = port[1:]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("healthz request: %w", err)
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("healthz: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, MaxErrorBody)); err != nil {
		return fmt.Errorf("healthz drain: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: status %d", res.StatusCode)
	}
	return nil
}
