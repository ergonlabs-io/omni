package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"", "", true},
		{"", "x", false},
		{"claude-opus-5", "claude-opus-5", true},
		{"claude-opus-5", "claude-opus-4", false},
		{"claude-haiku-4-5*", "claude-haiku-4-5", true},
		{"claude-haiku-4-5*", "claude-haiku-4-5-20251001", true},
		{"claude-haiku-4-5*", "claude-haiku-3-5", false},
		{"*sonnet*", "claude-sonnet-5", true},
		{"*sonnet*", "claude-opus-5", false},
		{"claude-*-5", "claude-opus-5", true},
		{"claude-*-5", "claude-opus-4", false},
		{"?", "a", true},
		{"?", "ab", false},
		{"a?c", "abc", true},
		// '*' spans '/' and ':', unlike path.Match.
		{"minimax/*", "minimax/minimax-m3:free", true},
		{"*/*", "a/b", true},
		{"*:free", "minimax/minimax-m3:free", true},
		// '[' is a literal, not a character class.
		{"a[bc]d", "a[bc]d", true},
		{"a[bc]d", "abd", false},
		// Backtracking cases.
		{"*a*b", "aab", true},
		{"*a*b", "ba", false},
		{"a*a*a", "aaa", true},
		{"*x*x*x*", "xxx", true},
	}
	for _, tc := range cases {
		if got := Match(tc.pattern, tc.s); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

func TestIsLiteral(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"claude-opus-5", true},
		{"claude-*", false},
		{"a?b", false},
		{"", true},
	} {
		if got := IsLiteral(tc.in); got != tc.want {
			t.Errorf("IsLiteral(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
