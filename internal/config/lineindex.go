package config

import (
	"strconv"
	"strings"
)

// lineIndex is a lightweight, line-oriented TOML scan that maps a dotted key
// path to the 1-based line number it was assigned on. It is not a general
// TOML parser — it does not understand multi-line strings/arrays or inline
// tables spanning lines — but omni's own schema and generated files never
// use those, so this is exact for every file omni reads or writes.
//
// This exists because github.com/BurntSushi/toml's MetaData does not expose
// per-key line numbers (only line numbers on syntax errors), and
// `omni config show` needs real "file:line" provenance per
// internal-docs/08-configuration.md.
func lineIndex(content string) map[string]int {
	idx := map[string]int{}
	var section []string
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			h := strings.TrimSpace(strings.Trim(line, "[]"))
			// Ignore array-of-tables ([[x]]) — unused by our schema; treat
			// the header as unparseable rather than guess wrong.
			if h == "" {
				continue
			}
			section = splitTOMLKeyPath(h)
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		path := append(append([]string{}, section...), splitTOMLKeyPath(key)...)
		idx[strings.Join(path, ".")] = i + 1
	}
	return idx
}

// splitTOMLKeyPath splits a possibly dotted, possibly quoted TOML key (as
// found in a table header or on the left of "=") into its path segments.
// Quoting is stripped; this deliberately does not handle a literal "." or
// "]" inside a quoted segment — not needed for omni's schema.
func splitTOMLKeyPath(s string) []string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		parts[i] = p
	}
	return parts
}

// sourceAt returns "file:line" for dotted path p using idx, falling back to
// just file if the line-indexer could not locate the key (should not happen
// for well-formed files, but degrades gracefully rather than panicking).
func sourceAt(file string, idx map[string]int, path string) string {
	if ln, ok := idx[path]; ok {
		return file + ":" + strconv.Itoa(ln)
	}
	return file
}
