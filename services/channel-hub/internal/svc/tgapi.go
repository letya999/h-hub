package svc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	Kind      string // text | photo | secret | answer | ignore
	Text      string
	FileID    string
	FileSize  int64
	RequestID string // answer kind: the request its button carried
	Choice    int    // answer kind: 1-based option index
	QueryID   string // answer kind: callback query to acknowledge
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
		return fmt.Errorf("telegram %s request: %w", method, redactToken(err, token))
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s call: %w", method, redactToken(err, token))
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
		if env.unchangedEdit(method) {
			return nil
		}
		return fmt.Errorf("telegram %s: api %d %s", method, env.ErrorCode, env.Description)
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram %s result: %w", method, err)
		}
	}
	return nil
}

// A retried edit whose text already matches has reached its destination.
func (e tgEnvelope) unchangedEdit(method string) bool {
	return method == "editMessageText" && e.ErrorCode == http.StatusBadRequest &&
		strings.HasPrefix(e.Description, "Bad Request: message is not modified")
}

// tgRawUpdate is the wire shape of one update.
type tgRawUpdate struct {
	UpdateID      int64          `json:"update_id"`
	Message       *tgRawMessage  `json:"message"`
	CallbackQuery *tgRawCallback `json:"callback_query"`
}

// tgRawMessage is the wire shape of one message update.
type tgRawMessage struct {
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
	Photo []tgRawPhoto `json:"photo"`
}

// tgRawPhoto is one resolution variant of a photo update.
type tgRawPhoto struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int64  `json:"width"`
	Height   int64  `json:"height"`
}

// tgRawCallback is the wire shape of a button press.
type tgRawCallback struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Message *struct {
		Chat struct {
			ID      int64 `json:"id"`
			IsForum bool  `json:"is_forum"`
		} `json:"chat"`
		MessageThreadID *int64 `json:"message_thread_id"`
	} `json:"message"`
}

// getUpdates long-polls; timeout is the provider-side wait.
func (a *tgAPI) getUpdates(ctx context.Context, token string,
	offset int64, timeoutSec int) ([]tgRawUpdate, error) {
	var out []tgRawUpdate
	err := a.call(ctx, token, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message", "callback_query"},
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

// tgRequestMsg is one owner request bound for the wire: its prompt
// text and the option labels its buttons carry.
type tgRequestMsg struct {
	Chat      int64
	Topic     string
	Text      string
	RequestID string
	Options   []string
}

// sendRequest posts one owner request with an inline keyboard — one
// button per option, callback_data "a:<request>:<1-based index>".
// The same index a mock user types is what the button reports.
func (a *tgAPI) sendRequest(ctx context.Context, token string, m tgRequestMsg) (int64, error) {
	body := map[string]any{"chat_id": m.Chat, "text": m.Text}
	topicJSON(body, m.Topic)
	var rows [][]map[string]any
	for i, o := range m.Options {
		rows = append(rows, []map[string]any{{
			"text":          o,
			"callback_data": fmt.Sprintf("a:%s:%d", m.RequestID, i+1)}})
	}
	body["reply_markup"] = map[string]any{"inline_keyboard": rows}
	var out struct {
		MessageID int64 `json:"message_id"`
	}
	err := a.call(ctx, token, "sendMessage", body, &out)
	return out.MessageID, err
}

// answerCallbackQuery stops the button's spinner; the reply itself
// arrives as the intake's completion notice.
func (a *tgAPI) answerCallbackQuery(ctx context.Context, token, queryID string) error {
	return a.call(ctx, token, "answerCallbackQuery",
		map[string]any{"callback_query_id": queryID}, nil)
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
		return nil, fmt.Errorf("telegram file request: %w", redactToken(err, token))
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram file: %w", redactToken(err, token))
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

// redactToken strips the leased token from an error before it can
// reach a log line: the Bot API puts the token in the request path,
// so transport errors quote the full URL.
func redactToken(err error, token string) error {
	if err == nil || token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "[redacted]"))
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
