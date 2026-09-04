package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/omni.conf.tmpl
var omniConfTemplate []byte

// The credentials file ships as comments only. It exists so the mode is
// right before there is anything in it to protect: the file has to be 0600
// or stricter or omni refuses to read it at all, and a user who creates it
// by hand at the moment they first need a key is exactly the user who hits
// that refusal. Writing it at init makes uncommenting a line the whole
// procedure. writeFile never clobbers, so a real key here is safe.
//
//go:embed templates/credentials.tmpl
var credentialsTemplate []byte

// Permissions are set explicitly at creation rather than left to umask.
// (umask can only clear bits, never set them, so an explicit mode is a
// ceiling — but a permissive umask must not be able to widen these.)
//
// privatePerm is for anything whose *contents or listing* is sensitive:
// ~/.omni, which holds everything below it, and sessions/, whose listing
// alone leaks when you ran which agent and whose contents are prompts and
// source code.
//
// sessions/ used to be dirPerm on the theory that the home directory's
// 0700 already blocks traversal. It does — but only when the home is one
// omni created. Init cannot tighten a directory that already existed (see
// PermissionWarnings), so a sessions/ that defends itself is worth the one
// changed constant. Its contents are separately 0700/0600, written by
// internal/record.
//
// dirPerm is for directories whose listing is not interesting on its own.
const (
	privatePerm = 0o700
	dirPerm     = 0o755
	filePerm    = 0o644
)

// Init creates the omni home tree under home and writes one fully-commented
// default config file:
//
//	~/.omni/
//	├── omni.conf          (everything: defaults, backends, per-agent)
//	├── credentials        (0600, comments only until you add a key)
//	├── profiles.d/
//	└── sessions/
//
// Nothing else. A cache/ directory was created here for a Models API
// capability cache, and a ca/ for Tier 2's certificate authority; neither
// feature exists, and nothing read or wrote either directory. They come
// back with the code that needs them, created at that point with the
// permissions that code requires.
//
// Only omni.conf is written, and it is the only file omni reads for
// defaults, backends, and per-agent settings — a single setting lives in a
// single place.
//
// Init is idempotent: it never overwrites or removes an existing file or
// directory. It returns the absolute paths of everything it newly created,
// in creation order, so a caller (`omni init`) can report exactly what
// happened. Safe to call on every `omni claude` invocation for
// auto-bootstrap — a repeat call against an already-initialized home
// creates nothing and returns an empty slice.
func Init(home string) ([]string, error) {
	var created []string

	mkdir := func(path string, perm os.FileMode) error {
		if _, err := os.Stat(path); err == nil {
			return nil // already exists — leave it alone
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(path, perm); err != nil {
			return err
		}
		created = append(created, path)
		return nil
	}

	writeFile := func(path string, content []byte, perm os.FileMode) error {
		if _, err := os.Stat(path); err == nil {
			return nil // never clobber an existing file
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, content, perm); err != nil {
			return err
		}
		created = append(created, path)
		return nil
	}

	if err := mkdir(home, privatePerm); err != nil {
		return created, fmt.Errorf("config: init %s: %w", home, err)
	}
	// Directory permissions are not retroactively tightened on an
	// already-existing home — Init only sets them at creation time, per
	// spec. (An existing world-readable ~/.omni is left as the user made
	// it; Init does not silently rewrite permissions out from under them.)

	for _, d := range []struct {
		path string
		perm os.FileMode
	}{
		{filepath.Join(home, "profiles.d"), dirPerm},
		{filepath.Join(home, "sessions"), privatePerm},
	} {
		if err := mkdir(d.path, d.perm); err != nil {
			return created, fmt.Errorf("config: init %s: %w", d.path, err)
		}
	}

	if err := writeFile(GlobalConfigPath(home), omniConfTemplate, filePerm); err != nil {
		return created, fmt.Errorf("config: init %s: %w", GlobalConfigPath(home), err)
	}
	if err := writeFile(CredentialsPath(home), credentialsTemplate, credentialsPerm); err != nil {
		return created, fmt.Errorf("config: init %s: %w", CredentialsPath(home), err)
	}

	return created, nil
}
