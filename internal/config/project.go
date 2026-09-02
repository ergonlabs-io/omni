package config

import "fmt"

// loadProjectConfig reads and filters ./.omni.conf, the per-project,
// repo-local config layer. This is the SECURITY-CRITICAL boundary in this
// package: a repo you `cd` into and did not write is untrusted input, and
// must not be able to change omni's binary, upstream, redaction, traffic
// scope, or proxy bind address. Only mode, route, and record.bodies are
// honored; every other key found in the file is dropped and reported as a
// LevelWarning Issue (not an error — a stray key here is a mistake, not a
// reason to fail the launch).
//
// [[route]] is honored here only in its rename form. A rule naming a
// backend is rejected outright — see assignProjectRoutes.
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
	for _, p := range sortedLeaves(leaves) {
		v := leaves[p]
		if !projectAllowedPath(p) {
			fl.issues = append(fl.issues, Issue{
				Path: p,
				Message: fmt.Sprintf(
					"%q is not permitted in project config (./.omni.conf may only set mode, route, record.bodies) — ignored",
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
// projectAllowedPath has already confirmed path is on the allowlist. It
// type-checks the untyped TOML value on the way, since nothing has decoded
// this file into a typed struct.
func assignProjectLeaf(dst *rawAgent, path string, v interface{}, source string) *Issue {
	switch path {
	case "mode":
		s, ok := v.(string)
		if !ok {
			iss := typeIssue(path, "string", v, source)
			return &iss
		}
		dst.Mode = &s
		return nil

	case "route":
		return assignProjectRoutes(dst, path, v, source)

	default:
		// Unreachable: caller only invokes this after projectAllowedPath(path).
		return nil
	}
}

// assignProjectRoutes accepts a project config's [[route]] list, but only
// the rules that rename a model within the agent's own provider.
//
// A rule carrying `backend` is refused. Renaming a model is a local
// preference a repository can reasonably express; choosing which third
// party receives your prompts, and bills you for them, is not — and the
// backend it named would be one the *user* declared globally, which makes
// the escalation quiet rather than obvious. See loadProjectConfig.
func assignProjectRoutes(dst *rawAgent, path string, v interface{}, source string) *Issue {
	elems, ok := routeElements(v)
	if !ok {
		iss := typeIssue(path, "array of tables", v, source)
		return &iss
	}
	for _, el := range elems {
		if b, present := el["backend"]; present {
			return &Issue{
				Path: path,
				Message: fmt.Sprintf(
					"project config may not route to a backend (%v) — ./.omni.conf can rename a "+
						"model but not change its destination; move this to ~/.omni/omni.conf if you meant it",
					b,
				),
				Source: source,
				Level:  LevelError,
			}
		}
	}
	for _, el := range elems {
		var r rawRoute
		if m, ok := el["match"].(string); ok {
			r.Match = &m
		}
		if m, ok := el["model"].(string); ok {
			r.Model = &m
		}
		dst.Route = append(dst.Route, r)
	}
	return nil
}

// routeElements normalizes the shapes BurntSushi/toml can hand back for an
// array of tables.
func routeElements(v interface{}) ([]map[string]interface{}, bool) {
	switch t := v.(type) {
	case []map[string]interface{}:
		return t, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, el := range t {
			m, ok := el.(map[string]interface{})
			if !ok {
				return nil, false
			}
			out = append(out, m)
		}
		return out, true
	default:
		return nil, false
	}
}
