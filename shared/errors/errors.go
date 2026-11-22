// Package errors is the single error form for every h-hub service and
// for hubctl. Codes are a closed allowlist; HTTP status is derived from
// the code in exactly one place (Status).
package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable error identifier.
type Code string

// Closed set of allowed codes. Adding a code requires owner review.
const (
	CodeInvalid      Code = "invalid"
	CodeNotFound     Code = "not_found"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeConflict     Code = "conflict"
	CodeStale        Code = "stale"
	CodeUnavailable  Code = "unavailable"
	CodeUncertain    Code = "uncertain"
)

// Error carries a stable code, a locale message key with args, an
// optional structured details payload, and the original cause.
type Error struct {
	Code      Code           `json:"code"`
	Retryable bool           `json:"retryable"`
	Key       string         `json:"message"`
	Args      map[string]any `json:"args,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	cause     error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Key) }
func (e *Error) Unwrap() error { return e.cause }

// Is matches sentinel errors of the same code so callers can use
// errors.Is(err, errors.ErrStale).
func (e *Error) Is(target error) bool {
	var t *Error
	if !stderrors.As(target, &t) {
		return false
	}
	return t.Code == e.Code
}

// With attaches a structured detail and returns the error.
func (e *Error) With(key string, v any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = v
	return e
}

// New builds an error with the given code and locale message key.
func New(code Code, key string) *Error {
	return &Error{Code: code, Key: key, Retryable: retryable(code)}
}

// Wrap keeps the cause in the chain while presenting the shared form.
func Wrap(code Code, key string, cause error) *Error {
	e := New(code, key)
	e.cause = cause
	return e
}

// Sentinel values for the most common comparisons.
var (
	ErrStale   = New(CodeStale, "err.stale")
	ErrUnclear = New(CodeUncertain, "err.uncertain")
)

// CodeOf extracts the shared code from any error chain. Foreign errors
// map to CodeUnavailable so internals never leak.
func CodeOf(err error) Code {
	var e *Error
	if stderrors.As(err, &e) {
		return e.Code
	}
	return CodeUnavailable
}

// Status is the only place where a code becomes an HTTP status.
func Status(c Code) int {
	switch c {
	case CodeInvalid:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeStale:
		return http.StatusConflict
	case CodeUnavailable, CodeUncertain:
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func retryable(c Code) bool {
	switch c {
	case CodeStale, CodeUnavailable, CodeUncertain:
		return true
	case CodeInvalid, CodeNotFound, CodeUnauthorized, CodeForbidden, CodeConflict:
		return false
	}
	return false
}

// Require is the common input validator: it returns nil when ok holds
// and a CodeInvalid error otherwise.
func Require(ok bool, key string) error {
	if ok {
		return nil
	}
	return New(CodeInvalid, key)
}
