package svc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
)

const linkTTL = 24 * time.Hour

func (s *Server) linkBegin(ctx context.Context, r idv1.LinkBeginReq) (idv1.LinkBeginResp, error) {
	if r.Owner == "" {
		return idv1.LinkBeginResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	if err := checkOperator(ctx, r.Owner, "", "LinkBegin"); err != nil {
		return idv1.LinkBeginResp{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("link begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := activePrincipal(ctx, tx, r.Owner); err != nil {
		return idv1.LinkBeginResp{}, err
	}
	out, err := s.pendingCode(ctx, tx, r.Owner)
	if err != nil {
		return out, err
	}
	if out.Code == "" {
		out, err = s.newCode(ctx, tx, r.Owner)
	}
	if err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("link commit: %w", err)
	}
	return out, nil
}

func activePrincipal(ctx context.Context, tx *sql.Tx, owner string) error {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT m.state FROM fsm.machines m WHERE m.kind='principal' AND m.id=$1`, owner).Scan(&state); err != nil {
		return fmt.Errorf("principal state: %w", err)
	}
	if state != "active" {
		return herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return nil
}

func (s *Server) pendingCode(ctx context.Context, tx *sql.Tx, owner string) (idv1.LinkBeginResp, error) {
	var hash, sealed []byte
	var state string
	var exp time.Time
	err := tx.QueryRowContext(ctx, `SELECT c.hash,c.sealed,c.expires_at,m.state FROM link_codes c
		JOIN fsm.machines m ON m.kind='link_code' AND m.id=encode(c.hash,'hex')
		WHERE c.owner=$1 FOR UPDATE OF c,m`, owner).Scan(&hash, &sealed, &exp, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return idv1.LinkBeginResp{}, nil
	}
	if err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("pending code: %w", err)
	}
	if state != "pending" || exp.Unix() <= s.now() {
		return idv1.LinkBeginResp{}, expirePendingCode(ctx, tx, hash, state)
	}
	if s.seal == nil {
		return idv1.LinkBeginResp{}, herrors.New(herrors.CodeUnavailable, "err.seal")
	}
	plain, err := s.seal.Open("link-code", hex.EncodeToString(hash), sealed)
	if err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("open link code: %w", err)
	}
	return idv1.LinkBeginResp{Code: string(plain), ExpiresAt: exp.Unix()}, nil
}

func expirePendingCode(ctx context.Context, tx *sql.Tx, hash []byte, state string) error {
	if state == "pending" {
		if err := linkMachine().Transition(ctx, tx, fsm.Request{ID: hex.EncodeToString(hash), Event: "expire", Actor: "identity-hub"}, nil); err != nil {
			return fmt.Errorf("expire code: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM link_codes WHERE hash=$1`, hash); err != nil {
		return fmt.Errorf("delete expired code: %w", err)
	}
	return nil
}

func (s *Server) newCode(ctx context.Context, tx *sql.Tx, owner string) (idv1.LinkBeginResp, error) {
	if s.seal == nil {
		return idv1.LinkBeginResp{}, herrors.New(herrors.CodeUnavailable, "err.seal")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("link random: %w", err)
	}
	code := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(code))
	id := hex.EncodeToString(hash[:])
	sealed, err := s.seal.Seal("link-code", id, []byte(code))
	if err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("seal link code: %w", err)
	}
	exp := time.Unix(s.now(), 0).Add(linkTTL)
	if _, err := tx.ExecContext(ctx, `INSERT INTO link_codes(hash,owner,sealed,expires_at) VALUES($1,$2,$3,$4)`, hash[:], owner, sealed, exp); err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("insert link code: %w", err)
	}
	if err := linkMachine().Create(ctx, tx, nowClock{s.now}, fsm.CreateReq{ID: id, Initial: "pending", Actor: "operator"}); err != nil {
		return idv1.LinkBeginResp{}, fmt.Errorf("create link code: %w", err)
	}
	return idv1.LinkBeginResp{Code: code, ExpiresAt: exp.Unix()}, nil
}

