package termtext

import "testing"

func TestClean(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"plain text", "plain text"},
		{"Café ü", "Café ü"},
		{"a\x1b[31mred\x1b[0m", "a[31mred[0m"},
		{"line\nbreak\ttab\r", "linebreaktab"},
		{"bell\a and del\x7f", "bell and del"},
		{"c1\u0085 next", "c1 next"},
		{"bidi ‮evil‬", "bidi evil"},
		{"zero​width", "zerowidth"},
		{"", ""},
	} {
		if got := Clean(tc.in); got != tc.want {
			t.Errorf("Clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got, want := HasUnsafe(tc.in), tc.in != tc.want; got != want {
			t.Errorf("HasUnsafe(%q) = %v, want %v", tc.in, got, want)
		}
	}
}
