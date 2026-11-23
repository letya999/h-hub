//go:build libsqlite3

package svc

import (
	"database/sql"
	"fmt"
)

// The linked libsqlite3 is SQLCipher with the openssl codec provider;
// the static archives resolve each other's symbols inside a linker
// group so flag order never decides the link.

// openDB opens the SQLCipher database. The binding is the project's
// own minimal driver in sqlitedrv.go: the database key is applied
// with sqlite3_key at connection create, before any statement can
// touch the file — mattn/go-sqlite3 cannot express that ordering and
// fails to reopen an encrypted file (see sqlitedrv.go).
// cipher_version is verified against the live engine, so a
// plain-sqlite build fails closed instead of silently writing an
// unencrypted file.
func openDB(path, hexKey string) (*sql.DB, error) {
	db := sql.OpenDB(&sqliteConnector{path: path, drv: sqliteDriver{hexKey: hexKey}})
	db.SetMaxOpenConns(1) // single writer: sqlite serializes anyway
	return db, nil
}

// cipherProof verifies the linked engine really is SQLCipher. A
// mismatched or plain build makes every later operation unsafe, so
// startup refuses before any schema work.
func cipherProof(db *sql.DB) error {
	var v string
	if err := db.QueryRow(`PRAGMA cipher_version`).Scan(&v); err != nil {
		return fmt.Errorf("cipher_version: %w (engine is not SQLCipher)", err)
	} else if v == "" {
		return fmt.Errorf("cipher_version: empty (engine is not SQLCipher)")
	}
	return nil
}
