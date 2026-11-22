// Package log is the shared structured logger. It writes one JSON
// record per line, lifts correlation fields from context, and redacts
// secret field names. User content may only be logged through an
// expiring opt-in window owned by services, not here.
package log

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyRunID
	keyJobID
	keyTenant
)

// WithRequestID tags the context for correlation.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// WithRunID tags the context with a run identifier.
func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRunID, id)
}

// WithJobID tags the context with a job identifier.
func WithJobID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyJobID, id)
}

// WithTenant tags the context with a tenant/user identifier.
func WithTenant(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyTenant, id)
}

var secretFields = map[string]bool{
	"authorization": true,
	"password":      true,
	"secret":        true,
	"token":         true,
	"kek":           true,
	"seed":          true,
	"private_key":   true,
	"bearer":        true,
}

// L is a structured logger for one service.
type L struct {
	svc string
	out io.Writer
	mu  sync.Mutex
}

// New builds a logger writing to out.
func New(svc string, out io.Writer) *L {
	return &L{svc: svc, out: out}
}

type record struct {
	TS        string         `json:"ts"`
	Level     string         `json:"level"`
	Service   string         `json:"service"`
	Msg       string         `json:"msg"`
	Err       string         `json:"err,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	Tenant    string         `json:"tenant,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Info logs an informational record.
func (l *L) Info(ctx context.Context, msg string, kv ...any) {
	l.write(ctx, "info", msg, nil, kv)
}

// Error logs an error record.
func (l *L) Error(ctx context.Context, msg string, err error, kv ...any) {
	l.write(ctx, "error", msg, err, kv)
}

func (l *L) write(ctx context.Context, level, msg string, err error, kv []any) {
	r := record{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Service:   l.svc,
		Msg:       msg,
		RequestID: ctxID(ctx, keyRequestID),
		RunID:     ctxID(ctx, keyRunID),
		JobID:     ctxID(ctx, keyJobID),
		Tenant:    ctxID(ctx, keyTenant),
		Fields:    fields(kv),
	}
	if err != nil {
		r.Err = err.Error()
	}
	b, merr := json.Marshal(r)
	if merr != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.Write(append(b, '\n')) //nolint:errcheck // best-effort sink
}

func ctxID(ctx context.Context, k ctxKey) string {
	if v, ok := ctx.Value(k).(string); ok {
		return v
	}
	return ""
}

func fields(kv []any) map[string]any {
	if len(kv) == 0 {
		return nil
	}
	out := make(map[string]any, len(kv)/2+1)
	for i := 0; i+1 < len(kv); i += 2 {
		name, ok := kv[i].(string)
		if !ok {
			continue
		}
		if secretFields[name] {
			out[name] = "[redacted]"
			continue
		}
		out[name] = kv[i+1]
	}
	return out
}
