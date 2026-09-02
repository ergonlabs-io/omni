package config

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CredentialsFileName is the secrets file under the omni home. It is the one
// file in the tree that is allowed to contain an API key, and it is
// deliberately *not* config: it is never merged into an [Effective], never
// printed by `omni config show`, never recorded, and never handed to the
// launched agent's environment. omni reads it to authenticate itself to a
// routing backend, and nothing else.
//
// It exists because the alternative people actually reach for is pasting the
// key into omni.conf — a file `omni init` writes 0644, that gets committed to
// dotfile repos and pasted into bug reports. A separate 0600 file whose only
// content is secrets is the smaller hole, and it lets the "no credentials in
// config, ever" rule stay literally true rather than becoming advice.
const CredentialsFileName = "credentials"

// credentialsPerm is the only mode the credentials file may have. Anything
// with a group or other bit set is refused rather than read — see
// loadCredentials.
const credentialsPerm fs.FileMode = 0o600

// envNameRe constrains a credentials-file key to a portable environment
// variable name, which is what it is: the left-hand side has to match some
// backend's api_key_env to ever be looked up.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CredentialsPath returns the path to the credentials file under home.
func CredentialsPath(home string) string {
	return filepath.Join(home, CredentialsFileName)
}

// Credentials is the set of secrets read from the credentials file, keyed by
// environment variable name.
//
// The zero value is valid and empty, which is what every caller gets when the
// file does not exist — the file is optional, and exporting the variable in a
// shell remains a first-class way to supply a key.
type Credentials struct {
	path string
	vals map[string]string
}

// Path returns the file these credentials were read from, whether or not it
// existed. Callers use it in error messages ("set it in the environment, or
// add it to <path>"), so it is populated even for an empty set.
func (c Credentials) Path() string { return c.path }

// Lookup returns the secret stored under name.
func (c Credentials) Lookup(name string) (string, bool) {
	v, ok := c.vals[name]
	return v, ok
}

// Names returns the variable names defined in the file, sorted. Names, never
// values: this is what makes it safe to print when someone is working out why
// a backend still has no credential.
func (c Credentials) Names() []string {
	out := make([]string, 0, len(c.vals))
	for k := range c.vals {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// loadCredentials reads home's credentials file. A missing file is not a
// problem and yields an empty set with no issues.
//
// Permissions are enforced, not advised: a credentials file readable by
// anyone but its owner is refused entirely (LevelFatal, the same level a
// credential pasted into omni.conf gets) rather than read with a warning.
// Reading it anyway would leave the secret exposed and teach the user that
// the mode does not matter, which is the opposite of the reason the file
// exists.
//
// Parse problems are LevelError: they stop a launch and fail `config check`,
// but they do not poison an otherwise loadable configuration the way a
// credential in config does. No Issue this function produces ever contains a
// value from the file — only names, line numbers, and the path.
func loadCredentials(home string) (Credentials, []Issue) {
	path := CredentialsPath(home)
	c := Credentials{path: path, vals: map[string]string{}}

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil // optional file, and the common case
		}
		return c, []Issue{{
			Path:    "credentials",
			Message: fmt.Sprintf("cannot stat credentials file: %s", err),
			Source:  path,
			Level:   LevelError,
		}}
	}
	if fi.IsDir() {
		return c, []Issue{{
			Path:    "credentials",
			Message: "credentials path is a directory, not a file",
			Source:  path,
			Level:   LevelError,
		}}
	}
	if perm := fi.Mode().Perm(); perm&^credentialsPerm != 0 {
		return c, []Issue{{
			Path: "credentials",
			Message: fmt.Sprintf(
				"credentials file is %04o, not %04o — it holds API keys and must not be "+
					"readable by anyone else; fix with: chmod %04o %s",
				perm, credentialsPerm, credentialsPerm, path,
			),
			Source: path,
			Level:  LevelFatal,
		}}
	}

	f, err := os.Open(path)
	if err != nil {
		return c, []Issue{{
			Path:    "credentials",
			Message: fmt.Sprintf("cannot read credentials file: %s", err),
			Source:  path,
			Level:   LevelError,
		}}
	}
	defer f.Close()

	var issues []Issue
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		name, value, iss := parseCredentialLine(sc.Text(), path, line)
		if iss != nil {
			issues = append(issues, *iss)
			continue
		}
		if name == "" {
			continue // blank or comment
		}
		if _, dup := c.vals[name]; dup {
			issues = append(issues, Issue{
				Path:    "credentials." + name,
				Message: fmt.Sprintf("%s is defined more than once; the last definition wins", name),
				Source:  fmt.Sprintf("%s:%d", path, line),
				Level:   LevelWarning,
			})
		}
		c.vals[name] = value
	}
	if err := sc.Err(); err != nil {
		issues = append(issues, Issue{
			Path:    "credentials",
			Message: fmt.Sprintf("cannot read credentials file: %s", err),
			Source:  path,
			Level:   LevelError,
		})
	}
	return c, issues
}

// parseCredentialLine parses one NAME=value line. It returns an empty name
// for a blank or comment line. A leading "export " is accepted because the
// line people have on their clipboard is the one from their shell rc, and
// rejecting it would be pedantry with no upside.
func parseCredentialLine(raw, path string, line int) (name, value string, iss *Issue) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", nil
	}
	s = strings.TrimPrefix(s, "export ")

	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return "", "", &Issue{
			Path:    "credentials",
			Message: "malformed line (want NAME=value)",
			Source:  fmt.Sprintf("%s:%d", path, line),
			Level:   LevelError,
		}
	}
	k = strings.TrimSpace(k)
	if !envNameRe.MatchString(k) {
		return "", "", &Issue{
			Path: "credentials",
			Message: fmt.Sprintf(
				"%q is not a valid environment variable name (want letters, digits and "+
					"'_', not starting with a digit)", k,
			),
			Source: fmt.Sprintf("%s:%d", path, line),
			Level:  LevelError,
		}
	}
	return k, unquote(strings.TrimSpace(v)), nil
}

// unquote strips one matching pair of surrounding quotes. Nothing else is
// interpreted: no escape sequences, no variable expansion. A credentials file
// is not a shell script, and a key containing a backslash must survive
// verbatim.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// CredentialsPathForMessage names the credentials file for a user-facing
// message, falling back to the conventional path for an Effective that was
// not built by Load (no home was ever resolved, so there is no real path to
// quote). A message that says "or put it in the credentials file" is only
// useful if it says which file.
func (e *Effective) CredentialsPathForMessage() string {
	if p := e.CredentialsPath(); p != "" {
		return p
	}
	return "~/.omni/" + CredentialsFileName
}
