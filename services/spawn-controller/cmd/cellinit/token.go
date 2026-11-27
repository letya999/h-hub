//go:build linux

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const tokenPath = "/run/hhub/generation"

func readToken(in io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 128))
	if err != nil {
		return "", fmt.Errorf("read generation token: %w", err)
	}
	return parseToken(raw)
}

func stageToken(token string) error {
	if _, err := os.Stat(tokenPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check generation file: %w", err)
	}
	pending := tokenPath + ".pending"
	if err := os.WriteFile(pending, []byte(token), 0o600); err != nil {
		return fmt.Errorf("stage generation token: %w", err)
	}
	if err := os.Rename(pending, tokenPath); err != nil {
		return fmt.Errorf("install generation token: %w", err)
	}
	return nil
}

func awaitToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(15 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := os.ReadFile(tokenPath)
		if err == nil {
			return consumeToken(raw)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read generation token: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("missing generation token: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func consumeToken(raw []byte) (string, error) {
	if err := os.Truncate(tokenPath, 0); err != nil {
		return "", fmt.Errorf("clear generation token: %w", err)
	}
	return parseToken(raw)
}

func parseToken(raw []byte) (string, error) {
	token := strings.TrimSpace(string(raw))
	if len(token) != 64 {
		return "", errors.New("invalid generation token")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("invalid generation token")
	}
	return token, nil
}
