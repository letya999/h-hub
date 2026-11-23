package svc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	"github.com/letya999/h-hub/shared/seal"
)

type issuerBootstrap struct {
	Kid    string `json:"kid"`
	Pub    []byte `json:"pub"`
	Sealed []byte `json:"sealed"`
}

// LoadIssuer imports a sealed bootstrap once, then reads only identity's database.
func LoadIssuer(ctx context.Context, db *sql.DB, sealer *seal.Seal, path string,
	want ed25519.PublicKey) (ed25519.PrivateKey, error) {
	row, err := issuerRow(ctx, db, sealer, path, want)
	if err != nil {
		return nil, err
	}
	key, err := openIssuer(sealer, row, want)
	if err != nil {
		return nil, err
	}
	if err := consumeIssuerBootstrap(path, sealer, want, key); err != nil {
		return nil, err
	}
	return key, nil
}

func issuerRow(ctx context.Context, db *sql.DB, sealer *seal.Seal, path string,
	want ed25519.PublicKey) (issuerBootstrap, error) {
	var row issuerBootstrap
	err := db.QueryRowContext(ctx, `SELECT kid,pub,sealed FROM signing_keys WHERE purpose='signing-key'`).
		Scan(&row.Kid, &row.Pub, &row.Sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return importIssuerRow(ctx, db, sealer, path, want)
	}
	if err != nil {
		return row, fmt.Errorf("load issuer: %w", err)
	}
	return row, nil
}

func importIssuerRow(ctx context.Context, db *sql.DB, sealer *seal.Seal, path string,
	want ed25519.PublicKey) (issuerBootstrap, error) {
	row, err := readIssuerBootstrap(path)
	if err != nil {
		return row, err
	}
	if _, err := openIssuer(sealer, row, want); err != nil {
		return row, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO signing_keys(purpose,kid,pub,sealed)
		VALUES('signing-key',$1,$2,$3)`, row.Kid, row.Pub, row.Sealed)
	if err != nil {
		return row, fmt.Errorf("import issuer: %w", err)
	}
	return row, nil
}

func consumeIssuerBootstrap(path string, sealer *seal.Seal, want ed25519.PublicKey, key ed25519.PrivateKey) error {
	boot, err := readIssuerBootstrap(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	old, err := openIssuer(sealer, boot, want)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, key) {
		return fmt.Errorf("issuer bootstrap differs from database")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove issuer bootstrap: %w", err)
	}
	return nil
}

func readIssuerBootstrap(path string) (issuerBootstrap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return issuerBootstrap{}, fmt.Errorf("read issuer bootstrap: %w", err)
	}
	var b issuerBootstrap
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("decode issuer bootstrap: %w", err)
	}
	return b, nil
}

func openIssuer(sealer *seal.Seal, row issuerBootstrap, want ed25519.PublicKey) (ed25519.PrivateKey, error) {
	if len(row.Pub) != ed25519.PublicKeySize || !bytes.Equal(row.Pub, want) || row.Kid != idv1.Kid(want) {
		return nil, fmt.Errorf("issuer public key differs")
	}
	plain, err := sealer.Open("signing-key", row.Kid, row.Sealed)
	if err != nil {
		return nil, fmt.Errorf("open issuer: %w", err)
	}
	if len(plain) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("issuer private key length invalid")
	}
	key := ed25519.PrivateKey(plain)
	if !bytes.Equal(key[ed25519.SeedSize:], want) {
		return nil, fmt.Errorf("issuer key pair differs")
	}
	return key, nil
}
