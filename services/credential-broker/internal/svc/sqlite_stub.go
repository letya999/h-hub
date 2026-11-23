//go:build !libsqlite3

package svc

import (
	"database/sql"
	"fmt"
)

// openDB refuses: without the libsqlite3 build tag the binary carries
// no SQLCipher engine and must not run (fail closed, brief).
func openDB(_, _ string) (*sql.DB, error) {
	return nil, fmt.Errorf("credential-broker requires the libsqlite3 build tag")
}

func cipherProof(_ *sql.DB) error {
	return fmt.Errorf("credential-broker requires the libsqlite3 build tag")
}
