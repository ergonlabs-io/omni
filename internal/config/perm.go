package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// PermissionWarnings reports the directories under home that are readable
// or writable by someone other than their owner and should not be, as
// human-readable lines a caller can print. `omni init` and
// `omni config check` both surface these — the latter because
// internal-docs/08-configuration.md lists permissions among the things
// `config check` is meant to validate.
//
// This exists because Init deliberately does not tighten a directory that
// already existed: an existing home is the user's, and silently chmod-ing
// someone's directory is worse than telling them about it. But saying
// nothing was the wrong other half of that bargain — `omni init` would
// populate a 0755 home, print "created ..." for every file, and leave the
// documented "~/.omni is 0700" guarantee looking satisfied. It is satisfied
// for a home omni created; this covers one it did not.
//
// The check is deliberately narrow: three directories whose contents or
// listing are sensitive, and only the group/other bits. Session files
// themselves are written 0700/0600 by internal/record and are not
// re-checked here.
//
// These are warnings, never errors. A loose home is worth telling someone
// about; it is not a reason to refuse to launch their agent. Returns nil
// when everything is as it should be, or when home does not exist yet.
func PermissionWarnings(home string) []string {
	var out []string
	for _, d := range []struct {
		path string
		why  string
	}{
		{home, "recorded sessions, and everything else omni keeps, live here"},
		{filepath.Join(home, "ca"), "this will hold the CA private key"},
		{filepath.Join(home, "sessions"), "session names reveal when you ran which agent"},
	} {
		fi, err := os.Stat(d.path)
		if err != nil || !fi.IsDir() {
			continue // absent, or not a directory: nothing to say
		}
		perm := fi.Mode().Perm()
		if perm&^fs.FileMode(privatePerm) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s is %04o, not %04o — %s.\n  fix with: chmod %04o %s",
			d.path, perm, privatePerm, d.why, privatePerm, d.path,
		))
	}
	return out
}
