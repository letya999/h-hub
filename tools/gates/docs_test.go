package gates

import (
	"slices"
	"testing"
)

// TestHeadingsOf pins the README template comparison: the document H1
// is a per-service title, not a template section; level-2+ headings
// are compared verbatim and in order.
func TestHeadingsOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  string
		want []string
	}{
		{"h1 skipped", "# svc\n\n## Purpose\n## Owns\n", []string{"## Purpose", "## Owns"}},
		{"no h1", "## Purpose\n## Owns\n", []string{"## Purpose", "## Owns"}},
		{"levels kept", "# t\n## A\n### sub\n## B\n", []string{"## A", "### sub", "## B"}},
		{"empty", "no headings\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := headingsOf(c.doc); !slices.Equal(got, c.want) {
				t.Errorf("headingsOf = %v, want %v", got, c.want)
			}
		})
	}
}
