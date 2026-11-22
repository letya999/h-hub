// Package httpapi is the shared transport plumbing: bearer checks with
// constant-time comparison, body limits, JSON decode rules, and the
// shared error form. In-process calls reuse the same codecs through
// Copy so behaviour matches over the wire.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	stderrors "errors"
	"net/http"

	herrors "github.com/letya999/h-hub/shared/errors"
	"github.com/letya999/h-hub/shared/locale"
)

// MaxBody bounds any request or response body we read.
const MaxBody int64 = 1 << 20

// MaxErrorBody bounds error bodies we read from peers.
const MaxErrorBody int64 = 4096

// Handler is an endpoint that returns an error instead of writing a
// response on failure.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Handle wraps a handler with bearer auth, a body limit, and shared
// error normalization.
func Handle(token string, next Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			writeError(w, r, herrors.New(herrors.CodeUnauthorized, "err.unauthorized"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		if err := next(w, r); err != nil {
			writeError(w, r, err)
		}
	}
}

func authorized(r *http.Request, token string) bool {
	got := r.Header.Get("Authorization")
	want := "Bearer " + token
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

type errorBody struct {
	Code      string         `json:"code"`
	Retryable bool           `json:"retryable"`
	Message   string         `json:"message"`
	Lang      string         `json:"message_lang"`
	Args      map[string]any `json:"args,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Err errorBody `json:"error"`
}

func langOf(r *http.Request) locale.Lang {
	if r.Header.Get("Accept-Language") == "ru" {
		return locale.RU
	}
	return locale.EN
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e := Normalize(err)
	lang := langOf(r)
	msg := locale.T(lang, e.Key, flattenArgs(e.Args)...)
	env := errorEnvelope{Err: errorBody{
		Code:      string(e.Code),
		Retryable: e.Retryable,
		Message:   msg,
		Lang:      string(lang),
		Args:      e.Args,
		Details:   e.Details,
	}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(herrors.Status(e.Code))
	enc := json.NewEncoder(w)
	enc.Encode(env) //nolint:errcheck // best-effort response body
}

func flattenArgs(args map[string]any) []any {
	out := make([]any, 0, len(args)*2)
	for k, v := range args {
		out = append(out, k, v)
	}
	return out
}

// WriteJSON encodes v with a 200 status.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.Encode(v) //nolint:errcheck // best-effort response body
}

// Decode reads a JSON body rejecting unknown fields.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return herrors.Wrap(herrors.CodeInvalid, "err.decode", err)
	}
	return nil
}

// Normalize maps any error into the shared form; foreign errors become
// a generic unavailable so internals never reach the wire.
func Normalize(err error) *herrors.Error {
	if err == nil {
		return nil
	}
	var e *herrors.Error
	if stderrors.As(err, &e) {
		return e
	}
	return herrors.New(herrors.CodeUnavailable, "err.unavailable")
}