func (s *Server) linkConfirm(ctx context.Context, r idv1.LinkConfirmReq) (idv1.LinkConfirmResp, error) {
	if r.Channel == "" || r.External == "" || r.Code == "" {
		return idv1.LinkConfirmResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	hash := sha256.Sum256([]byte(r.Code))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return idv1.LinkConfirmResp{}, fmt.Errorf("confirm begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	owner, space, err := s.confirmCode(ctx, tx, hash[:])
	if err != nil {
		return idv1.LinkConfirmResp{}, err
	}
	if err := linkMachine().Transition(ctx, tx, fsm.Request{ID: hex.EncodeToString(hash[:]), Event: "use", Actor: "channel-hub"}, nil); err != nil {
		return idv1.LinkConfirmResp{}, fmt.Errorf("use link code: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO channel_links(channel,external_id,owner) VALUES($1,$2,$3)`, r.Channel, r.External, owner); err != nil {
		return idv1.LinkConfirmResp{}, fmt.Errorf("channel link: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM link_codes WHERE hash=$1`, hash[:]); err != nil {
		return idv1.LinkConfirmResp{}, fmt.Errorf("delete code: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return idv1.LinkConfirmResp{}, fmt.Errorf("confirm commit: %w", err)
	}
	return idv1.LinkConfirmResp{Owner: owner, Space: space}, nil
}

func (s *Server) confirmCode(ctx context.Context, tx *sql.Tx, hash []byte) (string, string, error) {
	var owner, space, state string
	var exp time.Time
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.space,m.state,c.expires_at FROM link_codes c
		JOIN principals p ON p.id=c.owner JOIN fsm.machines m ON m.kind='principal' AND m.id=p.id
		WHERE c.hash=$1 FOR UPDATE OF c`, hash).Scan(&owner, &space, &state, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	if err != nil {
		return "", "", fmt.Errorf("confirm lookup: %w", err)
	}
	if state != "active" || exp.Unix() <= s.now() {
		return "", "", herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	return owner, space, nil
}

func (s *Server) resolve(ctx context.Context, r idv1.ResolveReq) (idv1.ResolveResp, error) {
	if r.Channel == "" || r.External == "" {
		return idv1.ResolveResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
	}
	var owner, space string
	err := s.db.QueryRowContext(ctx, `SELECT p.id,p.space FROM channel_links l JOIN principals p ON p.id=l.owner
		JOIN fsm.machines m ON m.kind='principal' AND m.id=p.id AND m.state='active'
		WHERE l.channel=$1 AND l.external_id=$2`, r.Channel, r.External).Scan(&owner, &space)
	if errors.Is(err, sql.ErrNoRows) {
		return idv1.ResolveResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
	}
	if err != nil {
		return idv1.ResolveResp{}, fmt.Errorf("resolve: %w", err)
	}
	return idv1.ResolveResp{Owner: owner, Space: space}, nil
}

// linkAud is the closed set of audiences a channel link token may
// carry: the job surface and the owner-scoped model ops.
var linkAud = map[string]bool{
	"task-engine": true, "model-gateway": true, "spawn-controller": true,
}

func (s *Server) mintLinkToken(ctx context.Context, r idv1.MintLinkTokenReq) (idv1.MintLinkTokenResp, error) {
	p := httpapi.PrincipalFrom(ctx)
	if p == nil || p.Sub != "channel-hub" || !linkAud[r.Aud] {
		return idv1.MintLinkTokenResp{}, herrors.New(herrors.CodeForbidden, "err.forbidden")
	}
	resolved, err := s.resolve(ctx, idv1.ResolveReq{Channel: r.Channel, External: r.External})
	if err != nil {
		return idv1.MintLinkTokenResp{}, fmt.Errorf("resolve link token: %w", err)
	}
	exp := s.now() + idv1.ServiceTTL
	claims, err := json.Marshal(idv1.OwnerClaims{Sub: "channel-hub", Owner: resolved.Owner, Space: resolved.Space,
		Aud: []string{r.Aud}, Exp: exp, Kid: s.kid})
	if err != nil {
		return idv1.MintLinkTokenResp{}, fmt.Errorf("link claims: %w", err)
	}
	tok, err := s.sign("owner", claims)
	if err != nil {
		return idv1.MintLinkTokenResp{}, fmt.Errorf("sign link token: %w", err)
	}
	return idv1.MintLinkTokenResp{Token: tok, ExpiresAt: exp}, nil
}
