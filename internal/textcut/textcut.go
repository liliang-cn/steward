// Package textcut shortens text for display without cutting a character in
// half.
//
// Every shortening in steward sliced bytes: s[:160] on Chinese text ends in
// the middle of a three-byte character more often than not, and the result is
// not UTF-8. It broke a reader decoding search output, and the same cut fed
// the recall context /chat sends a model.
package textcut

import "unicode/utf8"

// Prefix returns s cut to at most n bytes, at a character boundary, and
// whether anything was cut.
func Prefix(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	if n < 0 {
		n = 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

// Ellipsis is s cut to at most n bytes with "…" appended when cut.
func Ellipsis(s string, n int) string {
	if p, cut := Prefix(s, n); cut {
		return p + "…"
	}
	return s
}
