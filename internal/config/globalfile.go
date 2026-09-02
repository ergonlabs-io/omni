package config

import (
	"maps"
	"slices"

	"github.com/BurntSushi/toml"
)

// loadGlobal reads and validates ~/.omni/omni.conf. A missing file is not
// an error — callers check os.IsNotExist and treat the layer as absent.
func loadGlobal(path string) (rawGlobal, *fileLoad, error) {
	fl, err := readFile(path)
	if err != nil {
		return rawGlobal{}, nil, err
	}
	var g rawGlobal
	if _, err := toml.Decode(fl.content, &g); err != nil {
		return rawGlobal{}, fl, err
	}
	generic, err := decodeGeneric(fl)
	if err != nil {
		return rawGlobal{}, fl, err
	}
	fl.issues = append(fl.issues, checkGlobalKeys(generic, fl)...)
	return g, fl, nil
}

// checkGlobalKeys validates every key present in omni.conf: top level must
// be "defaults", "agents", or "backends"; under [defaults], keys must be in
// knownDefaultsPaths; under [agents.<name>], keys must be in
// knownAgentPaths (agent names themselves are user-chosen and unrestricted
// here — profiles.d can register arbitrary agents); under
// [backends.<name>], keys must be in knownBackendPaths.
func checkGlobalKeys(generic map[string]interface{}, fl *fileLoad) []Issue {
	var issues []Issue
	for _, topKey := range slices.Sorted(maps.Keys(generic)) {
		topVal := generic[topKey]
		switch topKey {
		case "defaults":
			sub, ok := topVal.(map[string]interface{})
			if !ok {
				continue
			}
			leaves := map[string]interface{}{}
			flattenLeaves("", sub, leaves)
			for _, p := range sortedLeaves(leaves) {
				if !knownPath(p, knownDefaultsPaths, nil) {
					full := "defaults." + p
					issues = append(issues, unknownKeyIssue(full, p, fl.src(full), knownDefaultsPaths))
				}
			}
		case "agents":
			agentsMap, ok := topVal.(map[string]interface{})
			if !ok {
				continue
			}
			for _, agentName := range slices.Sorted(maps.Keys(agentsMap)) {
				agentVal := agentsMap[agentName]
				sub, ok := agentVal.(map[string]interface{})
				if !ok {
					continue
				}
				leaves := map[string]interface{}{}
				flattenLeaves("", sub, leaves)
				for _, p := range sortedLeaves(leaves) {
					v := leaves[p]
					full := "agents." + agentName + "." + p
					if p == "route" {
						issues = append(issues, checkRouteKeys(v, full, fl.src(full))...)
						continue
					}
					if !knownPath(p, knownAgentPaths, knownAgentWildcards) {
						issues = append(issues, unknownKeyIssue(full, p, fl.src(full), knownAgentPaths))
					}
				}
			}
		case "backends":
			backendsMap, ok := topVal.(map[string]interface{})
			if !ok {
				continue
			}
			for _, backendName := range slices.Sorted(maps.Keys(backendsMap)) {
				backendVal := backendsMap[backendName]
				sub, ok := backendVal.(map[string]interface{})
				if !ok {
					continue
				}
				leaves := map[string]interface{}{}
				flattenLeaves("", sub, leaves)
				for _, p := range sortedLeaves(leaves) {
					if !knownPath(p, knownBackendPaths, knownBackendWildcards) {
						full := "backends." + backendName + "." + p
						issues = append(issues, unknownKeyIssue(full, p, fl.src(full), knownBackendPaths))
					}
				}
			}
		default:
			issues = append(issues, unknownKeyIssue(topKey, topKey, fl.src(topKey),
				map[string]bool{"defaults": true, "agents": true, "backends": true}))
		}
	}
	return issues
}
