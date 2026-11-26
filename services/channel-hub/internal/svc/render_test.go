package svc

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderNeutralAndOverflow(t *testing.T) {
	t.Parallel()
	got := renderAnswer("mock", "# Hello\n- item\n<script>alert(1)</script>")
	if len(got) != 1 || !strings.Contains(got[0].Body, "Hello\n• item") || strings.Contains(got[0].Body, "<script>") {
		t.Fatalf("unsafe or lost Markdown: %+v", got)
	}
	long := renderAnswer("mock", strings.Repeat("Ж", maxTextPart*maxTextParts+17))
	if len(long) != maxTextParts+1 || long[5].Kind != "file" || !strings.Contains(long[4].Body, "truncated") {
		t.Fatalf("overflow not visible: %d parts", len(long))
	}
	for _, p := range long[:5] {
		if utf8.RuneCountInString(p.Body) > maxTextPart {
			t.Fatal("part exceeds limit")
		}
	}
}
