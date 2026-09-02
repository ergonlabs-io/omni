package suggest

import (
	"reflect"
	"testing"
)

func TestNear(t *testing.T) {
	keys := []string{"mode", "redact", "binary", "upstream"}

	for _, tc := range []struct {
		name  string
		input string
		from  []string
		want  []string
	}{
		{"transposition", "mdoe", keys, []string{"mode"}},
		{"dropped letter", "binry", keys, []string{"binary"}},
		{"doubled letter", "reddact", keys, []string{"redact"}},
		{"exact match is its own suggestion", "mode", keys, []string{"mode"}},
		{"too far to be a typo", "completely-different", keys, nil},
		{"no candidates", "mode", nil, nil},
		// Three edits away: offering this would be guessing, not correcting.
		{"beyond the distance limit", "modes123", keys, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Near(tc.input, tc.from)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Near(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestNearIsSorted pins the ordering. `omni config check` is something people
// diff between runs and in CI, and its callers render this slice straight
// into a "did you mean: a, b?" message — an unstable order there would make
// identical configs produce different output.
func TestNearIsSorted(t *testing.T) {
	// Every candidate is one edit from the input, so all of them match and
	// only the ordering distinguishes a correct result from a lucky one.
	got := Near("bat", []string{"cat", "bit", "bad", "bar", "at"})
	want := []string{"at", "bad", "bar", "bit", "cat"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Near = %v, want %v (sorted)", got, want)
	}
}

func TestDistanceCountsRunesNotBytes(t *testing.T) {
	// "é" is two bytes; replacing it is one edit, not two. Config keys are
	// ASCII today, but agent names come from user-written profiles.d files.
	if d := distance("café", "cafe"); d != 1 {
		t.Errorf("distance(café, cafe) = %d, want 1", d)
	}
}
