package svc

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/letya999/h-hub/shared/testkit"
)

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
