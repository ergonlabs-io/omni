// Package suggest turns a near-miss into a "did you mean" list.
//
// It exists because omni tells the user when they typo'd something — a
// config key, an agent name — and the two places that do it had grown
// separate, near-identical copies of the same edit-distance routine. The
// shared thing worth naming is not the metric; it is the rule that a
// suggestion is a candidate within distance 2, reported in a stable order.
package suggest

import "sort"

// maxDistance is how far a candidate may be from the input and still be
// offered. Two edits catches the realistic typos — a transposition, a
// doubled or dropped letter, "mdoe" for "mode" — without suggesting
// something the user plainly did not mean.
const maxDistance = 2

// Near returns the candidates within maxDistance of input, sorted, so a key
// with more than one near-miss produces the same message every run. Callers
// render the result as "did you mean: a, b?" and rely on that stability —
// `omni config check` is something people diff.
func Near(input string, candidates []string) []string {
	var out []string
	for _, c := range candidates {
		if distance(input, c) <= maxDistance {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// distance is the Levenshtein edit distance between a and b, counted in
// runes rather than bytes so a multi-byte character costs one edit.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
