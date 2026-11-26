//go:build hhubdb

package svc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"testing"
	"time"

	"github.com/letya999/h-hub/shared/httpapi"
)

type telegramReplyCall struct {
	method, text string
	messageID    int64
}

func TestTelegramSuccessLeavesOnePlainAnswer(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"progress first", "answer first"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			const answer = "I'm here: <name> & literal &lt;"
			calls := make(chan telegramReplyCall, 10)
			s := telegramReplyServer(t, calls)
			r := waitingTelegramIntake(t, s)
			if order == "progress first" {
				sendReplyDeliveries(t, s, r.id)
			}
			if err := s.completeText(t.Context(), r, answer); err != nil {
				t.Fatal(err)
			}
			var postedDone bool
			if err := s.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM deliveries
				WHERE intake_id=$1 AND kind='status' AND body='Done.')`, r.id).Scan(&postedDone); err != nil || postedDone {
				t.Fatalf("standalone completion status queued=%v err=%v", postedDone, err)
			}
			sendReplyDeliveries(t, s, r.id)
			sendReplyDeliveries(t, s, r.id)
			close(calls)
			var got []telegramReplyCall
			for call := range calls {
				got = append(got, call)
			}
			want := []telegramReplyCall{{"sendMessage", answer, 0}}
			if order == "progress first" {
				want = []telegramReplyCall{{"sendMessage", "Working…", 0}, {"editMessageText", answer, 71}}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("Bot API deliveries: got %+v, want %+v", got, want)
			}
		})
	}
}

func telegramReplyServer(t *testing.T, calls chan<- telegramReplyCall) *Server {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			Text      string
			MessageID int64  `json:"message_id"`
			ParseMode string `json:"parse_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil || msg.ParseMode != "" {
			t.Errorf("plain Telegram message: parse mode %q, error %v", msg.ParseMode, err)
		}
		calls <- telegramReplyCall{path.Base(r.URL.Path), msg.Text, msg.MessageID}
		httpapi.WriteJSON(w, map[string]any{"ok": true, "result": map[string]int{"message_id": 71}})
	}))
	t.Cleanup(api.Close)
	db, key := schemaDB(t)
	return New("channel-hub", db, Deps{IdentityURL: idStub(t).URL, BrokerURL: credStub(t).URL,
		TG: NewTGAPI(api.URL), SelfKey: key, Now: time.Now})
}

func waitingTelegramIntake(t *testing.T, s *Server) inputRow {
	t.Helper()
	ctx := t.Context()
	if err := s.tg.admitUpdate(ctx, normalizeUpdate(rawText(9301, 81, 81, "hello"))); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM inbound_updates WHERE update_id='9301'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	r, err := inputOf(ctx, s, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.waitJob(ctx, r, "job"); err != nil {
		t.Fatal(err)
	}
	r, err = inputOf(ctx, s, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sendReplyDeliveries(t *testing.T, s *Server, intake string) {
	t.Helper()
	ctx := t.Context()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM deliveries WHERE intake_id=$1
		ORDER BY (kind='status'), created_at`, intake)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if done, err := s.send(ctx, id); err != nil || !done {
			t.Fatalf("deliver reply: done=%v err=%v", done, err)
		}
	}
}
