package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// fileLoad holds everything derived from reading one TOML file: its raw
// text (for the line indexer), the line index itself, and any Issues found
// while validating its keys.
type fileLoad struct {
	path    string
	content string
	lines   map[string]int
	issues  []Issue
}

// readFile reads path and prepares its fileLoad. Returns an error only for
// I/O failures — a missing file is reported by the caller as "layer absent",
// not as an error, since most layers are optional.
func readFile(path string) (*fileLoad, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(b)
	return &fileLoad{path: path, content: content, lines: lineIndex(content)}, nil
}

// src returns "path:line" (or just path if the line indexer could not find
// the key) for dotted path p within this file.
func (f *fileLoad) src(p string) string { return sourceAt(f.path, f.lines, p) }

// decodeGeneric parses f's content as an untyped TOML document, for
// unknown-key detection and for the project-config allowlist walk.
func decodeGeneric(f *fileLoad) (map[string]interface{}, error) {
	var m map[string]interface{}
	if _, err := toml.Decode(f.content, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// flattenLeaves walks v (as decoded by decodeGeneric) and records every
// leaf value under its dotted path in out. Empty tables produce no leaves —
// there is nothing to validate about a table nobody put a key in.
func flattenLeaves(prefix string, v interface{}, out map[string]interface{}) {
	if t, ok := v.(map[string]interface{}); ok {
		for k, sub := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flattenLeaves(p, sub, out)
		}
		return
	}
	out[prefix] = v
}

// unknownKeyIssue builds a LevelError Issue for a config key not present in
// known, with a Levenshtein-based "did you mean" suggestion. fullPath is
// used for display; localPath is the (possibly shorter, un-prefixed) form
// compared against known's keys.
func unknownKeyIssue(fullPath, localPath, source string, known map[string]bool) Issue {
	msg := fmt.Sprintf("unknown config key %q", fullPath)
	if s := suggest(localPath, known); len(s) > 0 {
		msg += fmt.Sprintf(" (did you mean: %s?)", strings.Join(s, ", "))
	}
	return Issue{Path: fullPath, Message: msg, Source: source, Level: LevelError}
}

// typeIssue builds a LevelError Issue reporting that path held a value of
// the wrong TOML type.
func typeIssue(path, want string, got interface{}, source string) Issue {
	return Issue{
		Path:    path,
		Message: fmt.Sprintf("%s must be a %s, got %T", path, want, got),
		Source:  source,
		Level:   LevelError,
	}
}

// checkRouteKeys validates the keys inside each element of a [[route]]
// array. flattenLeaves stops at the array — it only descends into tables —
// so a rule's own keys would otherwise never be checked, and a typo'd
// `backends = "openrouter"` would be silently dropped rather than reported.
func checkRouteKeys(v interface{}, fullPath, source string) []Issue {
	elems, ok := routeElements(v)
	if !ok {
		return []Issue{typeIssue(fullPath, "array of tables", v, source)}
	}
	var issues []Issue
	for i, el := range elems {
		for k := range el {
			if knownRoutePaths[k] {
				continue
			}
			p := fmt.Sprintf("%s[%d].%s", fullPath, i, k)
			issues = append(issues, unknownKeyIssue(p, k, source, knownRoutePaths))
		}
	}
	return issues
}
