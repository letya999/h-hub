package install

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/letya999/h-hub/shared/store"
)

func checkPrincipalSpacePairs(ctx context.Context, adminDSN string) error {
	idDSN, err := dsnFor(adminDSN, "identity_hub")
	if err != nil {
		return err
	}
	spDSN, err := dsnFor(adminDSN, "spawn_controller")
	if err != nil {
		return err
	}
	idDB, err := store.Connect(ctx, idDSN)
	if err != nil {
		return fmt.Errorf("identity pair DB: %w", err)
	}
	defer idDB.Close() //nolint:errcheck // read-only close
	spDB, err := store.Connect(ctx, spDSN)
	if err != nil {
		return fmt.Errorf("space pair DB: %w", err)
	}
	defer spDB.Close() //nolint:errcheck // read-only close
	principals, err := pairRows(ctx, idDB, `SELECT id,space FROM principals`)
	if err != nil {
		return err
	}
	spaces, err := pairRows(ctx, spDB, `SELECT owner,id FROM spaces WHERE name='home'`)
	if err != nil {
		return err
	}
	return comparePairs(principals, spaces)
}

func comparePairs(principals, spaces map[string]string) error {
	for owner, space := range spaces {
		if principals[owner] != space {
			return fmt.Errorf("home space %s/%s has no matching principal", owner, space)
		}
	}
	for owner, space := range principals {
		if spaces[owner] != space {
			return fmt.Errorf("principal %s/%s has no matching home space", owner, space)
		}
	}
	return nil
}

func pairRows(ctx context.Context, db *sql.DB, query string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("pair query: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only close
	out := map[string]string{}
	for rows.Next() {
		var owner, space string
		if err := rows.Scan(&owner, &space); err != nil {
			return nil, fmt.Errorf("pair row: %w", err)
		}
		out[owner] = space
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pair rows: %w", err)
	}
	return out, nil
}
