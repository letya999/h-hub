package locale

import (
	"strings"
	"testing"
)

func TestCatalogParity(t *testing.T) {
	t.Parallel()
	langs := Langs()
	if len(langs) < 2 {
		t.Fatal("need at least ru and en catalogs")
	}
	base := Catalog(langs[0])
	for _, l := range langs[1:] {
		cat := Catalog(l)
		for k := range base {
			if _, ok := cat[k]; !ok {
				t.Errorf("lang %s missing key %q", l, k)
			}
		}
		for k := range cat {
			if _, ok := base[k]; !ok {
				t.Errorf("lang %s has extra key %q", l, k)
			}
		}
	}
}

func TestTRendersByKey(t *testing.T) {
	t.Parallel()
	for _, l := range Langs() {
		got := T(l, "err.invalid-arg", "field", "state")
		if !strings.Contains(got, "state") {
			t.Errorf("lang %s: placeholder not replaced: %q", l, got)
		}
	}
}

func TestMissingKeyEchoes(t *testing.T) {
	t.Parallel()
	if got := T(EN, "err.no-such-key"); got != "err.no-such-key" {
		t.Fatalf("missing key must echo, got %q", got)
	}
	if _, ok := Lookup(EN, "err.no-such-key"); ok {
		t.Fatal("Lookup must report missing")
	}
}

func TestEveryKeyRenders(t *testing.T) {
	t.Parallel()
	for _, l := range Langs() {
		for k := range Catalog(l) {
			if T(l, k) == k {
				t.Errorf("lang %s key %q did not render", l, k)
			}
		}
	}
}
