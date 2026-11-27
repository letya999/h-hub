package svc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	sv1 "github.com/letya999/h-hub/contracts/spawn/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
)

type skillPin struct {
	inventory sv1.SkillInventoryResp
	revision  string
}

func pinnedSkills(s *Server, ctx context.Context, c idv1.RunClaims, digest string) (skillPin, error) {
	var pin skillPin
	task, err := taskClient(s, ctx)
	if err != nil {
		return pin, err
	}
	session, err := task.GetRunSession(ctx, tv1.RunSessionReq{Run: c.Run, Job: c.Job, Owner: c.Owner, Space: c.Space})
	if err != nil {
		return pin, fmt.Errorf("resolve skill session: %w", err)
	}
	if session.Key == "" {
		return pin, fmt.Errorf("skill session has no bound conversation")
	}
	pin, found, err := readSkillPin(ctx, s.db, session.Key)
	if err != nil || found {
		return pin, err
	}
	return captureSkillPin(s, ctx, c, digest, session.Key)
}

func captureSkillPin(s *Server, ctx context.Context, c idv1.RunClaims, digest, session string) (skillPin, error) {
	var pin skillPin
	spawn, err := s.spawnClient(ctx)
	if err != nil {
		return pin, err
	}
	inventory, err := spawn.SkillInventory(ctx, sv1.SkillInventoryReq{Owner: c.Owner, Space: c.Space, Digest: digest})
	if err != nil {
		return pin, fmt.Errorf("capture skill inventory: %w", err)
	}
	if inventory.Entries == nil {
		return pin, fmt.Errorf("skill inventory did not include entries")
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		return pin, fmt.Errorf("encode skill inventory: %w", err)
	}
	hash := sha256.Sum256(raw)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO skill_session_catalogs(session_key, revision, payload)
		VALUES ($1, $2, $3) ON CONFLICT (session_key) DO NOTHING`, session, hex.EncodeToString(hash[:]), raw); err != nil {
		return pin, fmt.Errorf("pin skill inventory: %w", err)
	}
	pin, found, err := readSkillPin(ctx, s.db, session)
	if err == nil && !found {
		err = fmt.Errorf("skill catalog pin was not recorded")
	}
	return pin, err
}

func readSkillPin(ctx context.Context, db *sql.DB, key string) (skillPin, bool, error) {
	var pin skillPin
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT revision, payload FROM skill_session_catalogs WHERE session_key=$1`, key).
		Scan(&pin.revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pin, false, nil
	}
	if err != nil {
		return pin, false, fmt.Errorf("read skill catalog pin: %w", err)
	}
	if err := json.Unmarshal(raw, &pin.inventory); err != nil {
		return pin, false, fmt.Errorf("decode skill catalog pin: %w", err)
	}
	return pin, true, nil
}
