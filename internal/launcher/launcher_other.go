//go:build !unix

package launcher

import (
	"context"
	"fmt"
	"runtime"
)

// Run is unsupported on this platform. omni's PTY launcher targets
// unix-like systems (github.com/creack/pty plus golang.org/x/term); Windows
// ConPTY support is a separate problem and out of scope for v1 (see
// internal-docs/05-constraints.md §4). Callers should surface this as a
// clear startup error rather than attempting to launch a child.
func Run(_ context.Context, _ Spec) (Result, error) {
	return Result{}, fmt.Errorf("launcher: unsupported platform %s/%s (PTY launch requires a unix-like OS)", runtime.GOOS, runtime.GOARCH)
}
