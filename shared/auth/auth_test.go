package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignParseVerify(t *testing.T) {
	t.Parallel()
	pub, priv := keys(t)
	tok, err := Sign(priv, "bot", []byte(`{"u":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Purpose != "bot" || string(parsed.Payload) != `{"u":"1"}` {
		t.Fatalf("parsed=%+v", parsed)
	}
	if err := parsed.Verify(pub); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	_, priv := keys(t)
	pub2, _ := keys(t)
	tok, err := Sign(priv, "bot", []byte("p"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Verify(pub2); err == nil {
		t.Fatal("foreign key must fail")
	}
}

func TestParseMalformed(t *testing.T) {
	t.Parallel()
	for _, tok := range []string{
		"", "plain", "hhub_", "hhub_a", "hhub_a.b", "hhub_a.b.",
		"hhub_.a.b", "hhub_a.!.b", "hhub_a.YQ.!",
	} {
		if _, err := Parse(tok); err == nil {
			t.Errorf("token %q must fail", tok)
		}
	}
	if _, err := Parse("hhub_p.YQ.YQ"); !errors.Is(err, nil) && err == nil {
		t.Fatal(err)
	}
}
