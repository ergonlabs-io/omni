// Package glob implements the wildcard matching omni uses for routing
// rules' `match` patterns.
//
// It exists as its own package rather than as a helper in either caller
// because two packages must agree on it *exactly*: internal/config
// validates rules (rejecting unknown backends, warning about shadowed
// rules) and internal/proxy applies them at request time. If those two
// disagreed about what a pattern matches, `omni config check` would be
// confidently describing behavior the proxy does not have.
package glob

// Match reports whether pattern matches s.
//
// The syntax is deliberately the small one users expect from a shell:
//
//   - matches any run of characters, including none
//     ?  matches exactly one character
//
// Everything else is a literal. Unlike path.Match, '*' also matches '/' —
// model identifiers are routinely vendor/model shaped ("minimax/minimax-m3"),
// and a pattern like "minimax/*" surprising nobody is worth more than
// path-segment semantics that mean nothing here. There are no character
// classes: '[' is a literal, so a pattern can never fail to compile and
// Match needs no error return.
//
// Matching is byte-wise. Model identifiers are ASCII, so '?' consuming one
// byte rather than one rune is not a distinction that arises; a multi-byte
// character in a pattern still matches itself literally.
func Match(pattern, s string) bool {
	// Two-pointer scan with backtracking to the most recent '*'. Linear on
	// realistic patterns, and it allocates nothing.
	var (
		p, si    int
		star     = -1 // index in pattern of the last '*' seen, or -1
		resumeAt int  // where in s to resume after that '*' absorbs one more byte
	)
	for si < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[si]):
			p++
			si++
		case p < len(pattern) && pattern[p] == '*':
			star = p
			p++
			resumeAt = si
		case star >= 0:
			// Mismatch, but a '*' is open: let it absorb one more byte.
			p = star + 1
			resumeAt++
			si = resumeAt
		default:
			return false
		}
	}
	// Trailing '*'s can match the empty remainder.
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// IsLiteral reports whether pattern contains no wildcards, i.e. it matches
// exactly one string: itself.
func IsLiteral(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' || pattern[i] == '?' {
			return false
		}
	}
	return true
}
