package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	herrors "github.com/letya999/h-hub/shared/errors"
)

type echo struct {
	Msg string `json:"msg"`
}

func srv(t *testing.T, h Handler) (*httptest.Server, *Client) {
	t.Helper()
	s := httptest.NewServer(Handle("tok", h))
	t.Cleanup(s.Close)
	return s, NewClient(s.URL, "tok")
}

func TestAuthRequired(t *testing.T) {
	t.Parallel()
	s, _ := srv(t, func(w http.ResponseWriter, _ *http.Request) error {
		WriteJSON(w, echo{Msg: "ok"})
		return nil
	})
	res, err := s.Client().Post(s.URL+"/x", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Body.Close(); err != nil {
		t.Error(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	_, c := srv(t, func(w http.ResponseWriter, r *http.Request) error {
		var in echo
		if err := Decode(r, &in); err != nil {
			return err
		}
		WriteJSON(w, echo{Msg: in.Msg})
		return nil
	})
	var out echo
	if err := c.Do(context.Background(), "POST", "/x", echo{Msg: "hi"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Msg != "hi" {
		t.Fatalf("out=%+v", out)
	}
}

func TestErrorFormRoundTrip(t *testing.T) {
	t.Parallel()
	_, c := srv(t, func(_ http.ResponseWriter, _ *http.Request) error {
		return herrors.New(herrors.CodeStale, "err.stale")
	})
	err := c.Do(context.Background(), "GET", "/x", nil, nil)
	var e *herrors.Error
	if !errors.As(err, &e) {
		t.Fatalf("want shared error, got %v", err)
	}
	if e.Code != herrors.CodeStale || !e.Retryable {
		t.Fatalf("e=%+v", e)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	t.Parallel()
	s, _ := srv(t, func(w http.ResponseWriter, r *http.Request) error {
		var in echo
		if err := Decode(r, &in); err != nil {
			return err
		}
		WriteJSON(w, echo{})
		return nil
	})
	req, err := http.NewRequest("POST", s.URL+"/x", strings.NewReader(`{"msg":"a","x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Body.Close(); err != nil {
		t.Error(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestCopyParity(t *testing.T) {
	t.Parallel()
	type other struct {
		Msg string `json:"msg"`
	}
	var out echo
	if err := Copy(&out, struct {
		Msg   string `json:"msg"`
		Extra int    `json:"extra"`
	}{Msg: "a", Extra: 1}); err == nil {
		t.Fatal("Copy with unknown field must fail")
	}
	var dst other
	if err := Copy(&dst, echo{Msg: "x"}); err != nil || dst.Msg != "x" {
		t.Fatalf("dst=%+v err=%v", dst, err)
	}
}

func TestNormalizeHidesForeign(t *testing.T) {
	t.Parallel()
	e := Normalize(errors.New("sql: password leak"))
	if e.Code != herrors.CodeUnavailable {
		t.Fatalf("code=%s", e.Code)
	}
	if strings.Contains(e.Key, "password") {
		t.Fatal("foreign details leaked")
	}
}
