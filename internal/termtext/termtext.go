// Package termtext makes text from outside axon safe to print on a
// terminal.
package termtext

import (
	"strings"
	"unicode"
)

// Clean returns s without control characters and Unicode format
// characters, such as the bidirectional overrides, so that text from a
// config file or a server cannot move the cursor, change colors or reorder
// what the terminal shows.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// IsUnsafe reports whether r is a control character or a Unicode format
// character, the characters Clean removes.
func IsUnsafe(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// HasUnsafe reports whether s holds a character Clean removes.
func HasUnsafe(s string) bool {
	return strings.IndexFunc(s, IsUnsafe) >= 0
}
