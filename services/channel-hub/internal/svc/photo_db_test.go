//go:build hhubdb

package svc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	idv1 "github.com/letya999/h-hub/contracts/identity/v1"
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

// TestTelegramPhotoCreatesJob replays the live wedge: a photo update
// must reach CreateJob with the intake id as its idempotency key and
// land the intake in waiting — the empty Idem the effect used to send
// is refused by the engine as missing-input.
func TestTelegramPhotoCreatesJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := testkit.TempSchema(t, ctx, "pgx", testkit.DSN())
	if err := fsm.EnsureSchema(ctx, db, "fsm"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db, Migrations(), "migrations"); err != nil {
		t.Fatal(err)
	}
	var got tv1.CreateJobReq
	_, selfKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
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
