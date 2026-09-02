package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInitCreatesPrivateDirs pins the modes on every directory whose
// contents or listing is sensitive. sessions/ is in this list deliberately:
// its listing alone reveals when you ran which agent, and it must defend
// itself rather than rely on the home directory above it — which Init
// cannot tighten if it already existed.
func TestInitCreatesPrivateDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, p := range []string{home, filepath.Join(home, "ca"), filepath.Join(home, "sessions")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("stat %s: %v", p, err)
			continue
		}
		if perm := fi.Mode().Perm(); perm != privatePerm {
			t.Errorf("%s perm = %04o, want %04o", p, perm, privatePerm)
		}
	}
}

// TestPermissionWarningsOnPreExistingHome covers the gap this function
// exists for: Init populating a directory somebody else created loosely.
// Init must not silently chmod it, but it must not stay quiet either.
func TestPermissionWarningsOnPreExistingHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Init leaves the directory as the user made it...
	fi, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o755 {
		t.Errorf("home perm = %04o, want 0755 untouched — Init must not chmod a directory it did not create", perm)
	}

	// ...and says so.
	warns := PermissionWarnings(home)
	if len(warns) == 0 {
		t.Fatal("no warning for a 0755 home; this is the case the check exists for")
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, home) {
		t.Errorf("warning does not name the path: %q", joined)
	}
	if !strings.Contains(joined, "chmod") {
		t.Errorf("warning is not actionable, no chmod suggested: %q", joined)
	}
}

func TestPermissionWarningsQuietWhenCorrect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if w := PermissionWarnings(home); len(w) != 0 {
		t.Errorf("a freshly initialized home warned about itself: %v", w)
	}
}

// A directory that is merely stricter than expected is fine and silent —
// the check is about exposure, not conformity.
func TestPermissionWarningsIgnoreStricter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(home, 0o700) })
	if w := PermissionWarnings(home); len(w) != 0 {
		t.Errorf("0500 home warned, but it is stricter than required: %v", w)
	}
}

func TestPermissionWarningsOnMissingHome(t *testing.T) {
	if w := PermissionWarnings(filepath.Join(t.TempDir(), "nope")); len(w) != 0 {
		t.Errorf("absent home warned: %v", w)
	}
}
