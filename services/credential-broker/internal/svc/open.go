package svc

import (
	"context"
	"database/sql"
)

// Open brings the database from a file on disk to a verified,
// activated handle: apply the key, prove the engine is SQLCipher,
// then run the activation/migration path that fails closed on a
// missing marker.
func Open(ctx context.Context, path, hexKey string) (*sql.DB, error) {
	db, err := openDB(path, hexKey)
	if err != nil {
		return nil, err
	}
	if err := cipherProof(db); err != nil {
		db.Close() //nolint:errcheck // best-effort close
		return nil, err
	}
	if err := activate(ctx, db); err != nil {
		db.Close() //nolint:errcheck // best-effort close
		return nil, err
	}
	return db, nil
}
