package svc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	herrors "github.com/letya999/h-hub/shared/errors"
)

// Noncer enforces single-use issuance nonces. The Postgres
// implementation is the durable one; tests may substitute memory.
type Noncer interface {
	Claim(ctx context.Context, sub, nonce string, exp time.Time) error
}

// SQLNoncer stores claimed nonces; a duplicate is a replay refusal.
type SQLNoncer struct{ DB *sql.DB }

// Claim inserts (sub, nonce); a conflict means replay.
func (s SQLNoncer) Claim(ctx context.Context, sub, nonce string, exp time.Time) error {
	if _, err := s.DB.ExecContext(ctx,
		"DELETE FROM nonces WHERE exp < now()"); err != nil {
		return fmt.Errorf("nonce gc: %w", err)
	}
	_, err := s.DB.ExecContext(ctx,
		"INSERT INTO nonces (sub, nonce, exp) VALUES ($1,$2,$3)", sub, nonce, exp)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return herrors.New(herrors.CodeForbidden, "err.forbidden").With("nonce", "replay") //nolint:wrapcheck // shared error form
		}
		return fmt.Errorf("nonce claim: %w", err)
	}
	return nil
}
