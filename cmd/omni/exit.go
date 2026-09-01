package main

// Exit codes.
//
// When the child agent ran, omni exits with the child's exit code — scripts
// wrapping `omni claude` must see exactly what bare `claude` would give them.
// When omni itself fails, we use sysexits.h ranges so we are unlikely to
// collide with a child's own codes.
//
// Collisions remain possible in principle. The disambiguator is that every
// omni-originated failure prints to stderr prefixed "omni: " and the child's
// output never is. See internal-docs/09-cli-design.md §4.
const (
	exitOK = 0

	// exitUsage: bad flags, unknown agent, malformed invocation.
	exitUsage = 64 // EX_USAGE
	// exitNoInput: a config file was referenced but is unreadable.
	exitNoInput = 66 // EX_NOINPUT
	// exitUnavailable: agent binary not on PATH, proxy could not bind.
	exitUnavailable = 69 // EX_UNAVAILABLE
	// exitSoftware: internal error. A bug in omni.
	exitSoftware = 70 // EX_SOFTWARE
	// exitConfig: configuration invalid, syntactically or semantically.
	exitConfig = 78 // EX_CONFIG
)
