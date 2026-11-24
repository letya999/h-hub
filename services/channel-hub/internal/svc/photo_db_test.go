//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
	mv1 "github.com/letya999/h-hub/contracts/model/v1"
	tv1 "github.com/letya999/h-hub/contracts/task/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/store"
	"github.com/letya999/h-hub/shared/testkit"
)

// tgStub answers getFile and the file download on the Bot API shape.
func tgStub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{botseg}/getFile", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, map[string]any{"ok": true,
			"result": map[string]any{"file_path": "photos/one.jpg", "file_size": 4}})
	})
	mux.HandleFunc("GET /file/{botseg}/photos/one.jpg", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// idStub issues tokens and resolves the telegram peer without crypto:
// the edge under test is the job request, not token verification.
func idStub(t *testing.T) *httptest.Server {
	t.Helper()
	ops := idv1.Ops(idv1.Handlers{
		IssueServiceToken: func(context.Context,
			idv1.IssueServiceTokenReq) (idv1.IssueServiceTokenResp, error) {
			return idv1.IssueServiceTokenResp{Token: "svc-tk", ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
		},
		Resolve: func(_ context.Context, r idv1.ResolveReq) (idv1.ResolveResp, error) {
			if r.Channel != "telegram" || r.External != "480637186" {
				return idv1.ResolveResp{}, herrors.New(herrors.CodeNotFound, "err.not-found")
			}
			return idv1.ResolveResp{Owner: "tguser1", Space: "space-t1"}, nil
		},
		MintLinkToken: func(context.Context,
			idv1.MintLinkTokenReq) (idv1.MintLinkTokenResp, error) {
			return idv1.MintLinkTokenResp{Token: "link-tk", ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
		},
	})
	mux := http.NewServeMux()
	for op, h := range ops {
		mux.Handle("/v1/"+op, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := h(w, r); err != nil { // proof-signed ops; stub ignores auth
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// credStub serves the leased bot token through the real contract fake.
func credStub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for op, h := range credv1.Ops(credv1.NewFake().Handlers()) {
		mux.Handle("/v1/"+op, httpapi.Handle("svc-tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if _, err := (&credv1.Client{T: credv1.HTTP{
		C: httpapi.NewClient(srv.URL, "svc-tk")}}).SetSecret(context.Background(),
		credv1.SetSecretReq{Name: "telegram-bot-token", Value: "tg-test-token"}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return srv
}

// taskStub mirrors the engine's missing-input refusal and records the
// request so the test can pin the idempotency key.
func taskStub(t *testing.T, got *tv1.CreateJobReq) *httptest.Server {
	t.Helper()
	ops := tv1.Ops(tv1.Handlers{CreateJob: func(_ context.Context,
		r tv1.CreateJobReq) (tv1.CreateJobResp, error) {
		*got = r
		if r.Space == "" || r.Conversation == "" || r.Input == "" || r.Idem == "" {
			return tv1.CreateJobResp{}, herrors.New(herrors.CodeInvalid, "err.missing-input")
		}
		return tv1.CreateJobResp{ID: "job-photo", State: "queued"}, nil
	}})
	mux := http.NewServeMux()
	for op, h := range ops {
		mux.Handle("/v1/"+op, httpapi.Handle("link-tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// schemaDB provisions the migrated schema and a signing key shared by
// the live-path tests below.
func schemaDB(t *testing.T) (*sql.DB, ed25519.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	_, selfKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return db, selfKey
}

// TestTelegramPhotoCreatesJob replays the live wedge: a photo update
// must reach CreateJob with the intake id as its idempotency key and
// land the intake in waiting — the empty Idem the effect used to send
// is refused by the engine as missing-input.
func TestTelegramPhotoCreatesJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, selfKey := schemaDB(t)
	var got tv1.CreateJobReq
	s := New("channel-hub", db, Deps{
		IdentityURL: idStub(t).URL,
		BrokerURL:   credStub(t).URL,
		TaskURL:     taskStub(t, &got).URL,
		TG:          NewTGAPI(tgStub(t).URL),
		SelfKey:     selfKey,
		Now:         time.Now,
	})
	u := rawText(9001, 480637186, 480637186, "")
	u.Message.Caption = "what is on the photo?"
	u.Message.Photo = append(u.Message.Photo,
		struct {
			FileID   string `json:"file_id"`
			FileSize int64  `json:"file_size"`
			Width    int64  `json:"width"`
			Height   int64  `json:"height"`
		}{FileID: "tgfile1", FileSize: 4, Width: 2, Height: 2})
	up := normalizeUpdate(u)
	if up.Kind != "photo" || up.FileID != "tgfile1" {
		t.Fatalf("normalize: %+v", up)
	}
	if err := s.tg.admitUpdate(ctx, up); err != nil {
		t.Fatalf("admit: %v", err)
	}
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM inbound_updates WHERE update_id='9001'`).Scan(&id); err != nil {
		t.Fatalf("intake id: %v", err)
	}
	done, err := s.tg.tgPhoto(ctx, id)
	if err != nil || !done {
		t.Fatalf("tgPhoto: done=%v err=%v", done, err)
	}
	var state string
	if err := db.QueryRowContext(ctx,
		`SELECT state FROM fsm.machines WHERE kind='intake' AND id=$1`, id).Scan(&state); err != nil {
		t.Fatalf("machine: %v", err)
	}
	if state != "waiting" {
		t.Fatalf("intake state %q", state)
	}
	if got.Idem != id || got.Space != "space-t1" || len(got.Attachments) != 1 {
		t.Fatalf("job req: %+v", got)
	}
}

// modelStub reports one no-vision active route so dispatchPhoto must
// refuse at the gate: a localized text plus a "Refused." status phase
// — never a job, never an upstream call.
func modelStub(t *testing.T) *httptest.Server {
	t.Helper()
	ops := mv1.Ops(mv1.Handlers{Routes: func(context.Context,
		mv1.RoutesReq) (mv1.RoutesResp, error) {
		return mv1.RoutesResp{Default: "devin", Routes: []mv1.RouteInfo{
			{Name: "devin", Model: "devin/swe-2-flash"}}}, nil
	}})
	mux := http.NewServeMux()
	for op, h := range ops {
		mux.Handle("/v1/"+op, httpapi.Handle("link-tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// taskStubDone takes the job, then reports one terminal state —
// failed runs carry the raw error in Result, which must not surface.
func taskStubDone(t *testing.T, job tv1.JobInfo) *httptest.Server {
	t.Helper()
	ops := tv1.Ops(tv1.Handlers{
		CreateJob: func(context.Context, tv1.CreateJobReq) (tv1.CreateJobResp, error) {
			return tv1.CreateJobResp{ID: job.ID, State: "queued"}, nil
		},
		GetJob: func(context.Context, tv1.GetJobReq) (tv1.GetJobResp, error) {
			return tv1.GetJobResp{Job: job}, nil
		},
	})
	mux := http.NewServeMux()
	for op, h := range ops {
		mux.Handle("/v1/"+op, httpapi.Handle("link-tk", h))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestTelegramPhotoNoVisionRefused pins the live finding: a photo on a
// no-vision route answers with the localized refusal and the status
// phase "Refused." — a refusal is not a failure.
func TestTelegramPhotoNoVisionRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, selfKey := schemaDB(t)
	var got tv1.CreateJobReq
	s := New("channel-hub", db, Deps{
		IdentityURL: idStub(t).URL,
		BrokerURL:   credStub(t).URL,
		TaskURL:     taskStub(t, &got).URL,
		ModelURL:    modelStub(t).URL,
		TG:          NewTGAPI(tgStub(t).URL),
		SelfKey:     selfKey,
		Now:         time.Now,
	})
	u := rawText(9002, 480637186, 480637186, "")
	u.Message.Photo = append(u.Message.Photo,
		struct {
			FileID   string `json:"file_id"`
			FileSize int64  `json:"file_size"`
			Width    int64  `json:"width"`
			Height   int64  `json:"height"`
		}{FileID: "tgfile2", FileSize: 4, Width: 2, Height: 2})
	up := normalizeUpdate(u)
	if err := s.tg.admitUpdate(ctx, up); err != nil {
		t.Fatalf("admit: %v", err)
	}
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM inbound_updates WHERE update_id='9002'`).Scan(&id); err != nil {
		t.Fatalf("intake id: %v", err)
	}
	r, err := inputOf(ctx, s, id)
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := dispatchPhoto(ctx, s, r); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT kind, body FROM deliveries WHERE intake_id=$1 ORDER BY kind`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var kind, body string
		if err := rows.Scan(&kind, &body); err != nil {
			t.Fatal(err)
		}
		seen[kind] = body
	}
	if !strings.Contains(seen["text"], "no vision") {
		t.Fatalf("refusal text: %q", seen["text"])
	}
	if seen["status"] != "Refused." {
		t.Fatalf("status phase: %q", seen["status"])
	}
	var state string
	if err := db.QueryRowContext(ctx,
		`SELECT state FROM fsm.machines WHERE kind='intake' AND id=$1`, id).Scan(&state); err != nil {
		t.Fatalf("machine: %v", err)
	}
	if state != "completed" {
		t.Fatalf("intake state %q", state)
	}
	if got.Space != "" {
		t.Fatalf("provider reached: %+v", got)
	}
}

// TestTelegramFailedJobLocalizedAnswer pins the live finding: a run
// that ends failed carries the raw error ("unavailable: ...") in
// Result; the user-facing answer must be the localized job-failed
// string, not the internal error code.
func TestTelegramFailedJobLocalizedAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, selfKey := schemaDB(t)
	s := New("channel-hub", db, Deps{
		IdentityURL: idStub(t).URL,
		BrokerURL:   credStub(t).URL,
		TaskURL: taskStubDone(t, tv1.JobInfo{ID: "job-f1", Done: true,
			State: "failed", Result: "unavailable: err.unavailable"}).URL,
		TG:      NewTGAPI(tgStub(t).URL),
		SelfKey: selfKey,
		Now:     time.Now,
	})
	up := normalizeUpdate(rawText(9004, 480637186, 480637186, "hello"))
	if err := s.tg.admitUpdate(ctx, up); err != nil {
		t.Fatalf("admit: %v", err)
	}
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM inbound_updates WHERE update_id='9004'`).Scan(&id); err != nil {
		t.Fatalf("intake id: %v", err)
	}
	if err := s.dispatch(ctx, id); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	done, err := s.pollJob(ctx, id)
	if err != nil || !done {
		t.Fatalf("poll: done=%v err=%v", done, err)
	}
	var body string
	if err := db.QueryRowContext(ctx,
		`SELECT body FROM deliveries WHERE intake_id=$1 AND kind='text'`, id).Scan(&body); err != nil {
		t.Fatalf("delivery: %v", err)
	}
	if strings.Contains(body, "unavailable") || !strings.Contains(body, "failed") {
		t.Fatalf("answer body: %q", body)
	}
}
