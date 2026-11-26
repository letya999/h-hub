package svc

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	credv1 "github.com/letya999/h-hub/contracts/cred/v1"
	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/fsm"
	"github.com/letya999/h-hub/shared/httpapi"
	"github.com/letya999/h-hub/shared/log"
)

// telegramTokenRef is the broker object the owner installs with
// `hubctl secret set telegram-bot-token`.
const telegramTokenRef = "cred://builtin/platform/telegram-bot-token"

// The token lease is under two minutes; the adapter renews well
// before expiry and keeps the value in memory only.
const tgLeaseRefresh = 45 * time.Second

// tgAdapter polls Telegram when a bot token exists in the broker.
// Token absence is a quiet off switch, not a failure: the operator
// sets it with `hubctl secret set` and the next lease cycle starts
// polling.
type tgAdapter struct {
	s       *Server
	api     *tgAPI
	token   string
	tokenAt time.Time
}

// runTelegram is the adapter goroutine: lease loop and update loop
// share one context so a restart drops every lease and offset rules
// stay in one place.
func (s *Server) runTelegram(ctx context.Context, l *log.L, api *tgAPI) {
	ad := &tgAdapter{s: s, api: api}
	for {
		if ctx.Err() != nil {
			return
		}
		tok, err := ad.lease(ctx)
		if err != nil {
			l.Error(ctx, "telegram lease", err)
			ad.pause(ctx, 5*time.Second)
			continue
		}
		if tok == "" {
			ad.pause(ctx, tgLeaseRefresh) // no token installed yet
			continue
		}
		ad.pollOnce(ctx, l)
	}
}

// lease returns a fresh bot token, refreshing near expiry. Empty
// means the credential is not installed.
func (ad *tgAdapter) lease(ctx context.Context) (string, error) {
	if ad.token != "" && ad.s.now().Sub(ad.tokenAt) < tgLeaseRefresh {
		return ad.token, nil
	}
	c, err := ad.credClient(ctx)
	if err != nil {
		return "", err
	}
	res, err := c.Acquire(ctx, credv1.AcquireReq{Ref: telegramTokenRef})
	if err != nil {
		if herrors.CodeOf(err) == herrors.CodeNotFound {
			return "", nil // not installed yet: adapter stays inactive
		}
		return "", fmt.Errorf("acquire token: %w", err)
	}
	v := res.Fields["value"]
	if v == "" {
		return "", fmt.Errorf("acquire token: empty credential")
	}
	ad.token, ad.tokenAt = v, ad.s.now()
	return v, nil
}

func (ad *tgAdapter) pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// pollOnce performs one long-poll and admits every returned update
// into durable intake; the loop itself only normalizes and persists —
// model and provider work stays in outbox effects.
func (ad *tgAdapter) pollOnce(ctx context.Context, l *log.L) {
	offset, err := ad.tgOffset(ctx)
	if err != nil {
		l.Error(ctx, "telegram offset", err)
		ad.pause(ctx, 5*time.Second)
		return
	}
	updates, err := ad.api.getUpdates(ctx, ad.token, offset, 25)
	if err != nil {
		l.Error(ctx, "telegram poll", err)
		ad.pause(ctx, 3*time.Second)
		return
	}
	for _, raw := range updates {
		u := normalizeUpdate(raw)
		if err := ad.admitUpdate(ctx, u); err != nil {
			l.Error(ctx, "telegram intake", err)
			continue // offset stays: the row replays next poll
		}
		if u.Kind == "answer" && u.QueryID != "" {
			if err := ad.api.answerCallbackQuery(ctx, ad.token, u.QueryID); err != nil {
				l.Error(ctx, "telegram callback ack", err)
			}
		}
	}
}

// secretRe matches a KEY=value-looking message body — intercepted
// before it can become chat history (blueprint: the channel deletes
// the provider copy and teaches the safe path).
var secretRe = regexp.MustCompile(`(?m)^[A-Z_][A-Z0-9_]{1,}=[^\s]{4,}`)

// normalizeUpdate maps the wire update onto the conversation key
// (channel, peer, chat, topic). A forum thread and a direct-message
// topic are different fields — the prefixes keep them apart.
func normalizeUpdate(u tgRawUpdate) tgUpdate {
	out := tgUpdate{ID: u.UpdateID, Kind: "ignore"}
	if cq := u.CallbackQuery; cq != nil {
		return normalizeCallback(out, cq)
	}
	m := u.Message
	if m == nil || m.From == nil {
		return out
	}
	out.MessageID = m.MessageID
	if m.ReplyToMessage != nil {
		out.ReplyTo = m.ReplyToMessage.MessageID
	}
	out.FromID = m.From.ID
	out.ChatID = m.Chat.ID
	out.Topic = messageTopic(m)
	switch {
	case len(m.Photo) > 0:
		big := largestPhoto(m)
		out.Kind, out.FileID, out.FileSize, out.Text = "photo", big.FileID, big.FileSize, m.Caption
	case m.Text != "" && secretRe.MatchString(m.Text):
		out.Kind = "secret"
	case m.Text != "":
		out.Kind, out.Text = "text", m.Text
	}
	return out
}

