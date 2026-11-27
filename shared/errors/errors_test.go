package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"
)

func TestStatusTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code Code
		want int
	}{
		{CodeInvalid, http.StatusBadRequest},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeConflict, http.StatusConflict},
		{CodeCapacity, http.StatusTooManyRequests},
		{CodeStale, http.StatusConflict},
		{CodeUnavailable, http.StatusServiceUnavailable},
		{CodeUncertain, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		if got := Status(c.code); got != c.want {
			t.Errorf("Status(%s)=%d want %d", c.code, got, c.want)
		}
	}
}

func TestRetryableByCode(t *testing.T) {
	t.Parallel()
	for _, c := range []Code{CodeStale, CodeUnavailable, CodeUncertain, CodeCapacity} {
		if !New(c, "err.x").Retryable {
			t.Errorf("%s must be retryable", c)
		}
	}
	for _, c := range []Code{CodeInvalid, CodeNotFound, CodeForbidden} {
		if New(c, "err.x").Retryable {
			t.Errorf("%s must not be retryable", c)
		}
	}
}

func TestWrapKeepsCauseAndClass(t *testing.T) {
	t.Parallel()
	cause := fmt.Errorf("db gone")
	err := Wrap(CodeUnavailable, "err.unavailable", cause)
	if !stderrors.Is(err, cause) {
		t.Fatal("wrapped cause lost")
	}
	if CodeOf(err) != CodeUnavailable {
		t.Fatalf("CodeOf=%s", CodeOf(err))
	}
	var e *Error
	if !stderrors.As(err, &e) || e.Code != CodeUnavailable {
		t.Fatal("As lost shared form")
	}
}

func TestCodeOfForeign(t *testing.T) {
	t.Parallel()
	if got := CodeOf(fmt.Errorf("raw")); got != CodeUnavailable {
		t.Fatalf("foreign maps to unavailable, got %s", got)
	}
}

func TestIsByCode(t *testing.T) {
	t.Parallel()
	if !stderrors.Is(Wrap(CodeStale, "err.stale", fmt.Errorf("x")), ErrStale) {
		t.Fatal("stale wrap must match ErrStale")
	}
}

func TestRequire(t *testing.T) {
	t.Parallel()
	if err := Require(true, "err.invalid"); err != nil {
		t.Fatal("require true must pass")
	}
	err := Require(false, "err.invalid")
	var e *Error
	if !stderrors.As(err, &e) || e.Code != CodeInvalid {
		t.Fatalf("require false code=%v", err)
	}
}
