package textcut

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCutsAtACharacterBoundary(t *testing.T) {
	s := strings.Repeat("删除资源", 50)
	for n := 0; n < 40; n++ {
		got := Ellipsis(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("Ellipsis(_, %d) = %q is not UTF-8", n, got)
		}
		if p, _ := Prefix(s, n); len(p) > n {
			t.Fatalf("Prefix(_, %d) is %d bytes", n, len(p))
		}
	}
	if Ellipsis("short", 10) != "short" {
		t.Error("an uncut string changed")
	}
}
