package config

import (
	"fmt"
	"strings"
)

// loadProjectConfig reads and filters ./.omni.conf, the per-project,
// repo-local config layer. This is the SECURITY-CRITICAL boundary in this
// package: a repo you `cd` into and did not write is untrusted input, and
// must not be able to change omni's binary, upstream, redaction, traffic
// scope, or proxy bind address. Only mode, model_map, and record.bodies are
// honored; every other key found in the file is dropped and reported as a
// LevelWarning Issue (not an error — a stray key here is a mistake, not a
// reason to fail the launch).
//
// Unlike loadGlobal/loadAgentFile, this does not decode into a typed
// struct first and then check keys — it walks the untyped document and
// only ever assigns a field for a key it has explicitly allowed. That
// ordering is deliberate: it means a field this package does not yet know
// about (or one added to rawAgent for other layers) can never leak into a
// project config's effect just because a struct tag matched.
func loadProjectConfig(path string) (rawAgent, *fileLoad, error) {
	fl, err := readFile(path)
	if err != nil {
		return rawAgent{}, nil, err
	}
	generic, err := decodeGeneric(fl)
	if err != nil {
		return rawAgent{}, fl, err
	}
	leaves := map[string]interface{}{}
	flattenLeaves("", generic, leaves)

	var out rawAgent
	for p, v := range leaves {
		if !projectAllowedPath(p) {
			fl.issues = append(fl.issues, Issue{
				Path: p,
				Message: fmt.Sprintf(
					"%q is not permitted in project config (./.omni.conf may only set mode, model_map, record.bodies) — ignored",
					p,
				),
				Source: fl.src(p),
				Level:  LevelWarning,
			})
			continue
		}
		if iss := assignProjectLeaf(&out, p, v, fl.src(p)); iss != nil {
			fl.issues = append(fl.issues, *iss)
		}
	}
	return out, fl, nil
}

// assignProjectLeaf sets the one field on dst that path names, after
// projectAllowedPath has already confirmed path is on the allowlist.
func assignProjectLeaf(dst *rawAgent, path string, v interface{}, source string) *Issue {
	switch {
	case path == "mode":
		s, ok := v.(string)
		if !ok {
			iss := typeIssue(path, "string", v, source)
			return &iss
		}
		dst.Mode = &s
		return nil

	case path == "record.bodies":
		b, ok := v.(bool)
		if !ok {
			iss := typeIssue(path, "bool", v, source)
			return &iss
		}
		dst.Record.Bodies = &b
		return nil

	case path == "model_map" || strings.HasPrefix(path, "model_map."):
		key := strings.TrimPrefix(path, "model_map.")
		if key == "" || key == path {
			// "model_map" itself as a non-table leaf, e.g. `model_map = 1`.
			iss := typeIssue(path, "table", v, source)
			return &iss
		}
		s, ok := v.(string)
		if !ok {
			iss := typeIssue(path, "string", v, source)
			return &iss
		}
		if dst.ModelMap == nil {
			dst.ModelMap = map[string]string{}
		}
		dst.ModelMap[key] = s
		return nil

	default:
		// Unreachable: caller only invokes this after projectAllowedPath(path).
		return nil
	}
}
