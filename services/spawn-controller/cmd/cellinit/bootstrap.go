//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	wv1 "github.com/letya999/h-hub/contracts/worker/v1"
)

func bootstrap(ctx context.Context, operation string, in io.Reader) error {
	token, err := readToken(in)
	if err != nil {
		return err
	}
	switch operation {
	case "install-token":
		return stageToken(token)
	case "probe":
		return probe(ctx, token)
	}
	return errors.New("unknown cellinit operation")
}

func probe(ctx context.Context, token string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	client := wv1.NewHTTPClient(wv1.WorkerSocket, token, 200*time.Millisecond)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := client.Do(ctx, http.MethodGet, wv1.HealthPath, nil, nil)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("cellinit readiness: %w", err)
		case <-ticker.C:
		}
	}
}
