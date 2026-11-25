package svc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/letya999/h-hub/shared/testkit"
)

func TestTelegramAlreadyAppliedEdit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`, http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	api := NewTGAPI(server.URL)
	if err := api.editMessageText(t.Context(), "fixture", 42, 71, "same answer"); err != nil {
		t.Fatalf("already visible edit failed: %v", err)
	}
	if _, err := api.sendMessage(t.Context(), "fixture", 42, "", "same answer"); err == nil {
		t.Fatal("a refused send was acknowledged")
	}
}

func rawText(id, from, chat int64, text string) tgRawUpdate {
	var u tgRawUpdate
	u.UpdateID = id
	u.Message = &tgRawMessage{MessageID: id * 10, Text: text,
		From: &struct {
			ID int64 `json:"id"`
		}{ID: from}}
	u.Message.Chat.ID = chat
	return u
}

func TestLongRequestIsOneDocumentWithChoices(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("Ц", 4096) + "\n\nPick a color\n1. blue\n2. red"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/bottest/sendDocument" {
			t.Errorf("long request used %s", r.URL.Path)
		} else {
			checkQuestionDocument(t, r, text)
		}
		io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`) //nolint:errcheck // test response
	}))
	defer srv.Close()
	err := NewTGAPI(srv.URL).sendRequest(context.Background(), "test", tgRequestMsg{
		Chat: 73, Topic: "thr:17", Text: text, RequestID: "req-1", Options: []string{"blue", "red"}})
	if err != nil || calls.Load() != 1 {
		t.Fatalf("sent %d requests: %v", calls.Load(), err)
	}
}

func checkQuestionDocument(t *testing.T, r *http.Request, want string) {
	t.Helper()
	f, _, err := r.FormFile("document")
	if err != nil {
		t.Error(err)
		return
	}
	defer f.Close() //nolint:errcheck // test upload
	b, err := io.ReadAll(f)
	if err != nil || string(b) != want {
		t.Errorf("document did not preserve the full question: %v", err)
	}
	if r.FormValue("chat_id") != "73" || r.FormValue("message_thread_id") != "17" ||
		!strings.Contains(r.FormValue("caption"), "Pick a color") {
		t.Error("question or thread missing from document message")
	}
	var keys struct {
		Rows [][]map[string]string `json:"inline_keyboard"`
	}
	wantKeys := [][]map[string]string{
		{{"text": "blue", "callback_data": "a:req-1:1"}},
		{{"text": "red", "callback_data": "a:req-1:2"}},
	}
	if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &keys); err != nil || !reflect.DeepEqual(keys.Rows, wantKeys) {
		t.Errorf("choice buttons: %+v %v", keys, err)
	}
}

func TestNormalizeUpdateKinds(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")

	if got := normalizeUpdate(rawText(1, 7, 42, "hello")); got.Kind != "text" {
		t.Fatalf("text kind=%q", got.Kind)
	}
	sec := normalizeUpdate(rawText(2, 7, 42, "AWS_SECRET=abcd1234efgh"))
	testkit.Deny(t, "sys.secret-leak", sec.Kind == "secret")
	testkit.Deny(t, "sys.secret-leak",
		sec.Kind == "secret" && !strings.Contains(sec.Text, "abcd1234efgh"))
	if got := normalizeUpdate(rawText(3, 7, 42, "key=value")); got.Kind == "secret" {
		t.Fatal("lowercase key intercepted")
	}
	if got := normalizeUpdate(rawText(4, 7, 42, "A=b")); got.Kind == "secret" {
		t.Fatal("short pair intercepted")
	}
}

func TestTgAPIErrorNeverCarriesToken(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "sys.secret-leak")

	srv := httptest.NewServer(nil)
	srv.Close() // refuse every connection: the transport error quotes the URL
	api := NewTGAPI(srv.URL)
	const token = "8775588743:AAtestSecretTokenMustNotLeak"

	_, callErr := api.getUpdates(context.Background(), token, 1, 0)
	testkit.Deny(t, "sys.secret-leak",
		callErr != nil && !strings.Contains(callErr.Error(), token))

	_, fileErr := api.download(context.Background(), token, "p", 8)
	testkit.Deny(t, "sys.secret-leak",
		fileErr != nil && !strings.Contains(fileErr.Error(), token))
}

func TestNormalizeUpdatePhotoAndTopics(t *testing.T) {
	t.Parallel()
	u := rawText(5, 7, 42, "")
	u.Message.Caption = "look"
	u.Message.Photo = append(u.Message.Photo,
		tgRawPhoto{FileID: "small", FileSize: 10, Width: 10, Height: 10},
		tgRawPhoto{FileID: "big", FileSize: 90, Width: 90, Height: 90})
	got := normalizeUpdate(u)
	if got.Kind != "photo" || got.FileID != "big" || got.Text != "look" {
		t.Fatalf("photo: %+v", got)
	}

	thr := rawText(6, 7, 42, "in thread")
	tid := int64(99)
	thr.Message.MessageThreadID = &tid
	thr.Message.Chat.IsForum = true
	if got := normalizeUpdate(thr); got.Topic != "thr:99" {
		t.Fatalf("thread topic=%q", got.Topic)
	}
	dm := rawText(7, 7, 42, "dm")
	dm.Message.DirectMessagesTopic = &struct {
		TopicID int64 `json:"topic_id"`
	}{TopicID: 55}
	if got := normalizeUpdate(dm); got.Topic != "dm:55" {
		t.Fatalf("dm topic=%q", got.Topic)
	}
	var none tgRawUpdate
	if got := normalizeUpdate(none); got.Kind != "ignore" {
		t.Fatalf("empty update kind=%q", got.Kind)
	}
}

func TestTopicJSONFields(t *testing.T) {
	t.Parallel()
	body := map[string]any{}
	topicJSON(body, "thr:12")
	if body["message_thread_id"] != int64(12) {
		t.Fatalf("thread field: %v", body)
	}
	body = map[string]any{}
	topicJSON(body, "dm:9")
	if body["direct_messages_topic_id"] != int64(9) {
		t.Fatalf("dm field: %v", body)
	}
	body = map[string]any{}
	topicJSON(body, "")
	if len(body) != 0 {
		t.Fatalf("empty topic wrote fields: %v", body)
	}
	topicJSON(body, "thr:notanumber")
	if len(body) != 0 {
		t.Fatalf("bad topic wrote fields: %v", body)
	}
}