func messageTopic(m *tgRawMessage) string {
	switch {
	case m.MessageThreadID != nil && m.Chat.IsForum:
		return "thr:" + strconv.FormatInt(*m.MessageThreadID, 10)
	case m.DirectMessagesTopic != nil:
		return "dm:" + strconv.FormatInt(m.DirectMessagesTopic.TopicID, 10)
	}
	return ""
}

// largestPhoto picks the highest-resolution variant telegram sent.
func largestPhoto(m *tgRawMessage) tgRawPhoto {
	big := m.Photo[0]
	for _, p := range m.Photo {
		if p.Width*p.Height > big.Width*big.Height {
			big = p
		}
	}
	return big
}

// normalizeCallback maps a button press to an "answer" update when
// its callback_data names a request (a:<request>:<choice>); other
// callbacks are ignored. The request id is unguessable — the
// presser's addressee check happens in dispatch.
func normalizeCallback(out tgUpdate, cq *tgRawCallback) tgUpdate {
	out.FromID, out.QueryID = cq.From.ID, cq.ID
	if cq.Message != nil {
		out.ChatID = cq.Message.Chat.ID
		out.Topic = messageTopic(cq.Message)
		out.ReplyTo = cq.Message.MessageID
	}
	parts := strings.Split(cq.Data, ":")
	if len(parts) != 3 || parts[0] != "a" {
		return out
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return out
	}
	out.Kind, out.RequestID, out.Choice = "answer", parts[1], n
	return out
}

// admitUpdate writes one normalized update and advances the durable
// offset in the same transaction — a crash can only lose the pair
// atomically, never one without the other.
func (ad *tgAdapter) admitUpdate(ctx context.Context, u tgUpdate) error {
	if u.Kind == "ignore" {
		return ad.setTGOffset(ctx, u.ID+1)
	}
	id, err := randomID("in-")
	if err != nil {
		return err
	}
	tx, err := ad.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tg intake begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	payload := fmt.Sprintf(`{"message_id":%d,"file_id":%q,"file_size":%d,"reply_to":%d}`,
		u.MessageID, u.FileID, u.FileSize, u.ReplyTo)
	if u.Kind == "answer" {
		payload = fmt.Sprintf(`{"request":%q,"choice":%d,"reply_to":%d}`,
			u.RequestID, u.Choice, u.ReplyTo)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO inbound_updates(
		id,channel,update_id,external_id,body,kind,chat_id,topic,payload)
		VALUES($1,'telegram',$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(channel,update_id) DO NOTHING`,
		id, strconv.FormatInt(u.ID, 10), strconv.FormatInt(u.FromID, 10),
		u.Text, u.Kind, strconv.FormatInt(u.ChatID, 10), u.Topic, payload)
	if err != nil {
		return fmt.Errorf("tg intake insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tg intake rows: %w", err)
	}
	if n > 0 {
		if err := ad.s.intake.Create(ctx, tx, clock{ad.s.deps.Now},
			fsm.CreateReq{ID: id, Initial: "queued", Actor: "telegram"}); err != nil {
			return fmt.Errorf("tg intake create: %w", err)
		}
		if err := ad.s.intake.Transition(ctx, tx, fsm.Request{ID: id, Event: "accept",
			Actor: "telegram", Effects: []fsm.Effect{{Topic: "dispatch", Payload: []byte(id)}}}, nil); err != nil {
			return fmt.Errorf("tg accept: %w", err)
		}
	}
	if err := setTGOffsetTx(ctx, tx, u.ID+1); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tg intake commit: %w", err)
	}
	return nil
}

// tgOffset reads the durable polling cursor; absence seeds zero.
func (ad *tgAdapter) tgOffset(ctx context.Context) (int64, error) {
	var off int64
	err := ad.s.db.QueryRowContext(ctx,
		`SELECT next_offset FROM tg_offsets WHERE channel='telegram'`).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("offset read: %w", err)
	}
	if off < 0 {
		return 0, fmt.Errorf("offset %d is invalid", off)
	}
	return off, nil
}

func (ad *tgAdapter) setTGOffset(ctx context.Context, off int64) error {
	tx, err := ad.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("offset begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := setTGOffsetTx(ctx, tx, off); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("offset commit: %w", err)
	}
	return nil
}

func setTGOffsetTx(ctx context.Context, tx *sql.Tx, off int64) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tg_offsets(channel,next_offset) VALUES('telegram',$1)
		 ON CONFLICT(channel) DO UPDATE SET next_offset=$1`, off); err != nil {
		return fmt.Errorf("offset write: %w", err)
	}
	return nil
}

// credClient builds a broker client with a fresh service token for
// the credential-broker audience.
func (ad *tgAdapter) credClient(ctx context.Context) (*credv1.Client, error) {
	id, err := ad.s.identityClient(ctx)
	if err != nil {
		return nil, err
	}
	nonce, err := randomID("nonce-")
	if err != nil {
		return nil, err
	}
	tok, err := id.IssueServiceToken(ctx, ad.s.sub, "credential-broker", nonce, ad.s.deps.SelfKey)
	if err != nil {
		return nil, fmt.Errorf("broker service token: %w", err)
	}
	return &credv1.Client{T: credv1.HTTP{
		C: httpapi.NewClient(ad.s.deps.BrokerURL, tok.Token)}}, nil
}
