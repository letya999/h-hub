package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// tgAPI is a minimal Telegram Bot API client: getUpdates long
// polling, message send/edit/delete and file download. The token
// arrives per call from the broker lease — it is never stored on the
// client, never logged, never persisted.
type tgAPI struct {
	base string // https://api.telegram.org in production, a test URL otherwise
	hc   *http.Client
}

// NewTGAPI builds the Bot API client; main passes the real endpoint,
// tests pass a stub URL or leave Deps.TG nil to disable the adapter.
func NewTGAPI(base string) *tgAPI {
	return &tgAPI{base: base, hc: &http.Client{Timeout: 40 * time.Second}}
}

// tgUpdate is the normalized slice of an update we consume.
type tgUpdate struct {
	ID        int64
	MessageID int64
	FromID    int64
	ChatID    int64
	Topic     string // "thr:<id>" for forum threads, "dm:<id>" for DM topics, "" otherwise
	Kind      string // text | photo | secret | ignore
	Text      string
	FileID    string
	FileSize  int64
}

type tgEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

func (a *tgAPI) call(ctx context.Context, token, method string,
	body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("telegram %s encode: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.base+"/bot"+token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("telegram %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s call: %w", method, err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("telegram %s read: %w", method, err)
	}
	var env tgEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("telegram %s decode: %w", method, err)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: api %d %s", method, env.ErrorCode, env.Description)
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram %s result: %w", method, err)
		}
	}
	return nil
}

// tgRawUpdate is the wire shape of one update.
type tgRawUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID           int64  `json:"message_id"`
		MessageThreadID     *int64 `json:"message_thread_id"`
		Text                string `json:"text"`
		Caption             string `json:"caption"`
		DirectMessagesTopic *struct {
			TopicID int64 `json:"topic_id"`
		} `json:"direct_messages_topic"`
		From *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID      int64  `json:"id"`
			Type    string `json:"type"`
			IsForum bool   `json:"is_forum"`
		} `json:"chat"`
		Photo []struct {
			FileID   string `json:"file_id"`
			FileSize int64  `json:"file_size"`
			Width    int64  `json:"width"`
			Height   int64  `json:"height"`
		} `json:"photo"`
	} `json:"message"`
}

// getUpdates long-polls; timeout is the provider-side wait.
func (a *tgAPI) getUpdates(ctx context.Context, token string,
	offset int64, timeoutSec int) ([]tgRawUpdate, error) {
	var out []tgRawUpdate
	err := a.call(ctx, token, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message"},
	}, &out)
	return out, err
}

// sendMessage returns the provider message id for later edits.
func (a *tgAPI) sendMessage(ctx context.Context, token string,
	chat int64, topic, text string) (int64, error) {
	body := map[string]any{"chat_id": chat, "text": text}
	topicJSON(body, topic)
	var out struct {
		MessageID int64 `json:"message_id"`
	}
	err := a.call(ctx, token, "sendMessage", body, &out)
	return out.MessageID, err
}

func (a *tgAPI) editMessageText(ctx context.Context, token string,
	chat, msg int64, text string) error {
	return a.call(ctx, token, "editMessageText", map[string]any{
		"chat_id": chat, "message_id": msg, "text": text}, nil)
}

func (a *tgAPI) deleteMessage(ctx context.Context, token string, chat, msg int64) error {
	return a.call(ctx, token, "deleteMessage", map[string]any{
		"chat_id": chat, "message_id": msg}, nil)
}

// filePath resolves a file_id to a download path.
func (a *tgAPI) filePath(ctx context.Context, token, fileID string) (string, error) {
	var out struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	err := a.call(ctx, token, "getFile", map[string]any{"file_id": fileID}, &out)
	return out.FilePath, err
}

// download fetches a file's bytes via the file endpoint.
func (a *tgAPI) download(ctx context.Context, token, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.base+"/file/bot"+token+"/"+path, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram file request: %w", err)
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram file: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort close
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("telegram file read: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("telegram file over %d bytes", limit)
	}
	return data, nil
}

// topicJSON writes the correct thread field: forum threads and
// direct-message topics are different fields, never one (blueprint).
func topicJSON(body map[string]any, topic string) {
	if len(topic) > 4 && topic[:4] == "thr:" {
		if id, err := strconv.ParseInt(topic[4:], 10, 64); err == nil {
			body["message_thread_id"] = id
		}
	}
	if len(topic) > 3 && topic[:3] == "dm:" {
		if id, err := strconv.ParseInt(topic[3:], 10, 64); err == nil {
			body["direct_messages_topic_id"] = id
		}
	}
}
